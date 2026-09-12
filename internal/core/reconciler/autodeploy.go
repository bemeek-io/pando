package reconciler

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
)

// Auto-deploy polling intervals. [P], design 05 §5.
const (
	BranchPollInterval  = 5 * time.Minute
	ReleasePollInterval = 15 * time.Minute
)

// AutoDeploy watches tracked refs and enqueues deployments (R-141).
//
// A separate scheduled job, deliberately not part of the reconciler. It never
// modifies a running app: it creates a spec revision carrying the new commit
// and enqueues a deployment, and everything then flows through the normal path
// including every plan-time check. The reconciler converges what is pinned;
// this decides what should be pinned. Conflating them would make an unrelated
// drift correction able to ship new code.
//
// Off by default (R-141). Nothing here runs for an app that did not ask.
type AutoDeploy struct {
	Apps        *state.Apps
	Deployments *state.Deployments
	Resolver    RefResolver
	Enqueue     func(ctx context.Context, dep state.Deployment, rev state.Revision)
	Logger      *zap.Logger
}

// RefResolver turns a ref into the commit it currently points at.
//
// Listing a remote's refs rather than cloning: this runs every five minutes for
// every app tracking a branch, and cloning each time to learn a SHA that has
// usually not changed would be most of Pando's network traffic.
type RefResolver interface {
	Resolve(ctx context.Context, src spec.Source) (string, error)
}

// Run polls until the context is cancelled.
func (a *AutoDeploy) Run(ctx context.Context) {
	ticker := time.NewTicker(BranchPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.Poll(ctx)
		}
	}
}

// Poll checks every app that tracks a ref.
func (a *AutoDeploy) Poll(ctx context.Context) {
	apps, err := a.Apps.WithAutoDeploy(ctx)
	if err != nil {
		a.Logger.Warn("could not list apps tracking a branch", zap.Error(err))
		return
	}

	for _, app := range apps {
		if err := a.pollOne(ctx, app); err != nil {
			a.Logger.Warn("could not check for new commits",
				zap.String("app_id", app.ID), zap.Error(err))
		}
	}
}

func (a *AutoDeploy) pollOne(ctx context.Context, app state.App) error {
	// Skipped, not queued. A queue on a fast-moving branch produces a backlog
	// nobody wants, and the next poll picks up whatever is newest anyway.
	inFlight, err := a.Deployments.InFlight(ctx, app.ID)
	if err != nil || inFlight {
		return err
	}

	rev, found, err := a.Apps.RevisionByID(ctx, app.PinnedSpecID)
	if err != nil || !found {
		return err
	}

	head, err := a.Resolver.Resolve(ctx, rev.Body.Source)
	if err != nil {
		return err
	}
	if head == "" || head == rev.Body.Source.Commit {
		return nil
	}

	// A new revision carrying the new commit, and then the ordinary path. The
	// deploy itself never resolves a ref (design 01 §2.1) — this is the
	// explicit act that does it.
	next := *rev.Body
	next.Source.Commit = head

	created, err := a.Apps.CreateRevision(ctx, app.ID, &next, spec.OriginDetected, "auto-deploy")
	if err != nil {
		return err
	}

	dep, err := a.Deployments.Create(ctx, app.ID, created.ID,
		triggerFor(rev.Body.Deploy.AutoDeploy.Trigger), "auto-deploy")
	if err != nil {
		return err
	}

	a.Logger.Info("auto-deploy enqueued",
		zap.String("app_id", app.ID), zap.String("commit", head))

	if a.Enqueue != nil {
		a.Enqueue(ctx, dep, created)
	}
	return nil
}

func triggerFor(t spec.AutoDeployTrigger) string {
	if t == spec.TriggerReleaseTagged {
		return "release_tagged"
	}
	return "branch_updated"
}
