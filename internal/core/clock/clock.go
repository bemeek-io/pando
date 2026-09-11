// Package clock provides the time source core depends on.
//
// The reconciler's backoff (R-149) and the give-up window (R-150) are both time
// dependent, and a test that proves a ten-failure threshold inside a
// thirty-minute window must not take thirty minutes. Every core component takes
// a Clock rather than calling time.Now.
package clock

import (
	"sync"
	"time"
)

// Clock is a time source.
type Clock interface {
	// Now returns the current time, always UTC (design 00 §3.5).
	Now() time.Time
	// Since is shorthand for Now().Sub(t).
	Since(t time.Time) time.Duration
	// After behaves like time.After.
	After(d time.Duration) <-chan time.Time
	// Sleep blocks for d.
	Sleep(d time.Duration)
}

// System is the real clock.
type System struct{}

func (System) Now() time.Time                         { return time.Now().UTC() }
func (System) Since(t time.Time) time.Duration        { return time.Since(t) }
func (System) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (System) Sleep(d time.Duration)                  { time.Sleep(d) }

// Fake is a manually advanced clock for tests.
//
// After returns a channel that fires when the fake clock is advanced past the
// deadline, so a test drives elapsed time explicitly instead of sleeping.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	deadline time.Time
	ch       chan time.Time
}

// NewFake returns a Fake set to t, or to a fixed arbitrary instant if t is zero.
func NewFake(t time.Time) *Fake {
	if t.IsZero() {
		t = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &Fake{now: t.UTC()}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) Since(t time.Time) time.Duration { return f.Now().Sub(t) }

func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	ch := make(chan time.Time, 1)
	deadline := f.now.Add(d)
	if !deadline.After(f.now) {
		ch <- f.now
		return ch
	}
	f.waiters = append(f.waiters, waiter{deadline: deadline, ch: ch})
	return ch
}

// Sleep on a Fake returns immediately after advancing the clock. A test that
// genuinely wants to block should use After and Advance from another goroutine.
func (f *Fake) Sleep(d time.Duration) { f.Advance(d) }

// Advance moves the clock forward and fires any waiter whose deadline passed.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	now := f.now

	var still []waiter
	var fired []waiter
	for _, w := range f.waiters {
		if !w.deadline.After(now) {
			fired = append(fired, w)
		} else {
			still = append(still, w)
		}
	}
	f.waiters = still
	f.mu.Unlock()

	for _, w := range fired {
		w.ch <- now
	}
}

// Set moves the clock to an absolute time. It fires waiters the same way
// Advance does when moving forward, and fires none when moving backward.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	d := t.UTC().Sub(f.now)
	f.mu.Unlock()
	if d > 0 {
		f.Advance(d)
		return
	}
	f.mu.Lock()
	f.now = t.UTC()
	f.mu.Unlock()
}

var (
	_ Clock = System{}
	_ Clock = (*Fake)(nil)
)
