package reconciler_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/reconciler"
)

// want builds a plan. A workload with no image of its own gets one, because a
// real plan always names an image — the case where it does not is a deployment
// that recorded nothing per workload, and that has its own test below.
func want(workloads ...api.WorkloadPlan) api.BundlePlan {
	for i := range workloads {
		if workloads[i].Image == "" {
			workloads[i].Image = "example/" + workloads[i].Name + ":1"
		}
	}
	return api.BundlePlan{BundleID: "app_1", Workloads: workloads}
}

func running(name string, healthy *bool) api.ObservedWorkload {
	return api.ObservedWorkload{Name: name, Present: true, Running: true, Healthy: healthy}
}

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

// The dividing line (design 05 §2.1): the reconciler may create and start
// things; it may not destroy anything a human may have wanted.
func TestR148_EverythingReconcilableIsACreationOrAStart(t *testing.T) {
	exited := 1
	drift := reconciler.Classify(
		want(api.WorkloadPlan{Name: "web"}, api.WorkloadPlan{Name: "worker"}),
		api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{
			{Name: "worker", Present: true, Running: false, ExitCode: &exited},
		}},
		reconciler.Inputs{})

	require.Len(t, drift.Reconcilable, 2)
	require.Empty(t, drift.ReportOnly)

	kinds := map[string]bool{}
	for _, d := range drift.Reconcilable {
		kinds[d.Kind] = true
	}
	require.True(t, kinds[reconciler.DriftWorkloadMissing], "web is gone: recreating destroys nothing")
	require.True(t, kinds[reconciler.DriftWorkloadStopped], "worker exited: starting destroys nothing")
}

// R-203, and the most important line in the package.
//
// Recreating a volume that previously held data produces an empty volume and an
// app that comes up healthy having lost everything — the failure that looks
// exactly like success.
func TestR203_AVolumeThatHeldDataIsReportedNeverRecreated(t *testing.T) {
	plan := want(api.WorkloadPlan{Name: "web"})
	plan.Volumes = []api.VolumePlan{{VolumeID: "vol_data", Name: "data"}}

	drift := reconciler.Classify(plan,
		api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{running("web", nil)}},
		reconciler.Inputs{VolumesThatHeldData: map[string]bool{"vol_data": true}})

	require.Empty(t, drift.Reconcilable,
		"a volume that existed must never be recreated by the reconciler")
	require.Len(t, drift.ReportOnly, 1)
	require.Equal(t, reconciler.DriftVolumeLostData, drift.ReportOnly[0].Kind)
	require.Contains(t, drift.ReportOnly[0].Detail, "look healthy",
		"the message has to say why this is being left alone")
}

// A volume that has never existed cannot lose data by being created.
func TestAVolumeThatNeverExistedIsSafeToCreate(t *testing.T) {
	plan := want(api.WorkloadPlan{Name: "web"})
	plan.Volumes = []api.VolumePlan{{VolumeID: "vol_new", Name: "new"}}

	drift := reconciler.Classify(plan,
		api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{running("web", nil)}},
		reconciler.Inputs{VolumesThatHeldData: map[string]bool{"vol_new": false}})

	require.Empty(t, drift.ReportOnly)
	require.Len(t, drift.Reconcilable, 1)
	require.Equal(t, reconciler.DriftVolumeMissing, drift.Reconcilable[0].Kind)
}

// Someone put it there on purpose. Removing it is destructive and unrequested.
func TestAnUnrecognizedWorkloadIsReportedNotRemoved(t *testing.T) {
	drift := reconciler.Classify(
		want(api.WorkloadPlan{Name: "web"}),
		api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{
			running("web", nil), running("someones-debug-sidecar", nil),
		}},
		reconciler.Inputs{})

	require.Empty(t, drift.Reconcilable)
	require.Len(t, drift.ReportOnly, 1)
	require.Equal(t, reconciler.DriftUnknownWorkload, drift.ReportOnly[0].Kind)
	require.Equal(t, "someones-debug-sidecar", drift.ReportOnly[0].Workload)
}

// R-193: the one drift that cannot be observed, because ObservedWorkload
// carries no environment and deliberately never will.
func TestR193_AChangedEnvironmentFingerprintIsDrift(t *testing.T) {
	drift := reconciler.Classify(
		want(api.WorkloadPlan{Name: "web"}),
		api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{running("web", yes())}},
		reconciler.Inputs{AppliedEnvHash: "aaa", CurrentEnvHash: "bbb"})

	require.Len(t, drift.Reconcilable, 1)
	require.Equal(t, reconciler.DriftStaleEnvironment, drift.Reconcilable[0].Kind)
}

// An unanswerable question is not drift.
func TestAnUnknownFingerprintIsNotDrift(t *testing.T) {
	for _, in := range []reconciler.Inputs{
		{AppliedEnvHash: "", CurrentEnvHash: "bbb"},
		{AppliedEnvHash: "aaa", CurrentEnvHash: ""},
	} {
		drift := reconciler.Classify(
			want(api.WorkloadPlan{Name: "web"}),
			api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{running("web", yes())}},
			in)
		require.True(t, drift.None(),
			"not being able to compare is not the same as having found a difference")
	}
}

