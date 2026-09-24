package deploy

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// waitForHealth reads the wall clock and waits on time.After, with a settle
// period of seconds and a deadline of minutes. These tests run it inside a
// synctest bubble, where that clock is virtual and advances only when every
// goroutine is blocked, so the whole two-minute window passes at once.

// scriptedRuntime answers Observe from a function of the call number and
// records every Apply and Logs call.
type scriptedRuntime struct {
	api.RuntimeAdapter
	observe  func(call int) (api.ObservedBundle, error)
	observed int
	applied  []time.Time
	logsFor  string
	onApply  func()
}

func (r *scriptedRuntime) Observe(context.Context, api.BundleRef) (api.ObservedBundle, error) {
	r.observed++
	return r.observe(r.observed)
}

func (r *scriptedRuntime) Apply(context.Context, api.BundlePlan) (api.BundleHandle, error) {
	r.applied = append(r.applied, time.Now())
	if r.onApply != nil {
		r.onApply()
	}
	return api.BundleHandle{}, nil
}

func (r *scriptedRuntime) Logs(_ context.Context, ref api.WorkloadRef, _ api.LogOptions) (io.ReadCloser, error) {
	r.logsFor = ref.Workload
	return io.NopCloser(strings.NewReader("Error: nginx.conf line 3: unknown directive\n")), nil
}

var healthBundle = api.BundlePlan{Workloads: []api.WorkloadPlan{{Name: "web", Exposed: true}, {Name: "worker"}}}

func running(names ...string) api.ObservedBundle {
	var b api.ObservedBundle
	for _, n := range names {
		b.Workloads = append(b.Workloads, api.ObservedWorkload{Name: n, Present: true, Running: true})
	}
	return b
}

func exitedWeb() api.ObservedBundle {
	code := 1
	return api.ObservedBundle{Workloads: []api.ObservedWorkload{
		{Name: "web", Present: true, ExitCode: &code},
		{Name: "worker", Present: true, Running: true},
	}}
}

// An app that is up at the first look is not reported deployed on that one
// glimpse: it has to stay up for the settle period (issue #55).
func TestR146_AnAppIsRunningOnlyAfterStayingUpForTheSettlePeriod(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := &scriptedRuntime{observe: func(int) (api.ObservedBundle, error) {
			return running("web", "worker"), nil
		}}
		start := time.Now()

		healthy, err := (&Runner{}).waitForHealth(t.Context(), rt, "app_01HQ8", healthBundle, io.Discard)
		require.NoError(t, err)
		require.True(t, healthy)
		require.GreaterOrEqual(t, time.Since(start), exitSettle)
		require.Greater(t, rt.observed, 1, "looked more than once")
		require.Empty(t, rt.applied, "nothing stopped, so nothing was started again")
	})
}

// TestR146_APartThatStoppedIsStartedAgainWhileTheDeployWaits asserts R-146.
//
// A compose backend that exits because its database was still starting is
// started again by re-applying the same plan, and the settle period counts from
// that restart rather than from the first look.
func TestR146_APartThatStoppedIsStartedAgainWhileTheDeployWaits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := &scriptedRuntime{}
		rt.observe = func(int) (api.ObservedBundle, error) {
			if len(rt.applied) == 0 {
				return exitedWeb(), nil
			}
			return running("web", "worker"), nil
		}

		healthy, err := (&Runner{}).waitForHealth(t.Context(), rt, "app_01HQ8", healthBundle, io.Discard)
		require.NoError(t, err)
		require.True(t, healthy)
		require.Len(t, rt.applied, 1)
		require.GreaterOrEqual(t, time.Since(rt.applied[0]), exitSettle,
			"up for the settle period after the restart, not before it")
	})
}

// TestR146_AnAppWhosePrimaryNeverStaysUpFailsWithItsOwnOutput asserts R-146.
//
// Started again for the whole window and still stopped: the deploy fails with
// STATE_APP_EXITED and the app's last output in the log, rather than reporting
// a degraded app that is not running at all.
func TestR146_AnAppWhosePrimaryNeverStaysUpFailsWithItsOwnOutput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := &scriptedRuntime{observe: func(int) (api.ObservedBundle, error) { return exitedWeb(), nil }}
		start := time.Now()

		var log strings.Builder
		healthy, err := (&Runner{}).waitForHealth(t.Context(), rt, "app_01HQ8", healthBundle, &log)
		require.False(t, healthy)

		e := errs.As(err)
		require.NotNil(t, e)
		require.Equal(t, errs.StateAppExited, e.Code)
		require.Contains(t, e.Message, "exit code 1")
		require.NotEmpty(t, e.Remedy)

		require.Equal(t, "web", rt.logsFor)
		require.Contains(t, log.String(), `the last lines "web" wrote before it stopped`)
		require.Contains(t, log.String(), "unknown directive")

		require.GreaterOrEqual(t, time.Since(start), 2*time.Minute)
		require.Greater(t, len(rt.applied), 2, "started again more than once while waiting")
		for i := 1; i < len(rt.applied); i++ {
			require.GreaterOrEqual(t, rt.applied[i].Sub(rt.applied[i-1]), exitRestartEvery,
				"restarts are spaced out, not a tight loop")
		}
	})
}

// An app that runs but never reports healthy is degraded, not failed: that is
// recoverable and still being worked (R-147).
func TestAnAppThatNeverReportsHealthyIsDegradedNotFailed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		unhealthy := false
		rt := &scriptedRuntime{observe: func(int) (api.ObservedBundle, error) {
			b := running("web", "worker")
			b.Workloads[0].Healthy = &unhealthy
			return b, nil
		}}

		healthy, err := (&Runner{}).waitForHealth(t.Context(), rt, "app_01HQ8", healthBundle, io.Discard)
		require.NoError(t, err)
		require.False(t, healthy)
	})
}

