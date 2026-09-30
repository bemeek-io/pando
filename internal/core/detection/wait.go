package detection

import (
	"context"
	"time"

	"github.com/trypando/pando/internal/core/clock"
	"github.com/trypando/pando/internal/core/state"
)

// MaxWait is the longest a caller may ask Await to hold a request open.
//
// Long enough that a client waiting out a trial run makes a handful of
// requests rather than hundreds, and short enough to finish well inside the
// idle timeout of any proxy in front of Pando — a held request cut off by one
// is a failure the caller did nothing to cause (issue #80).
const MaxWait = 60 * time.Second

// pollInterval is how often Await reads the detection while it waits. Stages
// are minutes apart; half a second is prompt without being a load.
const pollInterval = 500 * time.Millisecond

// DetectionReader reads an app's current detection.
type DetectionReader interface {
	Get(ctx context.Context, appID string) (state.Detection, error)
}

// Await returns an app's detection once it has moved on from what it was when
// Await was called — a new stage, or an outcome — or once wait has passed,
// whichever is first (issue #80).
//
// A detection that is not running is returned at once: there is nothing to
// wait for, and holding a finished answer back would only look like a hang.
// So is one with no wait asked for, which is the plain GET.
//
// A long poll rather than a stream, because it works through every proxy and
// fits MCP's one-request-one-answer shape, and every surface uses the same
// one (R-261). Reading the row on an interval rather than listening for a
// notification, because detection is recorded in exactly one place and this
// is the boring way to watch it.
func Await(ctx context.Context, r DetectionReader, c clock.Clock, appID string, wait time.Duration) (state.Detection, error) {
	if c == nil {
		c = clock.System{}
	}
	first, err := r.Get(ctx, appID)
	if err != nil || first.Status != state.DetectionRunning || wait <= 0 {
		return first, err
	}
	wait = min(wait, MaxWait)

	deadline := c.Now().Add(wait)
	current := first
	for {
		remaining := deadline.Sub(c.Now())
		if remaining <= 0 {
			return current, nil
		}
		select {
		case <-ctx.Done():
			// The caller gave up — a client disconnecting, usually. What was
			// last read is still the truth, and nobody is left to be told
			// about a cancellation.
			return current, nil
		case <-c.After(min(pollInterval, remaining)):
		}

		next, err := r.Get(ctx, appID)
		if err != nil {
			return current, err
		}
		current = next
		// Every stage and every outcome is written with a new updated_at, so
		// that alone says something happened.
		if current.Status != first.Status || !current.UpdatedAt.Equal(first.UpdatedAt) {
			return current, nil
		}
	}
}
