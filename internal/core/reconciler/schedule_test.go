package reconciler

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/clock"
	"github.com/bemeek-io/pando/internal/errs"
)

// These are in-package because the schedule and the health reading are
// unexported decisions, and they are the two things most worth pinning down:
// one decides when Pando gives up on an app (R-150), the other decides whether
// it ever should (R-221).

func at(t time.Time) *clock.Fake { return clock.NewFake(t) }

var noon = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

// R-149: retry with backoff, capped at five minutes. The first correction is
// immediate — a container killed once should come back now, not in five seconds.
func TestR149_TheDefaultBackoffStartsImmediateAndCapsAtFiveMinutes(t *testing.T) {
	r := &Reconciler{Clock: at(noon)}

	require.Equal(t, DefaultBackoff, r.backoff())
	require.Equal(t, time.Duration(0), DefaultBackoff[0], "the first retry is immediate")
	require.Equal(t, 5*time.Minute, DefaultBackoff[len(DefaultBackoff)-1])

	for failures, want := range DefaultBackoff {
		require.Equal(t, noon.Add(want), r.nextAttempt(failures), "after %d failures", failures)
	}
}

// The cap matters more than the curve: an app that cannot start must not be
// retried forever at speed, and must still be retried.
func TestTheScheduleHoldsAtTheCapRatherThanRunningOff(t *testing.T) {
	r := &Reconciler{Clock: at(noon)}
	capped := DefaultBackoff[len(DefaultBackoff)-1]

	for _, failures := range []int{len(DefaultBackoff), 50, 10000} {
		require.Equal(t, noon.Add(capped), r.nextAttempt(failures))
	}
}

// Configurable because the acceptance test for R-151 has to wait out the real
// schedule, and at production numbers that is most of the suite. Compressing it
// changes how long each step waits and nothing about which step comes next.
func TestAConfiguredScheduleReplacesTheDefaultEntirely(t *testing.T) {
	r := &Reconciler{
		Clock:            at(noon),
		Backoff:          []time.Duration{0, time.Millisecond},
		FailureThreshold: 3,
		FailureWindow:    time.Minute,
	}

	require.Equal(t, []time.Duration{0, time.Millisecond}, r.backoff())
	require.Equal(t, 3, r.failureThreshold())
	require.Equal(t, time.Minute, r.failureWindow())
	require.Equal(t, noon.Add(time.Millisecond), r.nextAttempt(7), "still capped at the last step")
}

// R-150's give-up rule, with the defaults used when nothing overrides them.
func TestR150_TheDefaultGiveUpRuleAppliesWhenNothingIsConfigured(t *testing.T) {
	r := &Reconciler{}
	require.Equal(t, DefaultFailureThreshold, r.failureThreshold())
	require.Equal(t, DefaultFailureWindow, r.failureWindow())
}

// Not enforced — a floor would make the schedule untestable end to end, which
// is the problem the configurability exists to solve.
func TestMinProductionCapIsAThresholdForSayingSoNotALimit(t *testing.T) {
	require.Equal(t, 30*time.Second, MinProductionCap)

	r := &Reconciler{Clock: at(noon), Backoff: []time.Duration{time.Millisecond}}
	require.Equal(t, noon.Add(time.Millisecond), r.nextAttempt(0),
		"a schedule below the threshold is still honored")
}

func TestNowFallsBackToTheWallClockWhenNoneIsInjected(t *testing.T) {
	require.WithinDuration(t, time.Now().UTC(), (&Reconciler{}).now(), time.Second)
	require.Equal(t, noon, (&Reconciler{Clock: at(noon)}).now())
}

// --- health ----------------------------------------------------------------

func workload(name string) api.ObservedWorkload {
	return api.ObservedWorkload{Name: name, Present: true, Running: true}
}

// R-221: a nil Healthy is no signal, which is not unhealthy. An app with no
// health check is running, not perpetually degraded.
func TestR221_NoHealthSignalIsNotUnhealthy(t *testing.T) {
	require.True(t, observedHealthy(api.ObservedBundle{
		Workloads: []api.ObservedWorkload{workload("web")},
	}, noon))

	unhealthy := false
	require.False(t, observedHealthy(api.ObservedBundle{
		Workloads: []api.ObservedWorkload{{Name: "web", Present: true, Running: true, Healthy: &unhealthy}},
	}, noon))

	healthy := true
	require.True(t, observedHealthy(api.ObservedBundle{
		Workloads: []api.ObservedWorkload{{Name: "web", Present: true, Running: true, Healthy: &healthy}},
	}, noon))
}

