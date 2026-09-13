package reconciler

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/backup"
	"github.com/bemeek-io/pando/internal/core/state"
)

// Rolling per-app backups (R-210, R-211) and expiry.
//
// R-211's "daily, 7 retained" had a schema column, a retention default and a
// query that found expired rows — and nothing that ever took a backup or
// deleted one. The requirement was true in the database and false in practice.
//
// Here rather than in the reconciler's own loop because nothing about this is
// urgent and all of it is destructive, which is the same reason spec pruning
// and bundle teardown are here.

// BackupRunner takes and prunes per-app backups.
//
// An interface rather than *backup.Service so the collector is testable without
// a destination, and so this package cannot reach anything else the service
// exposes — restore in particular, which the janitor has no business calling.
type BackupRunner interface {
	CreateForApp(ctx context.Context, id string, req backup.AppCreateRequest) (backup.Created, error)
	Discard(ctx context.Context, adapterRef, objectName string) error
}

// backupDue reports whether an app is due a rolling backup.
//
// Daily, measured from the last rolling backup rather than from a fixed clock
// time: an install that is off overnight should take a backup when it comes
// back, not skip the day. R-211's "daily" is an interval, not an appointment.
const backupInterval = 24 * time.Hour

// runBackups takes rolling backups for apps that are due one, then removes
// backups whose retention has passed.
func (g *GC) runBackups(ctx context.Context) {
	if g.Backups == nil || g.Backup == nil || g.BundleSource == nil {
		return
	}

	g.takeRollingBackups(ctx)
	g.pruneExpiredBackups(ctx)
}

func (g *GC) takeRollingBackups(ctx context.Context) {
	apps, err := g.Apps.WithStorage(ctx)
	if err != nil {
		g.Logger.Warn("could not list apps for rolling backups", zap.Error(err))
		return
	}

	for _, app := range apps {
		// Only apps that have data to lose. An app with no volumes has nothing
		// a rolling backup would hold that its spec revisions do not, and
		// taking one anyway would fill the destination with empty bundles.
		if app.Retain <= 0 {
			continue
		}

		last, err := g.Backups.LastRolling(ctx, app.AppID)
		if err != nil {
			g.Logger.Warn("could not read the last backup", zap.String("app_id", app.AppID), zap.Error(err))
			continue
		}
		if !last.IsZero() && g.now().Sub(last) < backupInterval {
			continue
		}

		if err := g.takeOne(ctx, app); err != nil {
			// Logged, not retried harder. A destination that is down will be up
			// on the next pass, and a backup that fails is not a reason to stop
			// backing up every other app.
			g.Logger.Warn("could not take a rolling backup",
				zap.String("app_id", app.AppID), zap.Error(err))
			continue
		}
		g.Logger.Info("took a rolling backup", zap.String("app_id", app.AppID))
	}
}

func (g *GC) takeOne(ctx context.Context, app state.AppWithStorage) error {
	volumes, err := g.BundleSource.VolumesForApp(ctx, app.AppID)
	if err != nil {
		return err
	}
	if len(volumes) == 0 {
		return nil
	}

	id := g.Backups.NewID()
	created, err := g.Backup.CreateForApp(ctx, id, backup.AppCreateRequest{
		AppID:   app.AppID,
		Kind:    "rolling",
		Spec:    app.Spec,
		Volumes: volumes,

		// Retained for as many days as the app asks to keep copies. R-211's
		// count is a number of dailies, so the window is that many days — which
		// is what makes "7 retained" mean seven rather than seven-ish.
		RetainFor: time.Duration(app.Retain) * backupInterval,
	})
	if err != nil {
		return err
	}

	return g.Backups.Record(ctx, state.Backup{
		ID: id, AppID: app.AppID, Kind: "rolling",
		AdapterRef: created.AdapterRef, ObjectName: created.ObjectName,
		SizeBytes: created.SizeBytes, Manifest: created.Manifest,
		RetainUntil: created.RetainUntil, CreatedBy: "system",
	})
}

// pruneExpiredBackups removes backups whose retention has passed (R-211).
//
// The object first, then the row. The other order leaves an object nothing
// references, which is invisible and accumulates — and an on_delete backup is
// never returned here, because R-204 keeps those until somebody discards them.
func (g *GC) pruneExpiredBackups(ctx context.Context) {
	expired, err := g.Backups.Expired(ctx, g.now())
	if err != nil {
		g.Logger.Warn("could not list expired backups", zap.Error(err))
		return
	}

	for _, b := range expired {
		if err := g.Backup.Discard(ctx, b.AdapterRef, b.ObjectName); err != nil {
			g.Logger.Warn("could not remove an expired backup",
				zap.String("backup_id", b.ID), zap.Error(err))
			continue
		}
		if err := g.Backups.Forget(ctx, b.ID); err != nil {
			g.Logger.Warn("removed an expired backup but could not record it",
				zap.String("backup_id", b.ID), zap.Error(err))
			continue
		}

		g.Logger.Info("removed an expired backup", zap.String("backup_id", b.ID))
		if g.Auditor != nil {
			_ = g.Auditor.Write(ctx, AuditEvent{
				Action: "backup.expire", AppID: b.AppID,
				Detail: map[string]any{"backup_id": b.ID, "kind": b.Kind},
			})
		}
	}
}

func (g *GC) now() time.Time {
	if g.Clock == nil {
		return time.Now().UTC()
	}
	return g.Clock.Now()
}