// An app matching its spec has no drift, whatever its health.
func TestHealthIsNotDrift(t *testing.T) {
	for _, health := range []*bool{yes(), no(), nil} {
		drift := reconciler.Classify(
			want(api.WorkloadPlan{Name: "web"}),
			api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{running("web", health)}},
			reconciler.Inputs{})
		require.True(t, drift.None(),
			"an unhealthy app still matches its spec; nothing needs converging")
	}
}

// A missing route is reconcilable — re-ensuring one creates nothing destructive.
func TestAMissingRouteIsReconcilable(t *testing.T) {
	drift := reconciler.Classify(
		want(api.WorkloadPlan{Name: "web"}),
		api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{running("web", yes())}},
		reconciler.Inputs{RouteExpected: true, RoutePresent: false})

	require.Len(t, drift.Reconcilable, 1)
	require.Equal(t, reconciler.DriftRouteMissing, drift.Reconcilable[0].Kind)
}

// A workload running an image nobody deployed is drift the reconciler corrects.
//
// Recreating it is a creation, not a destruction: the container that is there
// is replaced by the one the spec asks for, which is the same operation as
// restoring a missing one.
func TestAWorkloadRunningTheWrongImageIsReconcilable(t *testing.T) {
	drift := reconciler.Classify(
		want(api.WorkloadPlan{Name: "web", Image: "example/app:1"}),
		api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{
			{Name: "web", Present: true, Running: true, ImageDigest: "sha256:beef"},
		}},
		reconciler.Inputs{ExpectedDigests: map[string]string{"web": "sha256:cafe"}})

	require.Len(t, drift.Reconcilable, 1)
	require.Equal(t, reconciler.DriftWrongImage, drift.Reconcilable[0].Kind)
	require.Contains(t, drift.Reconcilable[0].Detail, "beef")
	require.Contains(t, drift.Reconcilable[0].Detail, "cafe")
}

// An unknown expected digest makes image drift undetectable, rather than making
// every workload look wrong.
func TestAnUnknownExpectedDigestIsNotDrift(t *testing.T) {
	drift := reconciler.Classify(
		want(api.WorkloadPlan{Name: "web"}),
		api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{
			{Name: "web", Present: true, Running: true, ImageDigest: "sha256:beef"},
		}},
		reconciler.Inputs{ExpectedDigest: ""})

	require.True(t, drift.None())
}

// TestR148_EachWorkloadIsComparedAgainstItsOwnImage asserts R-148.
//
// An app's parts can be built separately — a compose file with an application
// and a proxy builds one and pulls the other — and one recorded digest compared
// against every workload made every part but one look permanently wrong.
//
// What followed was worse than a wrong report. "Wrong image" is reconcilable,
// so the loop recreated the application container from the plan's image, and
// the plan had the same single image in it: the app was replaced by a second
// copy of its own proxy, fifteen seconds after every deploy. The app's site was
// unreachable and Pando called it degraded.
func TestR148_EachWorkloadIsComparedAgainstItsOwnImage(t *testing.T) {
	drift := reconciler.Classify(
		want(
			api.WorkloadPlan{Name: "app", Image: "pando/app:latest"},
			api.WorkloadPlan{Name: "proxy", Image: "caddy:2-alpine"},
		),
		api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{
			{Name: "app", Present: true, Running: true, ImageDigest: "sha256:theapp"},
			{Name: "proxy", Present: true, Running: true, ImageDigest: "sha256:caddy"},
		}},
		reconciler.Inputs{ExpectedDigests: map[string]string{
			"app":   "sha256:theapp",
			"proxy": "sha256:caddy",
		}})

	require.True(t, drift.None(), "both parts are running what they were deployed with")
}

// A workload the deployment recorded nothing for is not checked. Unknown is not
// drift — the alternative is every workload of every app deployed before the
// record existed looking wrong at once.
func TestAWorkloadWithNoRecordedImageIsNotDrift(t *testing.T) {
	drift := reconciler.Classify(
		want(
			api.WorkloadPlan{Name: "app", Image: "pando/app:latest"},
			api.WorkloadPlan{Name: "proxy", Image: "caddy:2-alpine"},
		),
		api.ObservedBundle{Exists: true, Workloads: []api.ObservedWorkload{
			{Name: "app", Present: true, Running: true, ImageDigest: "sha256:theapp"},
			{Name: "proxy", Present: true, Running: true, ImageDigest: "sha256:caddy"},
		}},
		reconciler.Inputs{ExpectedDigests: map[string]string{"app": "sha256:theapp"}})

	require.True(t, drift.None())
}

// TestR148_AMissingWorkloadWithNoRecordedImageIsReported asserts R-148.
//
// "Reconcile when possible, report when not." A workload built separately by a
// deployment that recorded only one image for the whole app is the "not": the
// only image to hand belongs to another part of the app, and starting this one
// from it is how an application container became a second copy of its proxy.
func TestR148_AMissingWorkloadWithNoRecordedImageIsReported(t *testing.T) {
	drift := reconciler.Classify(
		api.BundlePlan{BundleID: "app_1", Workloads: []api.WorkloadPlan{{Name: "app"}}},
		api.ObservedBundle{Exists: true},
		reconciler.Inputs{})

	require.Empty(t, drift.Reconcilable)
	require.Len(t, drift.ReportOnly, 1)
	require.Contains(t, drift.ReportOnly[0].Detail, "deploy this app again")
}
