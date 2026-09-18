package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// In-package: these are the small decisions a deploy makes on the way past —
// whether to build at all, what to tell someone when it failed, and what the
// runtime says is actually running. Each is unexported and each is wrong in a
// way that is quiet.

// R-221: an app with no health check is running, not perpetually degraded. "No
// signal" and "unhealthy" must not collapse.
func TestR221_NoHealthConfiguredMeansNoHealthPlan(t *testing.T) {
	s := &spec.AppSpec{}
	require.Nil(t, healthPlan(s, spec.Workload{Name: "web", Primary: true}))

	// A source with nothing to probe is still no signal.
	require.Nil(t, healthPlan(&spec.AppSpec{
		Health: spec.Health{Source: spec.HealthSource("http")},
	}, spec.Workload{Name: "web", Primary: true}))
}

// The app-level check describes the app's own endpoint, so it belongs to the
// primary workload and to nothing else.
func TestTheAppLevelHealthCheckAppliesToThePrimaryWorkloadOnly(t *testing.T) {
	s := &spec.AppSpec{Health: spec.Health{
		Source: spec.HealthSource("http"), Path: "/healthz", Port: 3000,
	}}

	plan := healthPlan(s, spec.Workload{Name: "web", Primary: true})
	require.NotNil(t, plan)
	require.Equal(t, "/healthz", plan.Path)
	require.Equal(t, 3000, plan.Port)

	require.Nil(t, healthPlan(s, spec.Workload{Name: "worker"}),
		"a worker is not the app's endpoint")
}

// A workload's own check wins, and applies whether or not it is primary.
func TestAWorkloadsOwnHealthCheckWinsOverTheApps(t *testing.T) {
	s := &spec.AppSpec{Health: spec.Health{
		Source: spec.HealthSource("http"), Path: "/healthz", Port: 3000,
	}}
	w := spec.Workload{Name: "worker", Health: &spec.Healthcheck{
		Command: []string{"pg_isready"}, IntervalSeconds: 5, TimeoutSeconds: 2, Retries: 10,
	}}

	plan := healthPlan(s, w)
	require.NotNil(t, plan)
	require.Equal(t, []string{"pg_isready"}, plan.Command)
	require.Empty(t, plan.Path, "the app's path does not leak into a workload's own check")
	require.Equal(t, 5, plan.IntervalSeconds)
	require.Equal(t, 2, plan.TimeoutSeconds)
	require.Equal(t, 10, plan.Retries)
}

// A check that states only what it cares about gets sensible timings rather
// than zeros, which a runtime would read as "probe constantly" or "never".
func TestUnstatedHealthTimingsFallBackRatherThanBeingZero(t *testing.T) {
	plan := healthPlan(&spec.AppSpec{}, spec.Workload{
		Name: "web", Health: &spec.Healthcheck{Path: "/healthz", Port: 3000},
	})
	require.NotNil(t, plan)
	require.Equal(t, 30, plan.IntervalSeconds)
	require.Equal(t, 5, plan.TimeoutSeconds)
	require.Equal(t, 3, plan.Retries)

	require.Equal(t, 7, orDefault(7, 30))
	require.Equal(t, 30, orDefault(0, 30))
	require.Equal(t, 30, orDefault(-1, 30), "a negative interval is not an interval")
}

// A prebuilt image is run as it is, and an image source has nothing to build
// from. Building either would be work nobody asked for, against a source that
// is not there.
func TestNothingIsBuiltForAnImageThatAlreadyExists(t *testing.T) {
	require.False(t, needsBuild(&spec.AppSpec{
		Build: spec.Build{Strategy: spec.BuildPrebuilt},
	}))
	require.False(t, needsBuild(&spec.AppSpec{
		Source: spec.Source{Type: spec.SourceImage},
	}))

	require.True(t, needsBuild(&spec.AppSpec{
		Source: spec.Source{Type: spec.SourceGit},
		Build:  spec.Build{Strategy: spec.BuildDockerfile},
	}))
	require.True(t, needsBuild(&spec.AppSpec{
		Source: spec.Source{Type: spec.SourceUpload},
		Build:  spec.Build{Strategy: spec.BuildBuildpack},
	}))
}

// Build failures are the one place an internal cause is worth showing: the
// caller is looking at their own build, and "the build failed" with nothing
// further is unactionable.
func TestABuildFailureCarriesItsUnderlyingCause(t *testing.T) {
	cause := errors.New("exit status 1: npm ERR! missing script: build")

	require.Equal(t, cause.Error(),
		detailOf(errs.Wrap(errs.BuildFailed, "The build failed.", cause)))

	// An envelope with nothing wrapped has no further detail to give, and
	// inventing one would be worse than saying nothing.
	require.Empty(t, detailOf(errs.New(errs.BuildFailed, "The build failed.")))

	// An error carrying no envelope at all is still rendered rather than lost.
	require.Equal(t, "connection refused", detailOf(errors.New("connection refused")))
}

