package docker

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/errs"
)

// TestR105_AMountOverAFileSaysWhichMount asserts R-105.
//
// The daemon's own words for this are a path under /var/lib/docker that appears
// in nothing the person configured:
//
//	Error response from daemon: source
//	/var/lib/docker/rootfs/overlayfs/a083…/etc/caddy/Caddyfile is not directory
//
// What reached the deploy log from it was `Could not create "proxy".` — true,
// and with nothing in it to act on.
func TestR105_AMountOverAFileSaysWhichMount(t *testing.T) {
	w := api.WorkloadPlan{
		Name: "proxy",
		Mounts: []api.MountPlan{
			{VolumeID: "vol_data", Path: "/data"},
			{VolumeID: "vol_caddyfile", Path: "/etc/caddy/Caddyfile"},
		},
	}
	daemon := errors.New("Error response from daemon: source " +
		"/var/lib/docker/rootfs/overlayfs/a083/etc/caddy/Caddyfile is not directory")

	err := createFailure(w, daemon)

	e := errs.As(err)
	require.NotNil(t, e)
	require.Equal(t, errs.AdapterFailed, e.Code)
	require.Contains(t, e.Message, "proxy")
	require.Contains(t, e.Message, "/etc/caddy/Caddyfile", "the mount, not the host path")
	require.NotContains(t, e.Message, "/data", "the directory mount is not the problem")
	require.NotEmpty(t, e.Remedy, "and what to do about it")

	// The daemon's text is still there for whoever wants it.
	require.ErrorIs(t, err, daemon)
}

// Every other refusal keeps the message it had. A guess about the cause of an
// error nobody has seen yet is worse than the error.
func TestAnyOtherCreateFailureIsUnchanged(t *testing.T) {
	w := api.WorkloadPlan{Name: "app", Mounts: []api.MountPlan{{VolumeID: "v", Path: "/etc/x.conf"}}}

	err := createFailure(w, errors.New("Error response from daemon: no such image"))

	e := errs.As(err)
	require.NotNil(t, e)
	require.Equal(t, `Could not create "app".`, e.Message)
	require.Empty(t, e.Remedy)
}

// A pull that fails says why. The daemon reports it inside a 200 response, and
// the stream was discarded, so the failure surfaced later as "No such image"
// with the reason gone (issue #55).
func TestAFailedPullReportsTheRegistrysReason(t *testing.T) {
	stream := `{"status":"Pulling from library/x"}
{"errorDetail":{"message":"no matching manifest for linux/arm64/v8 in the manifest list entries"},"error":"no matching manifest"}
`
	err := pullError(strings.NewReader(stream))
	require.Error(t, err)
	require.Contains(t, err.Error(), "linux/arm64")

	require.NoError(t, pullError(strings.NewReader(`{"status":"Downloaded newer image"}`+"\n")))
}
