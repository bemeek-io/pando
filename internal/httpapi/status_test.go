package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// TestR261_StatusReportsEachPartOfAnAppSeparately asserts R-261.
//
// An app can be a web service, a proxy and a database it brought with it, and
// "degraded" is one word for all of them. crewmate reported degraded while its
// application container restarted every two seconds, and the only place that
// was visible was `docker ps` on the host — which is the thing Pando exists so
// nobody has to run.
//
// Every surface reads this: the console's Parts table, `pando app status`, and
// pando_get_status.
func TestR261_StatusReportsEachPartOfAnAppSeparately(t *testing.T) {
	exit := 1
	healthy := false
	started := time.Now().Add(-time.Minute)

	s := &spec.AppSpec{Workloads: []spec.Workload{
		{Name: "app"},
		{Name: "proxy", Primary: true},
	}}

	got := workloadStatuses(s, api.ObservedBundle{Workloads: []api.ObservedWorkload{
		{Name: "proxy", Present: true, Running: true, StartedAt: started},
		{
			Name: "app", Present: true, Running: true, Restarting: true,
			RestartCount: 14, ExitCode: &exit, Healthy: &healthy,
		},
		// Running for this app and not in its spec: a provisioned database.
		{Name: "svc-01HQ8", Present: true, Running: true},
	}})

	require.Len(t, got, 3)

	// The spec's own order, so an app reads the way its author wrote it.
	require.Equal(t, "app", got[0].Name)
	require.Equal(t, "proxy", got[1].Name)
	require.Equal(t, "svc-01HQ8", got[2].Name, "what else is running, last")

	require.True(t, got[0].Restarting, "the crash loop is the thing worth seeing")
	require.Equal(t, 14, got[0].RestartCount)
	require.Equal(t, &exit, got[0].ExitCode)
	require.False(t, *got[0].Healthy)

	require.True(t, got[1].Primary, "the part the app's address resolves to")
	require.False(t, got[0].Primary)
	require.False(t, got[2].Primary, "and nothing outside the spec claims to be")
}

// The wire names, because the console and the CLI read them and the API is
// snake_case everywhere else. This used to serialize the adapter's own struct,
// which put Go field names in the response.
func TestStatusIsSerializedInTheAPIsVocabulary(t *testing.T) {
	healthy := true
	body, err := json.Marshal(WorkloadStatus{
		Name: "app", Running: true, RestartCount: 3, Healthy: &healthy,
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))

	for _, key := range []string{"name", "primary", "present", "running", "restarting", "restart_count", "healthy"} {
		require.Contains(t, decoded, key)
	}
	require.NotContains(t, decoded, "RestartCount")

	// R-221: no health check is null, not false. It has to survive the wire.
	body, err = json.Marshal(WorkloadStatus{Name: "app"})
	require.NoError(t, err)
	require.Contains(t, string(body), `"healthy":null`)
}
