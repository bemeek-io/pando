package docker

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/adapter/api"
	"github.com/trypando/pando/internal/errs"
)

// Observing, removing and listing edges, and the ways starting one fails.

func TestObserveEdgeReportsWhatIsThere(t *testing.T) {
	f, a := newFakeDaemon(t, nil)
	f.on("GET /containers/json", respond(http.StatusOK, []any{}))
	st, err := a.ObserveEdge(context.Background(), "rte_traefik")
	require.NoError(t, err)
	require.False(t, st.Present)

	f.on("GET /containers/json", respond(http.StatusOK, []map[string]any{
		{"Id": "e1", "State": "exited", "Labels": map[string]string{labelEdge: "rte_traefik"}},
	}))
	st, err = a.ObserveEdge(context.Background(), "rte_traefik")
	require.NoError(t, err)
	require.True(t, st.Present)
	require.False(t, st.Running)
	require.Contains(t, st.Detail, "exited", "it says what it found, and does not start it (design 05)")

	f.on("GET /containers/json", respond(http.StatusOK, []map[string]any{
		{"Id": "e1", "State": "running", "Labels": map[string]string{labelEdge: "rte_traefik"}},
	}))
	st, err = a.ObserveEdge(context.Background(), "rte_traefik")
	require.NoError(t, err)
	require.True(t, st.Running)
	require.Empty(t, st.Detail)
}

func TestRemoveEdgeRemovesTheContainerAndNotItsCertificates(t *testing.T) {
	f, a := newFakeDaemon(t, nil)
	f.on("GET /containers/json", respond(http.StatusOK, []any{}))
	require.NoError(t, a.RemoveEdge(context.Background(), "rte_gone"), "nothing to remove is not an error")

	f.on("GET /containers/json", respond(http.StatusOK, []map[string]any{
		{"Id": "e1", "Labels": map[string]string{labelEdge: "rte_traefik"}},
	}))
	f.on("DELETE /containers/e1", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	require.NoError(t, a.RemoveEdge(context.Background(), "rte_traefik"))
	require.Equal(t, 1, f.called("DELETE /containers/e1"))
	require.Zero(t, f.called("DELETE /volumes"), "the certificate store is kept for when it comes back")
}

// TestR212_EdgeVolumesAreTheOnesTheEdgeOwns asserts what the full-host backup
// asks for: the edge's labeled volumes, as handles.
func TestR212_EdgeVolumesAreTheOnesTheEdgeOwns(t *testing.T) {
	f, a := newFakeDaemon(t, nil)
	f.on("GET /volumes", respond(http.StatusOK, map[string]any{"Volumes": []map[string]any{
		{"Name": "pando-edge-rte_traefik-acme", "Labels": map[string]string{labelEdge: "rte_traefik"}},
		{"Name": "pando-edge-rte_cf-state", "Labels": map[string]string{labelEdge: "rte_cf"}},
	}}))
	got, err := a.EdgeVolumes(context.Background())
	require.NoError(t, err)
	require.Equal(t, []api.VolumeHandle{{Handle: "pando-edge-rte_cf-state"}, {Handle: "pando-edge-rte_traefik-acme"}}, got)

	f.on("GET /volumes", respond(http.StatusInternalServerError, map[string]string{"message": "down"}))
	_, err = a.EdgeVolumes(context.Background())
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(err))
}

func TestADaemonThatCannotListIsUnavailable(t *testing.T) {
	f, a := newFakeDaemon(t, nil)
	f.on("GET /containers/json", respond(http.StatusInternalServerError, map[string]string{"message": "down"}))
	_, err := a.Edges(context.Background())
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(err))
	_, err = a.ObserveEdge(context.Background(), "x")
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(err))
	require.Error(t, a.RemoveEdge(context.Background(), "x"))
}

func TestAnEdgePlanWithoutANameOrImageIsAMistake(t *testing.T) {
	_, a := newFakeDaemon(t, nil)
	require.Equal(t, errs.Internal, errs.CodeOf(a.ApplyEdge(context.Background(), api.EdgePlan{Image: "x"})))
	require.Equal(t, errs.Internal, errs.CodeOf(a.ApplyEdge(context.Background(), api.EdgePlan{Name: "x"})))
}

func TestAnEdgeMountWithNeitherSourceIsAMistake(t *testing.T) {
	_, a, _, _ := edgeDaemon(t)
	plan := traefikEdge()
	plan.Mounts = []api.EdgeMount{{Path: "/x"}}
	require.Equal(t, errs.Internal, errs.CodeOf(a.ApplyEdge(context.Background(), plan)))
}