func TestAWorkloadThatIsNotRunningIsNotHealthy(t *testing.T) {
	require.False(t, observedHealthy(api.ObservedBundle{
		Workloads: []api.ObservedWorkload{{Name: "web", Present: true, Running: false}},
	}, noon))
}

// Every workload has to be up. One down means the app is not healthy, however
// many others are.
func TestOneUnhealthyWorkloadIsEnough(t *testing.T) {
	down := false
	require.False(t, observedHealthy(api.ObservedBundle{
		Workloads: []api.ObservedWorkload{
			workload("web"),
			{Name: "worker", Present: true, Running: true, Healthy: &down},
		},
	}, noon))
}

// A bundle with nothing in it has nothing failing. The caller decides whether
// an empty bundle is what it wanted; this function only reads what is there.
func TestAnEmptyBundleIsVacuouslyHealthy(t *testing.T) {
	require.True(t, observedHealthy(api.ObservedBundle{}, noon))
}

// Restarting counts as not healthy, and getting this wrong is subtle: a
// crash-looping app is briefly Running between crashes, and a tick landing in
// that window would read it as recovered and clear the failure count. The app
// would then never reach `failed`.
func TestACrashLoopingWorkloadIsNotReadAsRecovered(t *testing.T) {
	// The runtime saying so, first. Docker reports Running and Restarting
	// together for a container in a crash loop.
	require.False(t, observedHealthy(api.ObservedBundle{
		Workloads: []api.ObservedWorkload{
			{Name: "web", Present: true, Running: true, Restarting: true},
		},
	}, noon))

	// For a runtime that cannot tell: restarted before, and started again
	// moments ago.
	require.False(t, observedHealthy(api.ObservedBundle{
		Workloads: []api.ObservedWorkload{
			{Name: "web", Present: true, Running: true, RestartCount: 4,
				StartedAt: noon.Add(-5 * time.Second)},
		},
	}, noon))
}

// Once it stays up past the settle window it is treated as healthy, which is
// exactly the distinction being drawn.
func TestAWorkloadThatHasStayedUpPastTheSettleWindowCountsAsRecovered(t *testing.T) {
	require.True(t, observedHealthy(api.ObservedBundle{
		Workloads: []api.ObservedWorkload{
			{Name: "web", Present: true, Running: true, RestartCount: 4,
				StartedAt: noon.Add(-RestartSettleWindow - time.Second)},
		},
	}, noon))

	// A workload that has never restarted is not judged by the window at all.
	require.True(t, observedHealthy(api.ObservedBundle{
		Workloads: []api.ObservedWorkload{
			{Name: "web", Present: true, Running: true, RestartCount: 0, StartedAt: noon},
		},
	}, noon))

	// Nor is one whose start time the runtime does not report.
	require.True(t, observedHealthy(api.ObservedBundle{
		Workloads: []api.ObservedWorkload{
			{Name: "web", Present: true, Running: true, RestartCount: 4},
		},
	}, noon))
}

// --- rendering -------------------------------------------------------------

// An adapter is ordinary Go code and may return anything. Dereferencing a nil
// envelope crashed the reconciler goroutine, and a panic in a goroutine takes
// the process with it: one adapter returning a plain error would stop Pando,
// including for every app that was fine.
func TestReasonSurvivesAnErrorCarryingNoEnvelope(t *testing.T) {
	require.Empty(t, reason(nil))
	require.Equal(t, "connection refused", reason(errors.New("connection refused")))
	require.Equal(t, "The runtime is not reachable.",
		reason(errs.New(errs.AdapterUnavailable, "The runtime is not reachable.")))

	// Wrapped, which is how it actually arrives.
	require.Equal(t, "The runtime is not reachable.",
		reason(errs.Wrap(errs.AdapterUnavailable, "The runtime is not reachable.",
			errors.New("dial unix /var/run/docker.sock"))))
}

func TestItoaRendersTheCountsThatGoIntoAuditDetail(t *testing.T) {
	for n, want := range map[int]string{0: "0", 1: "1", 9: "9", 10: "10", 4095: "4095"} {
		require.Equal(t, want, itoa(n))
	}
}
