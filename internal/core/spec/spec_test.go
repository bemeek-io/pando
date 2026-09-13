package spec_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

func ptr[T any](v T) *T { return &v }

// valid returns a minimal spec that passes validation, for tests to break in
// one specific way each.
func valid() *spec.AppSpec {
	return &spec.AppSpec{
		SchemaVersion: spec.SchemaVersion,
		AppID:         "app_01HQ8",
		Revision:      1,
		Origin:        spec.OriginManual,
		Source:        spec.Source{Type: spec.SourceGit, URL: "https://github.com/acme/notes", Ref: "main"},
		Build:         spec.Build{Strategy: spec.BuildDockerfile, AdapterRef: "bld_buildkit"},
		Workloads: []spec.Workload{{
			Name:    "web",
			Primary: true,
			Exposed: true,
			Ports:   []spec.Port{{Number: 3000, Protocol: "http", Source: spec.PortObserved}},
		}},
		Routing: spec.Routing{AdapterRef: "rte_loopback", Mode: spec.RoutingPort, Port: 8080},
		Runtime: spec.RuntimeRef{AdapterRef: "rt_docker", IsolationFloor: spec.IsolationContainer},
		Deploy:  spec.Deploy{Strategy: spec.DeployRecreate},
	}
}

func TestValidSpecPasses(t *testing.T) {
	require.NoError(t, spec.Validate(valid()))
}

// R-026 and the bundle model: the app has one canonical endpoint.
func TestValidatePrimaryWorkload(t *testing.T) {
	none := valid()
	none.Workloads[0].Primary = false
	err := spec.Validate(none)
	require.Error(t, err)
	require.Equal(t, errs.ValidPrimaryWorkload, errs.CodeOf(err))

	two := valid()
	two.Workloads = append(two.Workloads, spec.Workload{Name: "admin", Primary: true})
	err = spec.Validate(two)
	require.Error(t, err)
	require.Equal(t, errs.ValidPrimaryWorkload, errs.CodeOf(err))

	empty := valid()
	empty.Workloads = nil
	err = spec.Validate(empty)
	require.Error(t, err)
	require.Equal(t, errs.ValidPrimaryWorkload, errs.CodeOf(err))
}

// A dangling mount would produce a workload that starts with an empty directory
// where its data should be — healthy-looking and wrong.
func TestValidateDanglingMount(t *testing.T) {
	s := valid()
	s.Workloads[0].Mounts = []spec.Mount{{VolumeID: "vol_missing", Path: "/app/data"}}

	err := spec.Validate(s)
	require.Error(t, err)
	require.Equal(t, errs.ValidDanglingMount, errs.CodeOf(err))

	s.Volumes = []spec.Volume{{ID: "vol_missing", Name: "data", Declared: spec.VolumeFromUser}}
	require.NoError(t, spec.Validate(s))
}

func TestValidateEnvHasExactlyOneSource(t *testing.T) {
	none := valid()
	none.Workloads[0].Env = []spec.EnvEntry{{Key: "PORT"}}
	require.Equal(t, errs.ValidEnvAmbiguous, errs.CodeOf(spec.Validate(none)))

	both := valid()
	both.Workloads[0].Env = []spec.EnvEntry{{Key: "PORT", Value: ptr("3000"), SecretRef: ptr("sec_01HQ8")}}
	require.Equal(t, errs.ValidEnvAmbiguous, errs.CodeOf(spec.Validate(both)))

	ok := valid()
	ok.Workloads[0].Env = []spec.EnvEntry{{Key: "PORT", Value: ptr("3000")}}
	require.NoError(t, spec.Validate(ok))
}

func TestValidateDanglingSlotRef(t *testing.T) {
	s := valid()
	s.Workloads[0].Env = []spec.EnvEntry{{Key: "REDIS_URL", SlotRef: ptr("REDIS_URL")}}
	require.Equal(t, errs.ValidDanglingSlotRef, errs.CodeOf(spec.Validate(s)))

	s.Slots = []spec.Slot{{Key: "REDIS_URL", Type: spec.SlotRedis, Required: true}}
	require.NoError(t, spec.Validate(s), "an unfilled slot is still a valid spec — R-132 is a plan-time check")
}

func TestValidateDependencyCycle(t *testing.T) {
	s := valid()
	s.Workloads = []spec.Workload{
		{Name: "web", Primary: true, DependsOn: []string{"api"}},
		{Name: "api", DependsOn: []string{"worker"}},
		{Name: "worker", DependsOn: []string{"web"}},
	}

	err := spec.Validate(s)
	require.Error(t, err)
	require.Equal(t, errs.ValidDependencyCycle, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "→", "the message should name the loop")

	s.Workloads[2].DependsOn = nil
	require.NoError(t, spec.Validate(s))
}

func TestValidateUnknownDependency(t *testing.T) {
	s := valid()
	s.Workloads[0].DependsOn = []string{"database"}
	require.Error(t, spec.Validate(s))
}