func TestAStoppedMatchingEdgeIsStartedNotRecreated(t *testing.T) {
	f, a, _, _ := edgeDaemon(t)
	plan := traefikEdge()
	digest := edgeDigest(plan, []string{"pando_traefik-dynamic:/etc/traefik/dynamic:ro", "pando-edge-rte_traefik-acme:/acme"})
	f.on("GET /containers/json", respond(http.StatusOK, []map[string]any{
		{"Id": "edge0", "State": "exited", "Labels": map[string]string{labelEdge: "rte_traefik"}},
	}))
	f.on("GET /containers/edge0/json", respond(http.StatusOK, map[string]any{
		"Id": "edge0",
		"Config": map[string]any{
			"Labels": map[string]string{labelEdgeDigest: digest},
			"Env":    []string{"CF_DNS_API_TOKEN=tok-123"},
		},
	}))
	f.on("POST /containers/edge0/start", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	require.NoError(t, a.ApplyEdge(context.Background(), plan))
	require.Equal(t, 1, f.called("POST /containers/edge0/start"))
	require.Zero(t, f.called("POST /containers/create"))
}

func TestAChangedPlanRecreatesTheEdge(t *testing.T) {
	f, a, _, _ := edgeDaemon(t)
	f.on("GET /containers/json", respond(http.StatusOK, []map[string]any{
		{"Id": "edge0", "State": "running", "Labels": map[string]string{labelEdge: "rte_traefik"}},
	}))
	f.on("GET /containers/edge0/json", respond(http.StatusOK, map[string]any{
		"Id": "edge0", "Config": map[string]any{"Labels": map[string]string{labelEdgeDigest: "old"}},
	}))
	f.on("DELETE /containers/edge0", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	require.NoError(t, a.ApplyEdge(context.Background(), traefikEdge()))
	require.Equal(t, 1, f.called("POST /containers/create"))

	f.on("GET /containers/edge0/json", respond(http.StatusInternalServerError, map[string]string{"message": "down"}))
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(a.ApplyEdge(context.Background(), traefikEdge())))
}

func TestTheEdgeNetworkIsReusedAndItsFailuresNamed(t *testing.T) {
	f, a, _, _ := edgeDaemon(t)
	f.on("GET /networks", respond(http.StatusOK, []map[string]any{{"Name": edgeNetwork, "Id": "edge-net"}}))
	f.on("POST /networks/edge-net/connect", respond(http.StatusForbidden, map[string]string{"message": "endpoint with name pando already exists in network"}))
	require.NoError(t, a.ApplyEdge(context.Background(), traefikEdge()))
	require.Zero(t, f.called("POST /networks/create"), "one network for the install, made once")

	f.on("POST /networks/edge-net/connect", respond(http.StatusInternalServerError, map[string]string{"message": "boom"}))
	err := a.ApplyEdge(context.Background(), traefikEdge())
	require.ErrorContains(t, err, "network it shares with its edge")

	f.on("GET /networks", respond(http.StatusInternalServerError, map[string]string{"message": "down"}))
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(a.ApplyEdge(context.Background(), traefikEdge())))

	f.on("GET /networks", respond(http.StatusOK, []any{}))
	f.on("POST /networks/create", respond(http.StatusInternalServerError, map[string]string{"message": "no pool"}))
	require.ErrorContains(t, a.ApplyEdge(context.Background(), traefikEdge()), "set up the network")
}

func TestCreatingAndStartingFailuresAreNamed(t *testing.T) {
	f, a, _, _ := edgeDaemon(t)
	f.on("POST /containers/create", respond(http.StatusInternalServerError, map[string]string{"message": "no"}))
	require.ErrorContains(t, a.ApplyEdge(context.Background(), traefikEdge()), "Could not create the rte_traefik edge")

	f, a, _, _ = edgeDaemon(t)
	f.on("POST /containers/edge1/start", respond(http.StatusInternalServerError, map[string]string{"message": "oom"}))
	require.ErrorContains(t, a.ApplyEdge(context.Background(), traefikEdge()), "Could not start the rte_traefik edge")

	f, a, _, _ = edgeDaemon(t)
	f.on("POST /volumes/create", respond(http.StatusInternalServerError, map[string]string{"message": "disk"}))
	require.ErrorContains(t, a.ApplyEdge(context.Background(), traefikEdge()), "storage for the rte_traefik edge")
}
