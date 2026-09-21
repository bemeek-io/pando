package screening_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/screening"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// Every amendment kind, landing and refused. The closed set (R-332) is only a
// mechanism if each member does exactly what it says and refuses what it must.

func TestR332_EachAmendmentKindLandsWhereItSays(t *testing.T) {
	s := draft()
	s.Workloads[0].Ports = []spec.Port{{Number: 3000, Protocol: "http", Source: spec.PortFramework}}
	repo["public/index.html"] = "<html>"
	repo["apps/web/package.json"] = "{}"
	t.Cleanup(func() { delete(repo, "public/index.html"); delete(repo, "apps/web/package.json") })

	applied, refused := screening.Apply(s, env(), []api.Amendment{
		sound(api.Amendment{Kind: api.AmendSetCommand, Command: []string{"npm", "start"}}),
		sound(api.Amendment{Kind: api.AmendSetHealth, Path: "/healthz"}),
		sound(api.Amendment{Kind: api.AmendSetBuildContext, Path: "apps/web"}),
		sound(api.Amendment{Kind: api.AmendSetStaticDir, Path: "public"}),
		sound(api.Amendment{Kind: api.AmendAddSlot, Key: "CACHE_URL"}),
	})
	require.Empty(t, refused)
	require.Len(t, applied, 5)

	require.Equal(t, []string{"npm", "start"}, s.Workloads[0].Command)
	require.Equal(t, spec.HealthFromHTTP, s.Health.Source)
	require.Equal(t, "/healthz", s.Health.Path)
	require.Equal(t, 3000, s.Health.Port, "the workload's port when none is given")
	require.Equal(t, "apps/web", s.Build.Context)
	require.Equal(t, "public", s.Build.StaticDir)
	require.Equal(t, spec.BuildStatic, s.Build.Strategy)
	require.Equal(t, spec.SlotUnknown, s.Slots[0].Type, "an untyped dependency is recorded as unknown")
	require.False(t, s.Slots[0].Required)
}

func TestR332_ACommandLineIsHandedToAShell(t *testing.T) {
	s := draft()
	_, refused := screening.Apply(s, env(), []api.Amendment{
		sound(api.Amendment{Kind: api.AmendSetCommand, Value: `node server.js --name "a b"`}),
		sound(api.Amendment{Kind: api.AmendSetCommand}),
	})
	require.Equal(t, []string{"sh", "-c", `node server.js --name "a b"`}, s.Workloads[0].Command)
	require.Len(t, refused, 1, "an empty command is refused")
}

func TestR332_EachAmendmentKindRefusesWhatItMust(t *testing.T) {
	s := draft()
	s.Slots = []spec.Slot{{Key: "DATABASE_URL", Type: spec.SlotPostgres}}
	s.Workloads[0].Mounts = []spec.Mount{{VolumeID: "data", Path: "/data"}}

	cases := []api.Amendment{
		{Kind: api.AmendSetCommand, Workload: "worker", Command: []string{"x"}},
		{Kind: api.AmendSetEnv, Key: "not a key", Value: "x"},
		{Kind: api.AmendSetEnv, Workload: "worker", Key: "A", Value: "x"},
		{Kind: api.AmendSetPort, Port: 70000},
		{Kind: api.AmendSetPort, Workload: "worker", Port: 80},
		{Kind: api.AmendSetHealth, Path: "healthz"},
		{Kind: api.AmendSetHealth, Path: "/healthz"}, // no port anywhere
		{Kind: api.AmendAddSlot, Key: "DATABASE_URL"},
		{Kind: api.AmendAddSlot, Key: "bad key"},
		{Kind: api.AmendAddSlot, Key: "QUEUE_URL", SlotType: "kafka"},
		{Kind: api.AmendSetBuildContext, Path: ""},
		{Kind: api.AmendSetStaticDir, Path: "/srv/www"},
		{Kind: api.AmendAddVolume, Path: "relative/dir"},
		{Kind: api.AmendAddVolume, Path: "/data/"},
		{Kind: api.AmendAddVolume, Workload: "worker", Path: "/x"},
		{Kind: api.AmendAnswerQuestion, Key: "primary_port", Value: "3000"},
	}
	for i := range cases {
		cases[i] = sound(cases[i])
	}

	applied, refused := screening.Apply(s, env(), cases)
	require.Empty(t, applied)
	require.Len(t, refused, len(cases))
	for _, r := range refused {
		require.NotEmpty(t, r.Reason, "%+v", r.Amendment)
	}
}

