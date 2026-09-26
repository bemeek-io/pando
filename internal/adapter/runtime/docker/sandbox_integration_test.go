//go:build integration

package docker_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	dockeradapter "github.com/bemeek-io/pando/internal/adapter/runtime/docker"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// sandboxedAdapter is the adapter with oci_runtime set to runsc, skipping the
// test on a daemon that has no gVisor — CI, and Docker Desktop, which cannot
// install it. On a Mac, a Colima VM with runsc registered in its daemon.json
// can: point DOCKER_HOST at its socket.
func sandboxedAdapter(t *testing.T) *dockeradapter.Adapter {
	t.Helper()
	a := dockeradapter.New()
	raw, _ := json.Marshal(map[string]string{"oci_runtime": "runsc"})
	require.NoError(t, a.Configure(context.Background(), raw))
	if err := a.HealthCheck(context.Background()); err != nil {
		t.Skipf("no gVisor on this Docker daemon: %v", err)
	}
	return a
}

// kernelOf reads the kernel log of a running container, which under gVisor is
// gVisor's own and says so on its first line.
func kernelOf(t *testing.T, container string) string {
	t.Helper()
	out, err := exec.Command("docker", "exec", container, "dmesg").CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

// TestR114_ASandboxedTrialRunsInsideTheSandbox asserts that the trial run —
// the first time an unreviewed app's code runs at all — gets the boundary its
// deploy will, and that it reports what it can see through that boundary
// rather than what it cannot.
func TestR114_ASandboxedTrialRunsInsideTheSandbox(t *testing.T) {
	ctx := context.Background()
	a := sandboxedAdapter(t)

	caps, err := a.Capabilities(ctx)
	require.NoError(t, err)
	require.Equal(t, spec.IsolationSandboxed, caps.IsolationClass)

	result, err := a.Trial(ctx, api.TrialRequest{
		TrialID: trialID(t),
		Image:   "busybox:1.37",
		// The kernel log goes to the trial's own log, so the test can see
		// which kernel the trial ran on without catching the container.
		Command: []string{"sh", "-c", "dmesg | head -1; exec httpd -f -p 8123"},
		Timeout: 15 * time.Second,
	})
	require.NoError(t, err)

	require.True(t, result.Started)
	require.Nil(t, result.ExitCode, "it was still running, which is the passing outcome")
	require.Contains(t, result.Log, "gVisor", "the trial ran on gVisor's kernel, not the host's")
	require.Empty(t, result.ObservedPorts,
		"R-097: nothing is reported through a sandbox rather than something wrong")
	require.Empty(t, result.ObservedWrites)
}

// TestR114_ASandboxedDeployRunsInsideTheSandbox asserts the same of an app's
// deploy: the container the reported class describes is the one running.
func TestR114_ASandboxedDeployRunsInsideTheSandbox(t *testing.T) {
	ctx := context.Background()
	a := sandboxedAdapter(t)

	id := "app_sandbox" + time.Now().Format("150405")
	plan := bundle(id, nil)
	_, err := a.Apply(ctx, plan)
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.Destroy(context.Background(), api.BundleRef{BundleID: id}, api.DestroyOptions{}) })

	container := "pando-" + id + "-web"
	runtime, err := exec.Command("docker", "inspect", container, "--format", "{{.HostConfig.Runtime}}").Output()
	require.NoError(t, err)
	require.Equal(t, "runsc", strings.TrimSpace(string(runtime)))
	require.Contains(t, kernelOf(t, container), "gVisor")

	// And re-applying the same plan leaves it alone: in the sandbox already.
	_, err = a.Apply(ctx, plan)
	require.NoError(t, err)
	again, err := exec.Command("docker", "inspect", container, "--format", "{{.HostConfig.Runtime}}").Output()
	require.NoError(t, err)
	require.Equal(t, "runsc", strings.TrimSpace(string(again)))
}
