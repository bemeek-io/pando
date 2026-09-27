package docker

import (
	"context"
	"testing"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/stretchr/testify/require"
)

// TestR023_TheUpstreamIsTheContainerNameOnItsBundleNetwork asserts that Docker
// answers the proxy with the one name that means the same container on every
// network Pando is joined to.
//
// The workload's alias would not do: Pando sits on every app's network, so
// "web" names a different container on each. And it is computed, never asked
// of the daemon, because it runs on every proxied request.
func TestR023_TheUpstreamIsTheContainerNameOnItsBundleNetwork(t *testing.T) {
	a := New() // unconfigured: no daemon is reachable, and none is needed

	got, err := a.Upstream(context.Background(), api.WorkloadRef{BundleID: "app_01HQ8", Workload: "web"}, 3000)
	require.NoError(t, err)
	require.Equal(t, "http://pando-app_01HQ8-web:3000", got.URL)
	require.Equal(t, containerName("app_01HQ8", "web")+":3000", got.URL[len("http://"):],
		"the address is the name the container was created under")

	other, err := a.Upstream(context.Background(), api.WorkloadRef{BundleID: "app_01HQ9", Workload: "web"}, 3000)
	require.NoError(t, err)
	require.NotEqual(t, got.URL, other.URL, "two apps' workloads of the same name are different upstreams")
}

func TestAnUpstreamNeedsAPort(t *testing.T) {
	_, err := New().Upstream(context.Background(), api.WorkloadRef{BundleID: "app_01HQ8", Workload: "web"}, 0)
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
}