// Fewer workloads observed than planned is not running yet, even when all of
// those observed are up.
func TestAMissingWorkloadIsNotRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := &scriptedRuntime{observe: func(int) (api.ObservedBundle, error) { return running("web"), nil }}

		healthy, err := (&Runner{}).waitForHealth(t.Context(), rt, "app_01HQ8", healthBundle, io.Discard)
		require.NoError(t, err)
		require.False(t, healthy)
	})
}

func TestWaitingForHealthStopsOnAnObserveErrorOrCancellation(t *testing.T) {
	down := errors.New("daemon is not running")
	rt := &scriptedRuntime{observe: func(int) (api.ObservedBundle, error) { return api.ObservedBundle{}, down }}
	_, err := (&Runner{}).waitForHealth(t.Context(), rt, "app_01HQ8", healthBundle, io.Discard)
	require.ErrorIs(t, err, down)

	// The reconciler cancels a deploy whose spec changed underneath it.
	ctx, cancel := context.WithCancel(t.Context())
	rt = &scriptedRuntime{observe: func(int) (api.ObservedBundle, error) { return exitedWeb(), nil }, onApply: cancel}
	_, err = (&Runner{}).waitForHealth(ctx, rt, "app_01HQ8", healthBundle, io.Discard)
	require.ErrorIs(t, err, context.Canceled)
}

// capsRuntime reports fixed capabilities and fails any trial it is asked for.
type capsRuntime struct {
	api.RuntimeAdapter
	caps     api.RuntimeCapabilities
	trialErr error
	trials   int
}

func (r *capsRuntime) Capabilities(context.Context) (api.RuntimeCapabilities, error) {
	return r.caps, nil
}

func (r *capsRuntime) Trial(context.Context, api.TrialRequest) (api.TrialResult, error) {
	r.trials++
	return api.TrialResult{}, r.trialErr
}

// TestR097_APortCheckThatCannotRunLeavesTheAssumption asserts R-097.
//
// The check is an improvement on the assumed port, never a new way for a deploy
// to fail: a runtime that cannot watch, or a trial that errors, leaves the port
// as it was.
func TestR097_APortCheckThatCannotRunLeavesTheAssumption(t *testing.T) {
	ctx := context.Background()

	unable := &capsRuntime{}
	require.Equal(t, portCheck{}, checkPort(ctx, unable, assumedPortApp(8000), "img", "t1", io.Discard))
	require.Zero(t, unable.trials, "a runtime that cannot watch is not asked to")

	failing := &capsRuntime{
		caps:     api.RuntimeCapabilities{SupportsTrialRun: true, SupportsPortObservation: true},
		trialErr: errors.New("image not found"),
	}
	require.Equal(t, portCheck{}, checkPort(ctx, failing, assumedPortApp(8000), "img", "t1", io.Discard))
	require.Equal(t, 1, failing.trials)

	// Started, but seen listening nowhere: nothing better was learned.
	silent := &trialRuntime{result: api.TrialResult{Started: true}}
	require.Equal(t, portCheck{}, checkPort(ctx, silent, assumedPortApp(8000), "img", "t1", io.Discard))
}

// The trial is given the spec's plain values, and not a name with nothing in
// it; the app is watched the way it will run.
func TestThePortCheckPassesThePlainConfiguration(t *testing.T) {
	filled, empty := "info", ""
	app := assumedPortApp(8000)
	app.Workloads[0].Env = []spec.EnvEntry{
		{Key: "LOG_LEVEL", Value: &filled, Source: spec.EnvFromUser},
		{Key: "APP_BASE_URL", Value: &empty, Source: spec.EnvFromDetection},
		{Key: "DATABASE_URL", Source: spec.EnvFromUser},
	}

	rt := &trialRuntime{result: api.TrialResult{Started: true, ObservedPorts: []int{8000}}}
	checkPort(context.Background(), rt, app, "img", "t1", io.Discard)

	require.Equal(t, "info", rt.asked.Env["LOG_LEVEL"].Reveal())
	require.Equal(t, "8000", rt.asked.Env["PORT"].Reveal())
	require.NotContains(t, rt.asked.Env, "APP_BASE_URL")
	require.NotContains(t, rt.asked.Env, "DATABASE_URL")
	require.Equal(t, "img", rt.asked.Image)
	require.Equal(t, observeTimeout, rt.asked.Timeout)
}

// An app with no primary workload, or one with no port, has no guess to check.
func TestAnAppWithNoPortToGuessIsNotChecked(t *testing.T) {
	noPorts := assumedPortApp(8000)
	noPorts.Workloads[0].Ports = nil
	require.False(t, needsPortCheck(noPorts))

	noPrimary := assumedPortApp(8000)
	noPrimary.Workloads[0].Primary = false
	require.False(t, needsPortCheck(noPrimary))
}

// Only the primary workload's port is replaced by the observed one; a worker
// listed before it keeps its own.
func TestTheObservedPortReplacesOnlyThePrimarys(t *testing.T) {
	app := assumedPortApp(8000)
	app.Workloads = append([]spec.Workload{{
		Name: "worker", Ports: []spec.Port{{Number: 9000, Source: spec.PortUser}},
	}}, app.Workloads...)

	corrected := withObservedPort(app, 5000)
	require.Equal(t, 9000, corrected.Workloads[0].Ports[0].Number)
	require.Equal(t, 5000, corrected.Workloads[1].Ports[0].Number)
	require.Equal(t, spec.PortObserved, corrected.Workloads[1].Ports[0].Source)
}
