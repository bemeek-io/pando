package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/clock"
)

func TestSystemClockIsUTC(t *testing.T) {
	require.Equal(t, time.UTC, clock.System{}.Now().Location())
}

func TestFakeAdvances(t *testing.T) {
	f := clock.NewFake(time.Time{})
	start := f.Now()

	f.Advance(90 * time.Second)
	require.Equal(t, 90*time.Second, f.Since(start))
	require.Equal(t, start.Add(90*time.Second), f.Now())
}

// The reconciler's backoff caps at five minutes and its give-up window is thirty
// (R-149, R-150). Both must be testable without waiting.
func TestFakeAfterFiresOnAdvanceNotOnWallClock(t *testing.T) {
	f := clock.NewFake(time.Time{})
	ch := f.After(30 * time.Minute)

	select {
	case <-ch:
		t.Fatal("fired before the clock advanced")
	default:
	}

	f.Advance(29 * time.Minute)
	select {
	case <-ch:
		t.Fatal("fired early")
	default:
	}

	f.Advance(time.Minute)
	select {
	case got := <-ch:
		require.Equal(t, f.Now(), got)
	case <-time.After(time.Second):
		t.Fatal("did not fire after the deadline passed")
	}
}

func TestFakeAfterZeroFiresImmediately(t *testing.T) {
	f := clock.NewFake(time.Time{})
	select {
	case <-f.After(0):
	case <-time.After(time.Second):
		t.Fatal("a zero duration should fire immediately")
	}
}
