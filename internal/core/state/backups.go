package state

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bemeek-io/pando/internal/core/backup"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// Backups records what has been backed up and where (design 02 §2.8).
type Backups struct{ db *DB }

func NewBackups(db *DB) *Backups { return &Backups{db: db} }

// Backup is one stored backup.
type Backup struct {
	ID         string `json:"id"`
	AppID      string `json:"app_id,omitempty"`
	Kind       string `json:"kind"`
	AdapterRef string `json:"adapter_ref"`
	ObjectName string `json:"object_name"`
	SizeBytes  int64  `json:"size_bytes"`

	Manifest backup.Manifest `json:"manifest"`

	// RetainUntil is nil when the backup is kept until explicitly discarded:
	// always for on_delete (R-204), and for any kind whose destination owns
	// retention itself (R-217).
	RetainUntil *time.Time `json:"retain_until,omitempty"`

	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// NewID returns an ID for a backup about to be written.
//
// Allocated before the bundle is built, because the ID is the object name at
// the destination — so a crash mid-write leaves an object nothing references
// rather than a row pointing at nothing. The first is garbage; the second is a
// restore that cannot find its bundle.
func (b *Backups) NewID() string { return id.New(id.Backup) }

// Record stores a backup that has been fully written.
//
// Called after the destination's Close returns, never before. A row written
// first would claim a bundle exists during the window where it does not, and
// that window is exactly when a disaster is most likely to interrupt.
func (b *Backups) Record(ctx context.Context, rec Backup) error {
	manifest, err := json.Marshal(rec.Manifest)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the backup.", err)
	}

	// ON CONFLICT DO NOTHING so re-recording is safe. That matters after a
	// restore: the bundle's own row did not exist when the bundle was taken, so
	// restoring erases the record of the backup being restored from, and the
	// caller puts it back.
	_, err = b.db.Exec(ctx, `
		INSERT INTO backups (id, app_id, kind, adapter_ref, object_name, size_bytes,
		                     manifest, retain_until, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO NOTHING`,
		rec.ID, nullable(rec.AppID), rec.Kind, rec.AdapterRef, rec.ObjectName,
		rec.SizeBytes, manifest, rec.RetainUntil, rec.CreatedBy)
	if err != nil {
		if isUniqueViolation(err) {
			return errs.New(errs.Internal, "A backup with that name already exists at this destination.")
		}
		return errs.Wrap(errs.Internal, "Could not record the backup.", err)
	}
	return nil
}

// ByID returns one backup.
func (b *Backups) ByID(ctx context.Context, backupID string) (Backup, bool, error) {
	row := b.db.QueryRow(ctx, `
		SELECT id, coalesce(app_id, ''), kind, adapter_ref, object_name,
		       coalesce(size_bytes, 0), manifest, retain_until, created_by, created_at
		FROM backups WHERE id = $1`, backupID)

	rec, err := scanBackup(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Backup{}, false, nil
	}
	if err != nil {
		return Backup{}, false, errs.Wrap(errs.Internal, "Could not read the backup.", err)
	}
	return rec, true, nil
}