func TestValidateRouting(t *testing.T) {
	cases := map[string]func(*spec.AppSpec){
		"subdomain without hostname": func(s *spec.AppSpec) {
			s.Routing = spec.Routing{Mode: spec.RoutingSubdomain}
		},
		"path without prefix": func(s *spec.AppSpec) {
			s.Routing = spec.Routing{Mode: spec.RoutingPath}
		},
		"relative path prefix": func(s *spec.AppSpec) {
			s.Routing = spec.Routing{Mode: spec.RoutingPath, PathPrefix: "notes"}
		},
		"port out of range": func(s *spec.AppSpec) {
			s.Routing = spec.Routing{Mode: spec.RoutingPort, Port: 70000}
		},
		"no mode": func(s *spec.AppSpec) { s.Routing = spec.Routing{} },
		"unknown mode": func(s *spec.AppSpec) {
			s.Routing = spec.Routing{Mode: spec.RoutingMode("carrier-pigeon")}
		},
	}
	for name, break_ := range cases {
		s := valid()
		break_(s)
		require.Error(t, spec.Validate(s), name)
	}
}

// Someone hand-writing a spec deserves every problem at once, not one per round
// trip.
func TestValidationAccumulatesProblems(t *testing.T) {
	s := valid()
	s.Workloads[0].Primary = false
	s.Workloads[0].Mounts = []spec.Mount{{VolumeID: "vol_missing", Path: "/data"}}
	s.Routing = spec.Routing{}

	err := spec.Validate(s)
	require.Error(t, err)

	problems, ok := errs.As(err).Details["problems"].([]map[string]any)
	require.True(t, ok)
	require.GreaterOrEqual(t, len(problems), 3, "every problem should be reported at once")
}

func TestValidateSchemaVersion(t *testing.T) {
	s := valid()
	s.SchemaVersion = 99
	require.Error(t, spec.Validate(s))
}

func TestValidateSlotResolutionShapes(t *testing.T) {
	bound := valid()
	bound.Slots = []spec.Slot{{Key: "DATABASE_URL", Type: spec.SlotPostgres,
		Resolution: &spec.Resolution{Mode: spec.ResolutionBound}}}
	require.Error(t, spec.Validate(bound), "bound needs a target")

	literal := valid()
	literal.Slots = []spec.Slot{{Key: "DATABASE_URL", Type: spec.SlotPostgres,
		Resolution: &spec.Resolution{Mode: spec.ResolutionLiteral}}}
	require.Error(t, spec.Validate(literal), "a literal is stored as a secret, so it needs a secret ref")

	provisioned := valid()
	provisioned.Slots = []spec.Slot{{Key: "DATABASE_URL", Type: spec.SlotPostgres,
		Resolution: &spec.Resolution{Mode: spec.ResolutionProvisioned}}}
	require.NoError(t, spec.Validate(provisioned), "service_ref is filled in at deploy time")
}

// R-020: export is safe to hand to someone, which requires that no secret value
// can be represented in a spec at all.
func TestR020_SpecCarriesNoSecretValues(t *testing.T) {
	s := valid()
	s.Slots = []spec.Slot{{
		Key:        "DATABASE_URL",
		Type:       spec.SlotPostgres,
		Resolution: &spec.Resolution{Mode: spec.ResolutionLiteral, SecretRef: "sec_01HQ8"},
	}}
	s.Workloads[0].Env = []spec.EnvEntry{{Key: "DATABASE_URL", SlotRef: ptr("DATABASE_URL")}}
	require.NoError(t, spec.Validate(s))

	encoded, err := json.Marshal(s)
	require.NoError(t, err)

	// Only references appear, never values.
	require.Contains(t, string(encoded), "sec_01HQ8")
	require.NotContains(t, string(encoded), "postgres://")
}

// TestR161_AnAppsAddressFollowsItsRoutingMode asserts R-161.
//
// Three modes, three shapes of address, and the bug this pins is what happens
// when a caller assumes one of them: both console screens assembled "/" + slug
// for every app, which is right only in path mode and wrong on every install
// whose routing adapter does ports — the laptop default.
func TestR161_AnAppsAddressFollowsItsRoutingMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		host    string
		routing spec.Routing
		want    string
	}{
		{
			name:    "path mode is relative to the install",
			host:    "pando.corp:8080",
			routing: spec.Routing{Mode: spec.RoutingPath},
			want:    "/notes/",
		},
		{
			name:    "port mode keeps the host the caller used",
			host:    "pando.corp:8080",
			routing: spec.Routing{Mode: spec.RoutingPort, Port: 9010},
			want:    "//pando.corp:9010/",
		},
		{
			name:    "port mode on a bare host",
			host:    "localhost",
			routing: spec.Routing{Mode: spec.RoutingPort, Port: 9010},
			want:    "//localhost:9010/",
		},
		{
			name:    "subdomain mode is the app's own name",
			host:    "pando.corp",
			routing: spec.Routing{Mode: spec.RoutingSubdomain, Hostname: "notes.corp"},
			want:    "https://notes.corp/",
		},
		{
			// An app with no pinned spec has no routing block. Saying so beats
			// handing back a path that answers for nothing.
			name:    "no routing yet is no address",
			host:    "pando.corp",
			routing: spec.Routing{},
			want:    "",
		},
		{
			name:    "port mode with no port allocated yet",
			host:    "pando.corp",
			routing: spec.Routing{Mode: spec.RoutingPort},
			want:    "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, spec.Address(tc.host, "notes", tc.routing))
		})
	}
}
