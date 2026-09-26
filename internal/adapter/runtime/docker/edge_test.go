package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/secret"
)

// createRequest is the part of a container-create request these tests read.
type createRequest struct {
	Image      string
	Cmd        []string
	Env        []string
	Labels     map[string]string
	HostConfig struct {
		Binds         []string
		PortBindings  map[string][]struct{ HostPort string }
		RestartPolicy struct{ Name string }
		ExtraHosts    []string
	}
	NetworkingConfig struct {
		EndpointsConfig map[string]any
	}
}

func traefikEdge() api.EdgePlan {
	return api.EdgePlan{
		Name:  "rte_traefik",
		Image: "traefik:v3.2",
		Args:  []string{"--providers.file.directory=/etc/traefik/dynamic"},
		Env:   map[string]secret.Value{"CF_DNS_API_TOKEN": secret.New("tok-123")},
		Ports: []api.EdgePort{{Host: 80, Container: 80}, {Host: 443, Container: 443}},
		Mounts: []api.EdgeMount{
			{Path: "/etc/traefik/dynamic", SharedWithPando: "/etc/traefik/dynamic", ReadOnly: true},
			{Path: "/acme", Volume: "acme"},
		},
		ProxyAlias: "pando",
	}
}

// edgeDaemon answers the calls ApplyEdge makes on a host with no edge yet,
// with Pando in a container that mounts the routes directory from a volume.
func edgeDaemon(t *testing.T) (*fakeDaemon, *Adapter, *createRequest, *map[string]any) {
	t.Helper()
	f, a := newFakeDaemon(t, map[string]any{"proxy_container": "pando-self"})
	got := &createRequest{}
	connect := &map[string]any{}

	f.on("GET /networks", respond(http.StatusOK, []any{}))
	f.on("POST /networks/create", respond(http.StatusCreated, map[string]string{"Id": "edge-net"}))
	f.on("POST /networks/edge-net/connect", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(connect))
		w.WriteHeader(http.StatusOK)
	})
	f.on("GET /containers/pando-self/json", respond(http.StatusOK, map[string]any{
		"Id": "pando-self",
		"Mounts": []map[string]string{
			{"Type": "volume", "Name": "pando_traefik-dynamic", "Destination": "/etc/traefik/dynamic"},
		},
	}))
	f.on("GET /containers/json", respond(http.StatusOK, []any{}))
	f.on("GET /images/*", respond(http.StatusOK, map[string]any{"Id": "sha256:abc"}))
	f.on("POST /containers/create", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(got))
		writeJSON(w, http.StatusCreated, map[string]string{"Id": "edge1"})
	})
	f.on("POST /containers/edge1/start", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return f, a, got, connect
}

// TestR174_AnEdgePublishesItsPortsAndReachesOnlyPando asserts R-174 within
// R-023 and R-026: the edge is the one thing that publishes host ports, it
// joins the network it shares with Pando and no app's, and Pando answers to
// the alias the edge dials.
func TestR174_AnEdgePublishesItsPortsAndReachesOnlyPando(t *testing.T) {
	_, a, got, connect := edgeDaemon(t)

	require.NoError(t, a.ApplyEdge(context.Background(), traefikEdge()))

	require.Equal(t, "traefik:v3.2", got.Image)
	require.Equal(t, "80", got.HostConfig.PortBindings["80/tcp"][0].HostPort)
	require.Equal(t, "443", got.HostConfig.PortBindings["443/tcp"][0].HostPort)

	require.Len(t, got.NetworkingConfig.EndpointsConfig, 1)
	require.Contains(t, got.NetworkingConfig.EndpointsConfig, edgeNetwork, "the edge joins no app's network")
	require.Equal(t, []any{"pando"}, (*connect)["EndpointConfig"].(map[string]any)["Aliases"])

	require.Equal(t, "unless-stopped", got.HostConfig.RestartPolicy.Name)
	require.Contains(t, got.Env, "CF_DNS_API_TOKEN=tok-123", "revealed into the edge's own configuration")
	require.Equal(t, "rte_traefik", got.Labels[labelEdge])
	for k, v := range got.Labels {
		require.NotContains(t, v, "tok-123", "label %s must not carry a credential", k)
	}
}

// TestR251_ASharedPathIsTheStoragePandoHasThere asserts R-251: the edge is
// given whatever Pando's own container has mounted at the shared path — here a
// Compose volume — and core never names it.
func TestR251_ASharedPathIsTheStoragePandoHasThere(t *testing.T) {
	_, a, got, _ := edgeDaemon(t)

	require.NoError(t, a.ApplyEdge(context.Background(), traefikEdge()))

	require.Contains(t, got.HostConfig.Binds, "pando_traefik-dynamic:/etc/traefik/dynamic:ro")
	require.Contains(t, got.HostConfig.Binds, "pando-edge-rte_traefik-acme:/acme",
		"the certificate store is the edge's own volume and outlives the container")
}

