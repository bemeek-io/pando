package deploy_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/deploy"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/secret"
)

func ptr(s string) *string { return &s }

// withEnv is a one-workload spec carrying the given environment.
func withEnv(entries ...spec.EnvEntry) *spec.AppSpec {
	return &spec.AppSpec{Workloads: []spec.Workload{{Name: "web", Env: entries}}}
}

func literal(key, value string) spec.EnvEntry {
	return spec.EnvEntry{Key: key, Value: ptr(value)}
}

func secretEnv(key, ref string) spec.EnvEntry {
	return spec.EnvEntry{Key: key, SecretRef: ptr(ref)}
}

func slotEnv(key, ref string) spec.EnvEntry {
	return spec.EnvEntry{Key: key, SlotRef: ptr(ref)}
}

// R-193, and the whole mechanism behind it: a rotated secret has to reach the
// app, and Observe returns no environment to compare against.
func TestR193_RotatingASecretChangesTheFingerprint(t *testing.T) {
	s := withEnv(secretEnv("API_KEY", "sec_01HQ8"))

	before := deploy.EnvFingerprint(s, map[string]int{"sec_01HQ8": 1})
	after := deploy.EnvFingerprint(s, map[string]int{"sec_01HQ8": 2})

	require.NotEqual(t, before, after)
	require.Equal(t, before, deploy.EnvFingerprint(s, map[string]int{"sec_01HQ8": 1}),
		"the same version hashes the same, so a tick that changed nothing restarts nothing")
}

// It hashes secret versions, never secret values. A fingerprint is stored in a
// column, shown in logs, and survives in backups — hashing the value would put
// an oracle for every secret into all three.
func TestTheFingerprintCannotConfirmAGuessAtASecretValue(t *testing.T) {
	s := withEnv(secretEnv("API_KEY", "sec_01HQ8"))

	// Nothing about the value is an input, so no amount of guessing at it
	// changes the answer. The version is the only thing that moves.
	fingerprint := deploy.EnvFingerprint(s, map[string]int{"sec_01HQ8": 7})
	require.Equal(t, fingerprint, deploy.EnvFingerprint(s, map[string]int{"sec_01HQ8": 7}))

	// A secret with no recorded version hashes as version zero rather than
	// failing: an unversioned secret is a real state during migration.
	require.NotEqual(t, fingerprint, deploy.EnvFingerprint(s, nil))
}

// A spec whose workloads or env entries were reordered without changing has not
// drifted, and reporting that it had would restart every app the first time
// someone tidied a YAML file.
func TestReorderingWithoutChangingIsNotDrift(t *testing.T) {
	versions := map[string]int{"sec_01HQ8": 1}

	ordered := withEnv(literal("A", "1"), secretEnv("B", "sec_01HQ8"), literal("C", "3"))
	shuffled := withEnv(literal("C", "3"), literal("A", "1"), secretEnv("B", "sec_01HQ8"))
	require.Equal(t, deploy.EnvFingerprint(ordered, versions), deploy.EnvFingerprint(shuffled, versions))

	byWorkload := &spec.AppSpec{Workloads: []spec.Workload{
		{Name: "web", Env: []spec.EnvEntry{literal("A", "1")}},
		{Name: "worker", Env: []spec.EnvEntry{literal("B", "2")}},
	}}
	reversed := &spec.AppSpec{Workloads: []spec.Workload{
		{Name: "worker", Env: []spec.EnvEntry{literal("B", "2")}},
		{Name: "web", Env: []spec.EnvEntry{literal("A", "1")}},
	}}
	require.Equal(t, deploy.EnvFingerprint(byWorkload, nil), deploy.EnvFingerprint(reversed, nil))
}

// A literal in the spec is not a secret — it is already in the spec revision,
// readable by anyone who can read the app — so its value is hashed directly.
func TestChangingALiteralChangesTheFingerprint(t *testing.T) {
	require.NotEqual(t,
		deploy.EnvFingerprint(withEnv(literal("LOG_LEVEL", "info")), nil),
		deploy.EnvFingerprint(withEnv(literal("LOG_LEVEL", "debug")), nil))
}

