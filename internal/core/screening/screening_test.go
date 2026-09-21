package screening_test

import (
	"io"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/screening"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// memSource is an in-memory SourceView, so a screening can be tested against an
// exact repository shape rather than whatever a real one happens to contain.
type memSource map[string]string

func (m memSource) Open(name string) (io.ReadCloser, error) {
	content, ok := m[path.Clean(name)]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (m memSource) Stat(name string) (api.FileInfo, error) {
	clean := path.Clean(name)
	if content, ok := m[clean]; ok {
		return api.FileInfo{Name: path.Base(clean), Size: int64(len(content))}, nil
	}
	// Directories are implied by the files under them, as source.dirView
	// reports a real one.
	for name := range m {
		if strings.HasPrefix(name, clean+"/") {
			return api.FileInfo{Name: path.Base(clean), IsDir: true}, nil
		}
	}
	return api.FileInfo{}, io.EOF
}

func (m memSource) Glob(pattern string) ([]string, error) {
	var out []string
	for name := range m {
		if ok, _ := path.Match(pattern, name); ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

var repo = memSource{
	"package.json":        `{"scripts":{"start":"node server.js"}}`,
	"server.js":           "app.listen(3000, '127.0.0.1')",
	"README.md":           "Set HOST=0.0.0.0 in production.",
	"Dockerfile":          "FROM node:22",
	"apps/web/Dockerfile": "FROM node:22",
}

func draft() *spec.AppSpec {
	return &spec.AppSpec{
		SchemaVersion: spec.SchemaVersion,
		AppID:         "app_test",
		Build:         spec.Build{Strategy: spec.BuildBuildpack},
		Workloads: []spec.Workload{{
			Name: "web", Primary: true, Exposed: true,
			Env: []spec.EnvEntry{},
		}},
	}
}

func env() screening.Env {
	return screening.Env{Source: repo}
}

// sound is an amendment with everything R-314 requires, so a test that is about
// one rule is not also about that one.
func sound(a api.Amendment) api.Amendment {
	if a.Reason == "" {
		a.Reason = "The repository says so."
	}
	if len(a.Evidence) == 0 {
		a.Evidence = []string{"package.json"}
	}
	return a
}

// TestR314_AnAmendmentRestingOnNothingIsRefused asserts R-314.
func TestR314_AnAmendmentRestingOnNothingIsRefused(t *testing.T) {
	s := draft()
	applied, refused := screening.Apply(s, env(), []api.Amendment{{
		Kind: api.AmendSetEnv, Key: "HOST", Value: "0.0.0.0",
		Reason: "The app binds loopback.",
		// No evidence.
	}})

	require.Empty(t, applied)
	require.Len(t, refused, 1)
	require.Contains(t, refused[0].Reason, "named no file")
	require.Empty(t, s.Workloads[0].Env, "nothing was written")
}

// TestR314_EvidenceThatIsNotInTheRepositoryIsRefused asserts R-314.
//
// This is the cheapest available check on whether the screener read this
// repository or recalled a framework, and it is the one that catches the
// failure mode that matters: a confident amendment about a file that is not
// there.
func TestR314_EvidenceThatIsNotInTheRepositoryIsRefused(t *testing.T) {
	s := draft()
	applied, refused := screening.Apply(s, env(), []api.Amendment{{
		Kind: api.AmendSetEnv, Key: "HOST", Value: "0.0.0.0",
		Reason:   "Rails binds loopback by default.",
		Evidence: []string{"config/puma.rb"},
	}})

	require.Empty(t, applied)
	require.Len(t, refused, 1)
	require.Contains(t, refused[0].Reason, "config/puma.rb")
	require.Contains(t, refused[0].Reason, "not in this repository")
}

// TestR314_AnAppliedAmendmentRecordsScreenedProvenance asserts R-314.
//
// The review shows where a value came from, and "a model read your README" is
// not the same claim as "you set this" or "we watched it happen".
func TestR314_AnAppliedAmendmentRecordsScreenedProvenance(t *testing.T) {
	s := draft()
	applied, refused := screening.Apply(s, env(), []api.Amendment{sound(api.Amendment{
		Kind: api.AmendSetEnv, Key: "HOST", Value: "0.0.0.0",
		Evidence: []string{"README.md", "server.js"},
	})})

	require.Empty(t, refused)
	require.Len(t, applied, 1)
	require.Len(t, s.Workloads[0].Env, 1)
	require.Equal(t, "HOST", s.Workloads[0].Env[0].Key)
	require.Equal(t, spec.EnvFromScreening, s.Workloads[0].Env[0].Source)
	require.NotEqual(t, spec.EnvFromUser, s.Workloads[0].Env[0].Source,
		"a screener is not a person, and spec.Carry treats the two differently")
}

// TestR313_AnObservationOutranksAScreening asserts R-313.
//
// The rule most likely to be argued with. R-097 exists because watching the
// process bind beats asking about it, and a model's reading of a framework's
// documentation is a more elaborate form of asking.
func TestR313_AnObservationOutranksAScreening(t *testing.T) {
	s := draft()
	s.Workloads[0].Ports = []spec.Port{{Number: 3000, Protocol: "http", Source: spec.PortObserved}}

	applied, refused := screening.Apply(s, env(), []api.Amendment{sound(api.Amendment{
		Kind: api.AmendSetPort, Port: 8080,
		Reason: "The framework's default port is 8080.",
	})})

	require.Empty(t, applied)
	require.Len(t, refused, 1)
	require.Contains(t, refused[0].Reason, "watched this app bind")
	require.Equal(t, 3000, s.Workloads[0].Ports[0].Number, "the observation stands")
	require.Equal(t, spec.PortObserved, s.Workloads[0].Ports[0].Source)
}

// TestR313_AScreenedPortFillsWhatWasNotObserved asserts R-313.
//
// The asymmetry runs one way. Where nothing was observed, a screened port beats
// a question — which is the whole reason the rule is a refusal rather than a
// ban.
func TestR313_AScreenedPortFillsWhatWasNotObserved(t *testing.T) {
	s := draft()
	applied, refused := screening.Apply(s, env(), []api.Amendment{sound(api.Amendment{
		Kind: api.AmendSetPort, Port: 3000,
		Evidence: []string{"server.js"},
	})})

	require.Empty(t, refused)
	require.Len(t, applied, 1)
	require.Equal(t, 3000, s.Workloads[0].Ports[0].Number)
	require.Equal(t, spec.PortScreened, s.Workloads[0].Ports[0].Source)
}

// TestR312_AnAmendmentKindThatIsNotInTheSetSaysNothing asserts R-312.
//
// The closed set is the mechanism. A screener asking for something outside it
// has not said anything Pando can act on — which is why this is a refusal with
// no special case for the words it used.
func TestR312_AnAmendmentKindThatIsNotInTheSetSaysNothing(t *testing.T) {
	s := draft()
	before := *s

	applied, refused := screening.Apply(s, env(), []api.Amendment{
		sound(api.Amendment{Kind: "set_isolation_floor", Value: "container"}),
		sound(api.Amendment{Kind: "set_runtime_adapter", Value: "rt_docker"}),
		sound(api.Amendment{Kind: "set_egress_mode", Value: "inherit"}),
		sound(api.Amendment{Kind: "set_resource_limits", Value: "4096"}),
	})

	require.Empty(t, applied)
	require.Len(t, refused, 4)
	require.Equal(t, before.Runtime, s.Runtime)
	require.Equal(t, before.Egress, s.Egress)
	require.Equal(t, before.Resources, s.Resources)
	require.Equal(t, before.Routing, s.Routing)
}

// TestR312_RefusalsAreRecordedNeverSilent asserts R-312.
//
// An amendment that vanishes because core did not like it teaches nobody
// anything, and the refusals are how the next version of the prompt gets
// written.
func TestR312_RefusalsAreRecordedNeverSilent(t *testing.T) {
	s := draft()
	applied, refused := screening.Apply(s, env(), []api.Amendment{
		sound(api.Amendment{Kind: api.AmendSetEnv, Key: "HOST", Value: "0.0.0.0"}),
		sound(api.Amendment{Kind: "invented", Value: "x"}),
	})

	require.Len(t, applied, 1, "one bad amendment does not discard the good one beside it")
	require.Len(t, refused, 1)
	require.NotEmpty(t, refused[0].Reason)
	require.Equal(t, api.AmendmentKind("invented"), refused[0].Amendment.Kind,
		"the refused amendment is carried, not just its reason")
}

// TestO4_AScreenerMayNotMarkASlotRequiredOnACleanTrialRun asserts O-4's
// fallback, applied to screening (design 09 §3.2).
//
// R-132 blocks a deploy on an unfilled required slot, so a screener marking
// slots required freely turns "this app might not start" into "this app cannot
// be deployed" — a blocker where configuration would do (R-104).
func TestO4_AScreenerMayNotMarkASlotRequiredOnACleanTrialRun(t *testing.T) {
	s := draft()
	clean := screening.Env{Source: repo, Trial: api.TrialSummary{Ran: true, Crashed: false}}

	applied, refused := screening.Apply(s, clean, []api.Amendment{sound(api.Amendment{
		Kind: api.AmendAddSlot, Key: "DATABASE_URL", SlotType: spec.SlotPostgres, Required: true,
	})})

	require.Empty(t, refused)
	require.Len(t, applied, 1, "the dependency is still recorded — it is real")
	require.Len(t, s.Slots, 1)
	require.False(t, s.Slots[0].Required, "recorded, and not a blocker")
}

// TestO4_ACrashedTrialRunLetsAScreenerMarkASlotRequired asserts O-4's fallback.
func TestO4_ACrashedTrialRunLetsAScreenerMarkASlotRequired(t *testing.T) {
	s := draft()
	crashed := screening.Env{Source: repo, Trial: api.TrialSummary{Ran: true, Crashed: true}}

	applied, refused := screening.Apply(s, crashed, []api.Amendment{sound(api.Amendment{
		Kind: api.AmendAddSlot, Key: "DATABASE_URL", SlotType: spec.SlotPostgres, Required: true,
	})})

	require.Empty(t, refused)
	require.Len(t, applied, 1)
	require.True(t, s.Slots[0].Required)
	require.Contains(t, s.Slots[0].Evidence, "package.json", "the evidence travels onto the slot")
}

// TestR311_AScreenerCannotOverwriteAValueAPersonSet asserts R-311 and R-022's
// rule: a decision a person made outlives anything Pando worked out for itself.
func TestR311_AScreenerCannotOverwriteAValueAPersonSet(t *testing.T) {
	s := draft()
	mine := "1"
	s.Workloads[0].Env = []spec.EnvEntry{{Key: "DEBUG", Value: &mine, Source: spec.EnvFromUser}}

	applied, refused := screening.Apply(s, env(), []api.Amendment{sound(api.Amendment{
		Kind: api.AmendSetEnv, Key: "DEBUG", Value: "0",
	})})

	require.Empty(t, applied)
	require.Len(t, refused, 1)
	require.Contains(t, refused[0].Reason, "set by hand")
	require.Equal(t, "1", *s.Workloads[0].Env[0].Value)
}

// TestR311_AScreenerCannotWriteOverADependencyOrASecret asserts R-311.
//
// Writing a literal over a database URL Pando is about to provision would
// replace a resolved dependency with a string a model wrote.
func TestR311_AScreenerCannotWriteOverADependencyOrASecret(t *testing.T) {
	s := draft()
	s.Slots = []spec.Slot{{Key: "DATABASE_URL", Type: spec.SlotPostgres}}
	ref := "DATABASE_URL"
	secretRef := "sek_api"
	s.Workloads[0].Env = []spec.EnvEntry{
		{Key: "DATABASE_URL", SlotRef: &ref},
		{Key: "API_KEY", SecretRef: &secretRef},
	}

	_, refused := screening.Apply(s, env(), []api.Amendment{
		sound(api.Amendment{Kind: api.AmendSetEnv, Key: "DATABASE_URL", Value: "postgres://localhost/app"}),
		sound(api.Amendment{Kind: api.AmendSetEnv, Key: "API_KEY", Value: "hunter2"}),
	})

	require.Len(t, refused, 2)
	require.Equal(t, &ref, s.Workloads[0].Env[0].SlotRef)
	require.Nil(t, s.Workloads[0].Env[0].Value)
	require.Equal(t, &secretRef, s.Workloads[0].Env[1].SecretRef)
	require.Nil(t, s.Workloads[0].Env[1].Value)
}

// TestR021_APathAmendmentMustPointInsideTheRepository asserts R-021 and R-020.
func TestR021_APathAmendmentMustPointInsideTheRepository(t *testing.T) {
	s := draft()
	for _, p := range []string{"/etc/passwd", "../../secrets", "does/not/exist"} {
		applied, refused := screening.Apply(draft(), env(), []api.Amendment{sound(api.Amendment{
			Kind: api.AmendSetDockerfile, Path: p,
		})})
		require.Empty(t, applied, p)
		require.Len(t, refused, 1, p)
	}
	require.Equal(t, spec.BuildBuildpack, s.Build.Strategy)
}

// TestR106_AMonorepoEntrypointIsWhatScreeningIsFor asserts R-106's
// "disambiguating monorepo entrypoints".
func TestR106_AMonorepoEntrypointIsWhatScreeningIsFor(t *testing.T) {
	s := draft()
	applied, refused := screening.Apply(s, env(), []api.Amendment{sound(api.Amendment{
		Kind: api.AmendSetDockerfile, Path: "apps/web/Dockerfile",
		Reason:   "The repository root has no application; apps/web is the deployable service.",
		Evidence: []string{"apps/web/Dockerfile"},
	})})

	require.Empty(t, refused)
	require.Len(t, applied, 1)
	require.Equal(t, "apps/web/Dockerfile", s.Build.Dockerfile)
	require.Equal(t, spec.BuildDockerfile, s.Build.Strategy)
}

// TestR318_AnAnswerToAQuestionNobodyAskedIsRefused asserts R-318.
func TestR318_AnAnswerToAQuestionNobodyAskedIsRefused(t *testing.T) {
	answers, rest, refused := screening.Split(env(), []api.Amendment{
		sound(api.Amendment{Kind: api.AmendAnswerQuestion, Key: "primary_port", Value: "3000"}),
		sound(api.Amendment{Kind: api.AmendAnswerQuestion, Key: "favorite_color", Value: "green"}),
		sound(api.Amendment{Kind: api.AmendSetEnv, Key: "HOST", Value: "0.0.0.0"}),
	}, map[string]bool{"primary_port": true})

	require.Equal(t, map[string]string{"primary_port": "3000"}, answers)
	require.Len(t, rest, 1, "non-answer amendments pass through untouched")
	require.Len(t, refused, 1)
	require.Contains(t, refused[0].Reason, "favorite_color")
}

// TestR318_AnAnswerIsHeldToTheSameEvidenceRule asserts R-318 and R-314.
func TestR318_AnAnswerIsHeldToTheSameEvidenceRule(t *testing.T) {
	answers, _, refused := screening.Split(env(), []api.Amendment{{
		Kind: api.AmendAnswerQuestion, Key: "primary_port", Value: "3000",
		Reason: "It is 3000.",
	}}, map[string]bool{"primary_port": true})

	require.Empty(t, answers)
	require.Len(t, refused, 1)
}

// TestR098_AScreeningCannotFloodTheReview asserts R-098 by way of MaxAmendments.
//
// A review a person cannot read is a review that does not happen.
func TestR098_AScreeningCannotFloodTheReview(t *testing.T) {
	var many []api.Amendment
	for i := range screening.MaxAmendments + 5 {
		many = append(many, sound(api.Amendment{
			Kind: api.AmendAddWarning, Value: string(rune('a' + i%26)),
		}))
	}

	applied, refused := screening.Apply(draft(), env(), many)
	require.Len(t, applied, screening.MaxAmendments)
	require.Len(t, refused, 5)
}

// TestR314_WithoutAReadableRepositoryNothingIsApplied asserts R-314.
//
// Evidence that cannot be checked is evidence that was not checked, and the
// honest failure is the one that keeps the deterministic proposal.
func TestR314_WithoutAReadableRepositoryNothingIsApplied(t *testing.T) {
	applied, refused := screening.Apply(draft(), screening.Env{}, []api.Amendment{
		sound(api.Amendment{Kind: api.AmendSetEnv, Key: "HOST", Value: "0.0.0.0"}),
	})
	require.Empty(t, applied)
	require.Len(t, refused, 1)
}

// TestR201_AScreenerMayDeclareStorageForADirectoryItFound asserts R-201.
func TestR201_AScreenerMayDeclareStorageForADirectoryItFound(t *testing.T) {
	s := draft()
	applied, refused := screening.Apply(s, env(), []api.Amendment{sound(api.Amendment{
		Kind: api.AmendAddVolume, Path: "/app/uploads",
	})})

	require.Empty(t, refused)
	require.Len(t, applied, 1)
	require.Len(t, s.Volumes, 1)
	require.Equal(t, spec.VolumeFromScreening, s.Volumes[0].Declared)
	require.Len(t, s.Workloads[0].Mounts, 1)
	require.Equal(t, "/app/uploads", s.Workloads[0].Mounts[0].Path)
	require.Equal(t, s.Volumes[0].ID, s.Workloads[0].Mounts[0].VolumeID,
		"the mount resolves to a declared volume, or validation refuses the spec")
}

// TestR028_AScreenerCannotForgeAWarningCode asserts R-028 and design 01 §2.8.
//
// The codes mean specific things that specific parts of Pando produced. A
// screener able to pick one could claim the compose importer rewrote something
// it never touched.
func TestR028_AScreenerCannotForgeAWarningCode(t *testing.T) {
	s := draft()
	applied, refused := screening.Apply(s, env(), []api.Amendment{sound(api.Amendment{
		Kind:  api.AmendAddWarning,
		Key:   spec.WarnComposeConstructRewritten,
		Value: "Pando rewrote your compose file.",
	})})

	require.Empty(t, refused)
	require.Len(t, applied, 1)
	require.Len(t, s.Warnings, 1)
	require.Equal(t, spec.WarnScreeningAdvisory, s.Warnings[0].Code)
}