// The message is the one held to the R-105 standard; an adapter under no
// obligation to produce an envelope must not leave the caller with nothing.
func TestTheDeployMessageIsAlwaysSomethingAPersonCanRead(t *testing.T) {
	require.Equal(t, "The build timed out after 30 minutes.",
		messageOf(errs.New(errs.BuildTimeout, "The build timed out after 30 minutes.")))

	require.Equal(t, "The deploy failed.", messageOf(errors.New("dial unix /var/run/docker.sock")))
}

func TestFirstNonEmptyPicksTheFirstThingThatWasActuallySet(t *testing.T) {
	require.Equal(t, "explicit", firstNonEmpty("", "explicit", "fallback"))
	require.Equal(t, "fallback", firstNonEmpty("", "", "fallback"))
	require.Empty(t, firstNonEmpty("", "", ""))
	require.Empty(t, firstNonEmpty())
}

func TestACommitIsShortenedForDisplayWithoutBeingCorrupted(t *testing.T) {
	require.Equal(t, "1234abcd", short("1234abcdef567890"))
	require.Equal(t, "1234abcd", short("1234abcd"), "exactly eight is already short")
	require.Equal(t, "abc", short("abc"), "an upload has no commit, and a short one is not padded")
	require.Empty(t, short(""))
}

// observing is a runtime that answers Observe and nothing else.
type observing struct {
	api.RuntimeAdapter
	bundle api.ObservedBundle
	err    error
}

func (o observing) Observe(context.Context, api.BundleRef) (api.ObservedBundle, error) {
	return o.bundle, o.err
}

// The runtime's own observation rather than anything Pando computed: a tag can
// move, and the only thing worth comparing against a running container later is
// what was running now.
func TestThePrimaryDigestComesFromTheRuntime(t *testing.T) {
	got := primaryDigest(context.Background(), observing{bundle: api.ObservedBundle{
		Workloads: []api.ObservedWorkload{
			{Name: "worker", Present: true, Running: false, ImageDigest: "sha256:stopped"},
			{Name: "web", Present: true, Running: true, ImageDigest: "sha256:running"},
		},
	}}, "app_01HQ8")

	require.Equal(t, "sha256:running", got, "a stopped workload's image is not what is running")
}

// Empty on any failure, which makes image drift undetectable rather than making
// every workload look wrong.
func TestAnUnobservableRuntimeYieldsNoDigestRatherThanAWrongOne(t *testing.T) {
	ctx := context.Background()

	require.Empty(t, primaryDigest(ctx, observing{err: errors.New("daemon is not running")}, "app_01HQ8"))
	require.Empty(t, primaryDigest(ctx, observing{bundle: api.ObservedBundle{}}, "app_01HQ8"))

	// Running, but the runtime does not report a digest.
	require.Empty(t, primaryDigest(ctx, observing{bundle: api.ObservedBundle{
		Workloads: []api.ObservedWorkload{{Name: "web", Present: true, Running: true}},
	}}, "app_01HQ8"))
}

// TestR105_AFailedStepSaysWhyInTheLogSomebodyIsWatching asserts R-105.
//
// `!! apply failed: Could not create "proxy".` was the whole of what a real
// deploy said. The reason — a mount the Docker daemon refused — was in the
// error that message wrapped, and went only to the server's log. The person
// deploying has the deploy log and nothing else.
func TestR105_AFailedStepSaysWhyInTheLogSomebodyIsWatching(t *testing.T) {
	err := errs.Wrap(errs.AdapterFailed, `Could not create "proxy".`,
		errors.New("Error response from daemon: source /var/lib/docker/x is not directory")).
		WithRemedy("Remove that mount in the app's storage settings.")

	var log strings.Builder
	writeFailure(&log, "apply failed: "+messageOf(err), err)

	out := log.String()
	require.Contains(t, out, `!! apply failed: Could not create "proxy".`)
	require.Contains(t, out, "is not directory", "why")
	require.Contains(t, out, "storage settings", "and what to do")
}

// A cause already contained in the headline is not repeated. Two lines saying
// the same thing read as two problems.
func TestAFailureDoesNotSayTheSameThingTwice(t *testing.T) {
	err := errs.Newf(errs.PlanSlotUnfilled, "This app needs a PostgreSQL database.")

	var log strings.Builder
	writeFailure(&log, messageOf(err), err)

	require.Equal(t, "\n!! This app needs a PostgreSQL database.\n", log.String())
}