func TestTheSameValueUnderADifferentKeyIsADifferentFingerprint(t *testing.T) {
	require.NotEqual(t,
		deploy.EnvFingerprint(withEnv(literal("A", "x")), nil),
		deploy.EnvFingerprint(withEnv(literal("B", "x")), nil))
}

// The same environment on a different workload is a different environment.
func TestTheWorkloadNameIsPartOfTheFingerprint(t *testing.T) {
	web := &spec.AppSpec{Workloads: []spec.Workload{{Name: "web", Env: []spec.EnvEntry{literal("A", "1")}}}}
	worker := &spec.AppSpec{Workloads: []spec.Workload{{Name: "worker", Env: []spec.EnvEntry{literal("A", "1")}}}}
	require.NotEqual(t, deploy.EnvFingerprint(web, nil), deploy.EnvFingerprint(worker, nil))
}

// A slot's resolution can change from provisioned to bound, or point at a
// different target, and either means the app is talking to something else.
func TestHowASlotIsFilledIsPartOfTheFingerprint(t *testing.T) {
	base := func(r *spec.Resolution) *spec.AppSpec {
		s := withEnv(slotEnv("DATABASE_URL", "DATABASE_URL"))
		s.Slots = []spec.Slot{{Key: "DATABASE_URL", Type: spec.SlotPostgres, Required: true, Resolution: r}}
		return s
	}

	unresolved := deploy.EnvFingerprint(base(nil), nil)
	provisioned := deploy.EnvFingerprint(base(&spec.Resolution{
		Mode: spec.ResolutionProvisioned, ServiceRef: "svc_01HQ8"}), nil)
	bound := deploy.EnvFingerprint(base(&spec.Resolution{
		Mode: spec.ResolutionBound, Target: "postgres://elsewhere"}), nil)
	elsewhere := deploy.EnvFingerprint(base(&spec.Resolution{
		Mode: spec.ResolutionBound, Target: "postgres://somewhere-else"}), nil)

	require.NotEqual(t, unresolved, provisioned)
	require.NotEqual(t, provisioned, bound)
	require.NotEqual(t, bound, elsewhere, "a slot pointed at a different target has drifted")
}

// A slot the spec does not declare cannot be resolved, and an unanswerable
// question must not crash the fingerprint.
func TestAnEnvEntryPointingAtAMissingSlotIsUnresolvedNotFatal(t *testing.T) {
	s := withEnv(slotEnv("DATABASE_URL", "NOT_DECLARED"))
	require.NotPanics(t, func() { deploy.EnvFingerprint(s, nil) })
	require.NotEmpty(t, deploy.EnvFingerprint(s, nil))
}

// An entry with no source at all contributes nothing rather than hashing a nil.
func TestAnEmptyEnvEntryIsSkipped(t *testing.T) {
	require.Equal(t,
		deploy.EnvFingerprint(withEnv(), nil),
		deploy.EnvFingerprint(withEnv(spec.EnvEntry{Key: "ORPHAN"}), nil))
}

func TestAnAppWithNoEnvironmentStillHasAStableFingerprint(t *testing.T) {
	empty := deploy.EnvFingerprint(&spec.AppSpec{}, nil)
	require.Len(t, empty, 64, "a sha256, rendered as hex")
	require.Equal(t, empty, deploy.EnvFingerprint(&spec.AppSpec{}, nil))
}

// PlanFingerprint takes resolved values, so it is compared in-process and never
// stored. It hashes the shape — which variables exist — rather than the values.
func TestPlanFingerprintComparesTheSetOfVariables(t *testing.T) {
	plan := func(keys ...string) api.BundlePlan {
		env := map[string]secret.Value{}
		for _, k := range keys {
			env[k] = secret.New("resolved " + k)
		}
		return api.BundlePlan{Workloads: []api.WorkloadPlan{{Name: "web", Env: env}}}
	}

	one := deploy.PlanFingerprint(plan("A", "B"))
	require.Equal(t, one, deploy.PlanFingerprint(plan("B", "A")),
		"map iteration order must not change the answer")

	require.NotEqual(t, one, deploy.PlanFingerprint(plan("A")))
	require.NotEqual(t, one, deploy.PlanFingerprint(plan("A", "C")))

	require.Len(t, deploy.PlanFingerprint(api.BundlePlan{}), 64)
}