func TestASharedPathNothingIsMountedAtIsRefusedWithTheFix(t *testing.T) {
	f, a, _, _ := edgeDaemon(t)
	f.on("GET /containers/pando-self/json", respond(http.StatusOK, map[string]any{"Id": "pando-self"}))

	err := a.ApplyEdge(context.Background(), traefikEdge())
	require.Error(t, err)
	var e *errs.Error
	require.ErrorAs(t, err, &e)
	require.Contains(t, e.Message, "/etc/traefik/dynamic")
	require.NotEmpty(t, e.Remedy)
	require.Zero(t, f.called("POST /containers/create"))
}

func TestAMatchingEdgeIsLeftRunning(t *testing.T) {
	f, a, _, _ := edgeDaemon(t)
	plan := traefikEdge()
	digest := edgeDigest(plan, []string{"pando_traefik-dynamic:/etc/traefik/dynamic:ro", "pando-edge-rte_traefik-acme:/acme"})

	f.on("GET /containers/json", respond(http.StatusOK, []map[string]any{
		{"Id": "edge0", "State": "running", "Labels": map[string]string{labelEdge: "rte_traefik"}},
	}))
	f.on("GET /containers/edge0/json", respond(http.StatusOK, map[string]any{
		"Id": "edge0",
		"Config": map[string]any{
			"Labels": map[string]string{labelEdgeDigest: digest},
			"Env":    []string{"CF_DNS_API_TOKEN=tok-123"},
		},
	}))

	require.NoError(t, a.ApplyEdge(context.Background(), plan))
	require.Zero(t, f.called("POST /containers/create"))
	require.Zero(t, f.called("DELETE /containers/"))
}

// TestR193_ARotatedEdgeCredentialRecreatesTheEdge asserts R-193 for the edge:
// the value is not in the digest, so it is compared on the container.
func TestR193_ARotatedEdgeCredentialRecreatesTheEdge(t *testing.T) {
	f, a, _, _ := edgeDaemon(t)
	plan := traefikEdge()
	digest := edgeDigest(plan, []string{"pando_traefik-dynamic:/etc/traefik/dynamic:ro", "pando-edge-rte_traefik-acme:/acme"})

	f.on("GET /containers/json", respond(http.StatusOK, []map[string]any{
		{"Id": "edge0", "State": "running", "Labels": map[string]string{labelEdge: "rte_traefik"}},
	}))
	f.on("GET /containers/edge0/json", respond(http.StatusOK, map[string]any{
		"Id": "edge0",
		"Config": map[string]any{
			"Labels": map[string]string{labelEdgeDigest: digest},
			"Env":    []string{"CF_DNS_API_TOKEN=old-token"},
		},
	}))
	f.on("DELETE /containers/edge0", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	require.NoError(t, a.ApplyEdge(context.Background(), plan))
	require.Equal(t, 1, f.called("DELETE /containers/edge0"))
	require.Equal(t, 1, f.called("POST /containers/create"))
}

func TestAnEdgeWhosePortIsTakenSaysWhichPort(t *testing.T) {
	f, a, _, _ := edgeDaemon(t)
	f.on("POST /containers/edge1/start", respond(http.StatusInternalServerError, map[string]string{
		"message": "driver failed programming external connectivity: Bind for 0.0.0.0:80 failed: port is already allocated",
	}))

	err := a.ApplyEdge(context.Background(), traefikEdge())
	var e *errs.Error
	require.ErrorAs(t, err, &e)
	require.Contains(t, e.Message, "port 80 or 443")
	require.Contains(t, e.Remedy, "restart Pando")
}

func TestWithPandoOnTheHostTheEdgeReachesItThroughTheGateway(t *testing.T) {
	f, a, got, _ := edgeDaemon(t)
	f.on("POST /networks/edge-net/connect", respond(http.StatusNotFound, map[string]string{"message": "No such container: pando-self"}))
	f.on("GET /containers/pando-self/json", respond(http.StatusNotFound, map[string]string{"message": "No such container"}))

	require.NoError(t, a.ApplyEdge(context.Background(), traefikEdge()))
	require.Equal(t, []string{"pando:host-gateway"}, got.HostConfig.ExtraHosts)
	require.Contains(t, got.HostConfig.Binds, "/etc/traefik/dynamic:/etc/traefik/dynamic:ro",
		"not in a container, the path is on the host")
}

func TestEdgesNamesEveryEdge(t *testing.T) {
	f, a := newFakeDaemon(t, nil)
	f.on("GET /containers/json", respond(http.StatusOK, []map[string]any{
		{"Id": "b", "Labels": map[string]string{labelEdge: "rte_traefik"}},
		{"Id": "a", "Labels": map[string]string{labelEdge: "rte_cloudflare"}},
	}))
	names, err := a.Edges(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"rte_cloudflare", "rte_traefik"}, names)
}
