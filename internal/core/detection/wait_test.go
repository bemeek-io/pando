package detection

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/clock"
	"github.com/trypando/pando/internal/core/state"
)

// scriptedReader answers Get from a script: the nth read returns the nth
// detection, and the last one from then on.
type scriptedReader struct {
	reads  atomic.Int32
	script []state.Detection
}

func (s *scriptedReader) Get(context.Context, string) (state.Detection, error) {
	n := int(s.reads.Add(1)) - 1
	return s.script[min(n, len(s.script)-1)], nil
}

// ticking advances a fake clock by the poll interval until the test ends, so
// Await's waits pass without the test sleeping through them.
func ticking(t *testing.T) *clock.Fake {
	t.Helper()
	c := clock.NewFake(time.Time{})
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				c.Advance(pollInterval)
				time.Sleep(time.Millisecond)
			}
		}
	}()
	t.Cleanup(func() { close(done); wg.Wait() })
	return c
}

func running(stage time.Time) state.Detection {
	return state.Detection{Status: state.DetectionRunning, UpdatedAt: stage}
}

// TestR261_AwaitReturnsWhenDetectionMovesOn asserts the long poll every
// surface shares (R-261, issue #80): a wait on a running detection returns as
// soon as it reaches a new stage, and again as soon as it finishes.
func TestR261_AwaitReturnsWhenDetectionMovesOn(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	t.Run("a new stage", func(t *testing.T) {
		t.Parallel()
		r := &scriptedReader{script: []state.Detection{
			running(start), running(start), running(start.Add(time.Second)),
		}}
		got, err := Await(context.Background(), r, ticking(t), "app_1", 30*time.Second)
		require.NoError(t, err)
		require.Equal(t, start.Add(time.Second), got.UpdatedAt)
		require.EqualValues(t, 3, r.reads.Load(), "returned on the read that saw the change")
	})

	t.Run("an outcome", func(t *testing.T) {
		t.Parallel()
		r := &scriptedReader{script: []state.Detection{
			running(start), {Status: state.DetectionReady, UpdatedAt: start.Add(time.Minute)},
		}}
		got, err := Await(context.Background(), r, ticking(t), "app_1", 30*time.Second)
		require.NoError(t, err)
		require.Equal(t, state.DetectionReady, got.Status)
	})
}

// A detection that is not running has nothing to wait for, and neither does a
// request that asked for no wait: both are answered from the first read.
func TestAwaitAnswersAtOnceWhenThereIsNothingToWaitFor(t *testing.T) {
	t.Parallel()
	c := clock.NewFake(time.Time{})
	before := c.Now()

	finished := &scriptedReader{script: []state.Detection{{Status: state.DetectionNeedsAnswers}}}
	got, err := Await(context.Background(), finished, c, "app_1", 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, state.DetectionNeedsAnswers, got.Status)
	require.EqualValues(t, 1, finished.reads.Load())

	noWait := &scriptedReader{script: []state.Detection{running(before)}}
	got, err = Await(context.Background(), noWait, c, "app_1", 0)
	require.NoError(t, err)
	require.Equal(t, state.DetectionRunning, got.Status)
	require.EqualValues(t, 1, noWait.reads.Load())
	require.Equal(t, before, c.Now(), "no time passed")
}

// A wait that sees no change gives up when it said it would, with the
// detection as it stands, and never holds a request longer than MaxWait.
func TestAwaitGivesUpAfterTheWaitAndNeverPastMaxWait(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		wait time.Duration
		held time.Duration
	}{
		{"as asked", 3 * time.Second, 3 * time.Second},
		{"clamped", 10 * time.Minute, MaxWait},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := ticking(t)
			began := c.Now()
			r := &scriptedReader{script: []state.Detection{running(start)}}

			got, err := Await(context.Background(), r, c, "app_1", tc.wait)
			require.NoError(t, err)
			require.Equal(t, state.DetectionRunning, got.Status)
			held := c.Now().Sub(began)
			require.GreaterOrEqual(t, held, tc.held)
			// The ticker can move the clock on a few times between Await
			// returning and the read here; ten intervals is ample.
			require.Less(t, held, tc.held+10*pollInterval)
		})
	}
}

// A caller that goes away ends the wait, answered with what was last read
// rather than an error nobody is left to receive.
func TestAwaitEndsWhenTheCallerGoesAway(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &scriptedReader{script: []state.Detection{running(time.Time{})}}

	got, err := Await(ctx, r, clock.NewFake(time.Time{}), "app_1", 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, state.DetectionRunning, got.Status)
}
