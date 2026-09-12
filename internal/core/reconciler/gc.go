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