func TestR332_AnAmendmentMustSayWhichWorkloadWhenThereIsNoPrimary(t *testing.T) {
	s := draft()
	s.Workloads = []spec.Workload{{Name: "api"}, {Name: "worker"}}

	applied, refused := screening.Apply(s, env(), []api.Amendment{
		sound(api.Amendment{Kind: api.AmendSetEnv, Key: "A", Value: "1"}),
		sound(api.Amendment{Kind: api.AmendSetEnv, Workload: "worker", Key: "A", Value: "1"}),
	})
	require.Len(t, refused, 1)
	require.Contains(t, refused[0].Reason, "did not say which part")
	require.Len(t, applied, 1)
	require.Len(t, s.Workloads[1].Env, 1)

	single := draft()
	single.Workloads[0].Primary = false
	applied, _ = screening.Apply(single, env(), []api.Amendment{
		sound(api.Amendment{Kind: api.AmendSetEnv, Key: "A", Value: "1"}),
	})
	require.Len(t, applied, 1, "one workload is the one an amendment means")
}

func TestR331_AScreenerMayReplaceAValueDetectionInferred(t *testing.T) {
	s := draft()
	old := "development"
	s.Workloads[0].Env = []spec.EnvEntry{{Key: "NODE_ENV", Value: &old, Source: spec.EnvFromDetection}}

	applied, refused := screening.Apply(s, env(), []api.Amendment{
		sound(api.Amendment{Kind: api.AmendSetEnv, Key: "NODE_ENV", Value: "production"}),
	})
	require.Empty(t, refused)
	require.Len(t, applied, 1)
	require.Len(t, s.Workloads[0].Env, 1, "replaced, not duplicated")
	require.Equal(t, "production", *s.Workloads[0].Env[0].Value)
	require.Equal(t, spec.EnvFromScreening, s.Workloads[0].Env[0].Source)
}

func TestR201_TwoVolumesAtSimilarPathsGetDistinctNames(t *testing.T) {
	s := draft()
	s.Volumes = []spec.Volume{{ID: "data", Name: "data"}}
	_, refused := screening.Apply(s, env(), []api.Amendment{
		sound(api.Amendment{Kind: api.AmendAddVolume, Path: "/data"}),
		sound(api.Amendment{Kind: api.AmendAddVolume, Path: "/"}),
	})
	require.Empty(t, refused)
	require.Equal(t, "data-2", s.Volumes[1].ID)
	require.Equal(t, "data-3", s.Volumes[2].ID, "the root path is named data too, and still distinct")
}

func TestR334_AnEmptyEvidenceEntryIsIgnoredRatherThanChecked(t *testing.T) {
	applied, refused := screening.Apply(draft(), env(), []api.Amendment{{
		Kind: api.AmendAddWarning, Reason: "Worth knowing.", Evidence: []string{"", "README.md"},
	}})
	require.Empty(t, refused)
	require.Len(t, applied, 1)
}

// --- Run ---------------------------------------------------------------------

type screener struct {
	caps    api.AICapabilities
	capsErr error
	result  api.ScreenResult
	err     error
	got     api.ScreenRequest
}

func (s *screener) Capabilities(context.Context) (api.AICapabilities, error) {
	return s.caps, s.capsErr
}

func (s *screener) ScreenPlan(ctx context.Context, req api.ScreenRequest) (api.ScreenResult, error) {
	s.got = req
	if _, ok := ctx.Deadline(); !ok {
		return api.ScreenResult{}, errors.New("no deadline")
	}
	return s.result, s.err
}

