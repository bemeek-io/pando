package reconciler

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/state"
)

// GCInterval is how often garbage collection runs. [P], design 05 §6.
const GCInterval = time.Hour

// GC reclaims what retention says should be gone.
//
// A separate job from the reconciler, on a much slower clock, because nothing
// here is urgent and all of it is destructive. The reconciler's rule — never
// destroy anything a human may have wanted — applies here too, and is the
// reason each of these deletions has an explicit exemption attached.
type GC struct {
	Apps   *state.Apps
	Logger *zap.Logger
}

// Run collects until the context is cancelled.
func (g *GC) Run(ctx context.Context) {
	ticker := time.NewTicker(GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.Collect(ctx)
		}
	}
}

// Collect runs one pass.
//
// Spec revision pruning only, for now. Log retention (R-222–R-224) is not here
// because there is nothing for it to act on: Pando does not hold app logs, it
// streams them from the runtime, and the bytes live wherever that runtime put
// them. Enforcing a per-app cap means the log driver's own options at container
// creation; enforcing the aggregate would mean recreating every container when
// one app turns chatty, which is destruction on a schedule and not something
// the reconciler may do. Recorded as O-16 rather than guessed at.
//
// Backup expiry (R-211) arrives with backups, in phase 9.
func (g *GC) Collect(ctx context.Context) {
	pruned, err := g.Apps.PruneSpecRevisions(ctx)
	if err != nil {
		g.Logger.Warn("could not prune spec revisions", zap.Error(err))
		return
	}
	if pruned > 0 {
		g.Logger.Info("pruned spec revisions", zap.Int("count", pruned))
	}
}
