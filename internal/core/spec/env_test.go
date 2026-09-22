package spec_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
)

// SetEnv puts a person's value where the variable is read: the workload named,
// every workload declaring it, or — for a new variable — the primary one.
func TestR022_SetEnvPutsAValueWhereTheVariableIsRead(t *testing.T) {
	str := func(s string) *string { return &s }
	s := &spec.AppSpec{Workloads: []spec.Workload{
		{Name: "web", Primary: true, Env: []spec.EnvEntry{{Key: "API_URL"}}},
		{Name: "worker", Env: []spec.EnvEntry{{Key: "API_URL"}, {Key: "QUEUE"}}},
	}}

	spec.SetEnv(s, "", "API_URL", spec.EnvEntry{Value: str("https://api")})
	require.Equal(t, "https://api", *s.Workloads[0].Env[0].Value)
	require.Equal(t, "https://api", *s.Workloads[1].Env[0].Value)
	require.Equal(t, spec.EnvFromUser, s.Workloads[1].Env[0].Source)

	spec.SetEnv(s, "worker", "QUEUE", spec.EnvEntry{SecretRef: str("QUEUE")})
	require.Equal(t, "QUEUE", *s.Workloads[1].Env[1].SecretRef)
	require.Len(t, s.Workloads[0].Env, 1, "named workload only")

	spec.SetEnv(s, "", "NEW_ONE", spec.EnvEntry{Value: str("x")})
	require.Equal(t, "NEW_ONE", s.Workloads[0].Env[1].Key, "a new variable goes to the primary workload")
	require.Len(t, s.Workloads[1].Env, 2)
}