func screens() api.AICapabilities {
	return api.AICapabilities{Functions: []api.AIFunction{api.AIFunctionScreenPlan}, Model: "m-caps"}
}

// TestR335_EveryWayRunCanFailIsASkipWithAReason asserts R-335 for Run itself.
func TestR335_EveryWayRunCanFailIsASkipWithAReason(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		s    screening.Screener
		code screening.SkipCode
	}{
		"none":        {nil, screening.SkipNotConfigured},
		"caps fail":   {&screener{capsErr: errors.New("down")}, screening.SkipUnavailable},
		"unsupported": {&screener{caps: api.AICapabilities{}}, screening.SkipUnsupported},
		"screen fail": {&screener{caps: screens(), err: errors.New("timeout")}, screening.SkipUnavailable},
	} {
		_, outcome := screening.Run(ctx, tc.s, "ai_x", api.ScreenRequest{})
		require.False(t, outcome.Ran, name)
		require.Equal(t, tc.code, outcome.SkipCode, name)
		require.NotEmpty(t, outcome.Skipped, name)
		require.False(t, outcome.Changed(), name)
	}
}

// TestR339_RunLowersTheBudgetToTheAdaptersAndSetsADeadline asserts R-339.
func TestR339_RunLowersTheBudgetToTheAdaptersAndSetsADeadline(t *testing.T) {
	caps := screens()
	caps.MaxFiles, caps.MaxBytes = 5, 1000
	s := &screener{caps: caps, result: api.ScreenResult{FilesRead: []string{"a"}, Notes: []string{"n"}}}

	_, outcome := screening.Run(context.Background(), s, "ai_x", api.ScreenRequest{})
	require.True(t, outcome.Ran)
	require.Equal(t, "ai_x", outcome.AdapterRef)
	require.Equal(t, "m-caps", outcome.Model, "the capabilities' model when the result names none")
	require.Equal(t, []string{"a"}, outcome.FilesRead)
	require.Equal(t, 5, s.got.Budget.MaxFiles)
	require.Equal(t, int64(1000), s.got.Budget.MaxBytes)
	require.Equal(t, screening.DefaultTimeout, s.got.Budget.Timeout)

	s2 := &screener{caps: screens(), result: api.ScreenResult{Model: "m-result"}}
	_, outcome = screening.Run(context.Background(), s2, "ai_x", api.ScreenRequest{
		Budget: api.ScreenBudget{MaxFiles: 3, MaxBytes: 10, Timeout: time.Second},
	})
	require.Equal(t, "m-result", outcome.Model)
	require.Equal(t, 3, s2.got.Budget.MaxFiles, "never raised to the adapter's")
	require.Equal(t, int64(10), s2.got.Budget.MaxBytes)
	require.Equal(t, time.Second, s2.got.Budget.Timeout)
}

func TestOutcomeChangedAndElapsed(t *testing.T) {
	require.True(t, screening.Outcome{Answers: map[string]string{"k": "v"}}.Changed())
	require.True(t, screening.Outcome{Applied: []screening.Applied{{}}}.Changed())
	require.Equal(t, int64(1500), screening.Outcome{}.Elapsed(1500*time.Millisecond).DurationMS)
}

func TestR338_AnAnswerMayNotBeEmptyOrGivenTwice(t *testing.T) {
	answers, _, refused := screening.Split(env(), []api.Amendment{
		sound(api.Amendment{Kind: api.AmendAnswerQuestion, Key: "primary_port", Value: " "}),
		sound(api.Amendment{Kind: api.AmendAnswerQuestion, Key: "primary_port", Value: "3000"}),
		sound(api.Amendment{Kind: api.AmendAnswerQuestion, Key: "primary_port", Value: "4000"}),
	}, map[string]bool{"primary_port": true})
	require.Equal(t, map[string]string{"primary_port": "3000"}, answers)
	require.Len(t, refused, 2)
}