// List returns backups, newest first. An empty appID lists every backup.
func (b *Backups) List(ctx context.Context, appID string) ([]Backup, error) {
	var rows pgx.Rows
	var err error

	const columns = `SELECT id, coalesce(app_id, ''), kind, adapter_ref, object_name,
	                        coalesce(size_bytes, 0), manifest, retain_until, created_by, created_at
	                 FROM backups`
	if appID == "" {
		rows, err = b.db.Query(ctx, columns+` ORDER BY created_at DESC`)
	} else {
		rows, err = b.db.Query(ctx, columns+` WHERE app_id = $1 ORDER BY created_at DESC`, appID)
	}
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read the backups.", err)
	}
	defer rows.Close()

	out := make([]Backup, 0)
	for rows.Next() {
		rec, err := scanBackup(rows)
		if err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read the backups.", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Expired returns backups whose retention has passed (R-211).
//
// Never returns an on_delete row: those have no retain_until at all, and the
// schema refuses to give them one. The WHERE clause says so anyway, because a
// pruning query that relies on a constraint elsewhere is one migration away
// from deleting the backups R-204 promises to keep.
func (b *Backups) Expired(ctx context.Context, now time.Time) ([]Backup, error) {
	rows, err := b.db.Query(ctx, `
		SELECT id, coalesce(app_id, ''), kind, adapter_ref, object_name,
		       coalesce(size_bytes, 0), manifest, retain_until, created_by, created_at
		FROM backups
		WHERE kind <> 'on_delete' AND retain_until IS NOT NULL AND retain_until < $1
		ORDER BY retain_until`, now)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read expired backups.", err)
	}
	defer rows.Close()

	out := make([]Backup, 0)
	for rows.Next() {
		rec, err := scanBackup(rows)
		if err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read expired backups.", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// LastRolling returns when the app last had a rolling backup, or the zero time.
func (b *Backups) LastRolling(ctx context.Context, appID string) (time.Time, error) {
	var at *time.Time
	err := b.db.QueryRow(ctx,
		`SELECT max(created_at) FROM backups WHERE app_id = $1 AND kind = 'rolling'`,
		appID).Scan(&at)
	if err != nil {
		return time.Time{}, errs.Wrap(errs.Internal, "Could not read the app's backups.", err)
	}
	if at == nil {
		return time.Time{}, nil
	}
	return *at, nil
}

// Forget removes the record of a backup whose object has been deleted.
//
// The object goes first, then this. The other order leaves an object nothing
// references, which is invisible and accumulates.
func (b *Backups) Forget(ctx context.Context, backupID string) error {
	_, err := b.db.Exec(ctx, `DELETE FROM backups WHERE id = $1`, backupID)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not discard the backup.", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanBackup(row scanner) (Backup, error) {
	var rec Backup
	var manifest []byte
	if err := row.Scan(&rec.ID, &rec.AppID, &rec.Kind, &rec.AdapterRef, &rec.ObjectName,
		&rec.SizeBytes, &manifest, &rec.RetainUntil, &rec.CreatedBy, &rec.CreatedAt); err != nil {
		return Backup{}, err
	}
	if len(manifest) > 0 {
		if err := json.Unmarshal(manifest, &rec.Manifest); err != nil {
			return Backup{}, err
		}
	}
	return rec, nil
}

// BundleSource implements backup.StateSource against Postgres.
//
// Separate from Backups: one records what was backed up, the other supplies
// what goes in a bundle. Merging them would put the thing being backed up and
// the record of the backup behind one type, and the record has to survive the
// thing.
type BundleSource struct{ db *DB }

func NewBundleSource(db *DB) *BundleSource { return &BundleSource{db: db} }

// Counts are the object counts the manifest records (R-215).
//
// Checksums prove the bytes arrived. Counts prove the bytes describe the
// install the operator thinks they are restoring, which is the question
// somebody staring at a restore prompt actually has.
func (b *BundleSource) Counts(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	for object, query := range map[string]string{
		"apps":   `SELECT count(*) FROM apps WHERE deleted_at IS NULL`,
		"users":  `SELECT count(*) FROM users WHERE deleted_at IS NULL`,
		"grants": `SELECT count(*) FROM grants`,
		"specs":  `SELECT count(*) FROM spec_revisions`,
		"tokens": `SELECT count(*) FROM tokens`,
	} {
		var n int
		if err := b.db.QueryRow(ctx, query).Scan(&n); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not count what is in this installation.", err)
		}
		out[object] = n
	}
	return out, nil
}

// AdapterConfigs exports adapter configuration as readable JSON.
//
// Also in the pg_dump, and deliberately duplicated: this copy is for the
// operator rebuilding an install by hand, which is exactly the situation where
// the database is the thing that will not load.
func (b *BundleSource) AdapterConfigs(ctx context.Context) ([]byte, error) {
	return b.jsonRows(ctx, `
		SELECT coalesce(json_agg(row_to_json(t)), '[]'::json)
		FROM (SELECT id, kind, category, name, config FROM adapter_configs ORDER BY id) t`)
}

// HostPolicy exports the policy document.
func (b *BundleSource) HostPolicy(ctx context.Context) ([]byte, error) {
	return b.jsonRows(ctx, `
		SELECT coalesce(to_json(body), 'null'::json) FROM host_policy WHERE id = 1`)
}

func (b *BundleSource) jsonRows(ctx context.Context, query string) ([]byte, error) {
	var body []byte
	err := b.db.QueryRow(ctx, query).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return []byte("null"), nil
	}
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not export this installation's configuration.", err)
	}
	if len(body) == 0 {
		return []byte("null"), nil
	}
	return body, nil
}

// VolumesToSnapshot lists live volumes with the runtime that holds each.
//
// Volumes belonging to deleted apps are included: R-204 keeps a volume after
// its app is gone, and a DR bundle that dropped exactly the data somebody is
// still deciding whether to keep would defeat the point of keeping it.
func (b *BundleSource) VolumesToSnapshot(ctx context.Context) ([]backup.VolumeRef, error) {
	rows, err := b.db.Query(ctx, `
		SELECT id, coalesce(adapter_ref, ''), coalesce(handle, '')
		FROM volumes
		ORDER BY id`)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read this installation's storage.", err)
	}
	defer rows.Close()

	out := make([]backup.VolumeRef, 0)
	for rows.Next() {
		var v backup.VolumeRef
		if err := rows.Scan(&v.VolumeID, &v.AdapterRef, &v.Handle); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read this installation's storage.", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ServicesToSnapshot lists every provisioned service (R-131, R-212).
//
// Every one, not only those the backup will call Snapshot on. The manifest
// records both counts, and a bundle that says "5 services, 0 snapshotted"
// because they all live in app volumes is very different from a bundle that
// says it because the provisioner was unreachable — but only if the first
// number is counted here rather than inferred from the second.
func (b *BundleSource) ServicesToSnapshot(ctx context.Context) ([]backup.ServiceRef, error) {
	rows, err := b.db.Query(ctx, `
		SELECT id, app_id, adapter_ref, handle
		FROM service_instances ORDER BY id`)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read this installation's provisioned services.", err)
	}
	defer rows.Close()

	out := make([]backup.ServiceRef, 0)
	for rows.Next() {
		var v backup.ServiceRef
		if err := rows.Scan(&v.ServiceID, &v.AppID, &v.AdapterRef, &v.Handle); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read this installation's provisioned services.", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// VolumesForApp lists one app's volumes, for a per-app backup (R-204).
//
// Includes volumes whose app is already archived: R-204 keeps a volume after
// its app is gone, and the final backup is taken at exactly that moment.
func (b *BundleSource) VolumesForApp(ctx context.Context, appID string) ([]backup.VolumeRef, error) {
	rows, err := b.db.Query(ctx, `
		SELECT id, coalesce(adapter_ref, ''), coalesce(handle, '')
		FROM volumes WHERE app_id = $1 ORDER BY id`, appID)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read the app's storage.", err)
	}
	defer rows.Close()

	out := make([]backup.VolumeRef, 0)
	for rows.Next() {
		var v backup.VolumeRef
		if err := rows.Scan(&v.VolumeID, &v.AdapterRef, &v.Handle); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read the app's storage.", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var _ backup.StateSource = (*BundleSource)(nil)
