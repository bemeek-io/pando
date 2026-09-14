package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/clock"
)

// Design 00 §3.5: time is UTC everywhere.
func TestTheSystemClockIsUTC(t *testing.T) {
	var c clock.Clock = clock.System{}

	now := c.Now()
	require.Equal(t, time.UTC, now.Location())
	require.WithinDuration(t, time.Now(), now, time.Second)
}

func TestSystemSinceMeasuresElapsedTime(t *testing.T) {
	c := clock.System{}

	past := time.Now().Add(-time.Hour)
	require.InDelta(t, time.Hour.Seconds(), c.Since(past).Seconds(), 1)
	require.Positive(t, c.Since(time.Now().Add(-time.Millisecond)))
}

func TestSystemAfterFires(t *testing.T) {
	c := clock.System{}

	select {
	case fired := <-c.After(time.Millisecond):
		require.False(t, fired.IsZero())
	case <-time.After(5 * time.Second):
		t.Fatal("the timer never fired")
	}
}

func TestSystemSleepBlocksForAtLeastTheDuration(t *testing.T) {
	c := clock.System{}

	started := time.Now()
	c.Sleep(2 * time.Millisecond)
	require.GreaterOrEqual(t, time.Since(started), 2*time.Millisecond)
}

// Sleep on a Fake returns immediately after advancing the clock: a test that
// proves a ten-failure threshold inside a thirty-minute window must not take
// thirty minutes.
func TestFakeSleepAdvancesInsteadOfWaiting(t *testing.T) {
	f := clock.NewFake(time.Time{})
	before := f.Now()

	started := time.Now()
	f.Sleep(30 * time.Minute)

	require.Less(t, time.Since(started), time.Second, "no real time passed")
	require.Equal(t, before.Add(30*time.Minute), f.Now())
}

func TestSetMovesToAnAbsoluteTimeInEitherDirection(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)

	forward := start.Add(time.Hour)
	f.Set(forward)
	require.Equal(t, forward, f.Now())

	backward := start.Add(-time.Hour)
	f.Set(backward)
	require.Equal(t, backward, f.Now())

	// Whatever zone it is given, the clock reports UTC.
	f.Set(time.Date(2026, 9, 14, 12, 0, 0, 0, time.FixedZone("elsewhere", 3600)))
	require.Equal(t, time.UTC, f.Now().Location())
	require.Equal(t, 11, f.Now().Hour())
}

// Moving forward fires a waiter whose deadline passed; moving backward fires
// none, because time did not reach them.
func TestSetFiresWaitersOnlyWhenMovingForward(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	f := clock.NewFake(start)
	ch := f.After(time.Minute)
	f.Set(start.Add(2 * time.Minute))

	select {
	case fired := <-ch:
		require.Equal(t, start.Add(2*time.Minute), fired)
	default:
		t.Fatal("a waiter whose deadline passed did not fire")
	}

	f = clock.NewFake(start)
	pending := f.After(time.Minute)
	f.Set(start.Add(-time.Minute))

	select {
	case <-pending:
		t.Fatal("a waiter fired while the clock moved backward")
	default:
	}
}

func TestSetToTheSameInstantIsANoOp(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)

	f.Set(start)
	require.Equal(t, start, f.Now())
}

func TestFakeSinceUsesTheFakeNow(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)

	require.Equal(t, time.Hour, f.Since(start.Add(-time.Hour)))
	f.Advance(time.Minute)
	require.Equal(t, time.Hour+time.Minute, f.Since(start.Add(-time.Hour)))
}
