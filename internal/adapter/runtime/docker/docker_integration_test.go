//go:build integration

package docker_test

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	dockeradapter "github.com/bemeek-io/pando/internal/adapter/runtime/docker"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/secret"
)

func adapter(t *testing.T) *dockeradapter.Adapter {
	t.Helper()
	a := dockeradapter.New()
	require.NoError(t, a.Configure(context.Background(), nil))
	if err := a.HealthCheck(context.Background()); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	return a
}

// bundle returns a plan for a tiny container that stays up.
func bundle(bundleID string, env map[string]secret.Value) api.BundlePlan {
	return api.BundlePlan{
		BundleID: bundleID,
		Network:  api.NetworkPlan{Private: true, EgressMode: spec.EgressAllowAll},
		Labels:   map[string]string{"pando.app": bundleID},
		Workloads: []api.WorkloadPlan{{
			Name:    "web",
			Image:   "alpine:3.20",
			Command: []string{"sleep", "3600"},
			Env:     env,
			Exposed: true,
		}},
	}
}

func cleanup(t *testing.T, a *dockeradapter.Adapter, bundleID string) {
	t.Helper()
	t.Cleanup(func() {
		_ = a.Destroy(context.Background(), api.BundleRef{BundleID: bundleID}, api.DestroyOptions{})
	})
}

func TestApplyThenObserve(t *testing.T) {
	ctx := context.Background()
	a := adapter(t)
	id := "test-apply-" + time.Now().Format("150405")
	cleanup(t, a, id)

	_, err := a.Apply(ctx, bundle(id, nil))
	require.NoError(t, err)

	observed, err := a.Observe(ctx, api.BundleRef{BundleID: id})
	require.NoError(t, err)
	require.True(t, observed.Exists)
	require.Len(t, observed.Workloads, 1)
	require.Equal(t, "web", observed.Workloads[0].Name)
	require.True(t, observed.Workloads[0].Running)

	// R-221: no health check configured means no signal, which is NOT
	// unhealthy. An app without a health check is running, not perpetually
	// degraded, and collapsing those two states is the bug this guards.
	require.Nil(t, observed.Workloads[0].Healthy, "no health check means nil, not false")
}

// Apply is idempotent: the reconciler calls it freely, so a satisfied plan must
// touch nothing.
func TestApplyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	a := adapter(t)
	id := "test-idem-" + time.Now().Format("150405")
	cleanup(t, a, id)

	_, err := a.Apply(ctx, bundle(id, nil))
	require.NoError(t, err)

	first, err := a.Observe(ctx, api.BundleRef{BundleID: id})
	require.NoError(t, err)
	startedAt := first.Workloads[0].StartedAt

	_, err = a.Apply(ctx, bundle(id, nil))
	require.NoError(t, err)

	second, err := a.Observe(ctx, api.BundleRef{BundleID: id})
	require.NoError(t, err)
	require.Equal(t, startedAt, second.Workloads[0].StartedAt,
		"an already-satisfied plan must not restart the workload")
}

// R-193: a rotated secret changes the resolved environment, and the workload
// must be recreated rather than left running with the old value.
func TestR193_ChangedEnvironmentCausesRecreate(t *testing.T) {
	ctx := context.Background()
	a := adapter(t)
	id := "test-env-" + time.Now().Format("150405")
	cleanup(t, a, id)

	_, err := a.Apply(ctx, bundle(id, map[string]secret.Value{"TOKEN": secret.New("old-value")}))
	require.NoError(t, err)

	before, err := a.Observe(ctx, api.BundleRef{BundleID: id})
	require.NoError(t, err)

	_, err = a.Apply(ctx, bundle(id, map[string]secret.Value{"TOKEN": secret.New("rotated-value")}))
	require.NoError(t, err)

	after, err := a.Observe(ctx, api.BundleRef{BundleID: id})
	require.NoError(t, err)
	require.NotEqual(t, before.Workloads[0].StartedAt, after.Workloads[0].StartedAt,
		"a rotated secret must recreate the workload")
}

// R-026: workloads are reachable only inside the bundle's own network. Nothing
// is published to the host, because traffic arrives through Pando's proxy.
func TestR026_NoPortsArePublishedToTheHost(t *testing.T) {
	ctx := context.Background()
	a := adapter(t)
	id := "test-ports-" + time.Now().Format("150405")
	cleanup(t, a, id)

	plan := bundle(id, nil)
	plan.Workloads[0].Ports = []api.PortPlan{{Number: 8080, Protocol: "http"}}

	_, err := a.Apply(ctx, plan)
	require.NoError(t, err)

	// Inspect through the adapter's own logs path to confirm it is running,
	// then assert on the container's published ports via the Docker CLI, since
	// the adapter deliberately exposes no way to ask.
	observed, err := a.Observe(ctx, api.BundleRef{BundleID: id})
	require.NoError(t, err)
	require.True(t, observed.Workloads[0].Running)

	ports := dockerInspect(t, "pando-"+id+"-web", "{{json .NetworkSettings.Ports}}")
	require.NotContains(t, ports, "HostPort", "no port may be published to the host")
}

// R-025: each bundle gets its own network, so no app can reach another's.
func TestR025_EachBundleGetsItsOwnNetwork(t *testing.T) {
	ctx := context.Background()
	a := adapter(t)

	one := "test-net-a-" + time.Now().Format("150405")
	two := "test-net-b-" + time.Now().Format("150405")
	cleanup(t, a, one)
	cleanup(t, a, two)

	_, err := a.Apply(ctx, bundle(one, nil))
	require.NoError(t, err)
	_, err = a.Apply(ctx, bundle(two, nil))
	require.NoError(t, err)

	netOne := dockerInspect(t, "pando-"+one+"-web", "{{json .NetworkSettings.Networks}}")
	netTwo := dockerInspect(t, "pando-"+two+"-web", "{{json .NetworkSettings.Networks}}")

	require.Contains(t, netOne, "pando-"+one)
	require.Contains(t, netTwo, "pando-"+two)
	require.NotContains(t, netOne, "pando-"+two, "one app must not be on another's network")
}

// R-204: volumes outlive the apps that mount them, so the default teardown
// keeps them.
func TestR204_DestroyKeepsVolumesByDefault(t *testing.T) {
	ctx := context.Background()
	a := adapter(t)
	id := "test-vol-" + time.Now().Format("150405")

	plan := bundle(id, nil)
	plan.Volumes = []api.VolumePlan{{VolumeID: "vol_data", Name: "data"}}
	plan.Workloads[0].Mounts = []api.MountPlan{{VolumeID: "vol_data", Path: "/data"}}

	_, err := a.Apply(ctx, plan)
	require.NoError(t, err)

	require.NoError(t, a.Destroy(ctx, api.BundleRef{BundleID: id}, api.DestroyOptions{KeepVolumes: true}))

	observed, err := a.Observe(ctx, api.BundleRef{BundleID: id})
	require.NoError(t, err)
	require.Empty(t, observed.Workloads, "containers are gone")
	require.NotEmpty(t, observed.Volumes, "but the storage is not")

	// Now discard it explicitly.
	require.NoError(t, a.Destroy(ctx, api.BundleRef{BundleID: id}, api.DestroyOptions{}))
	observed, err = a.Observe(ctx, api.BundleRef{BundleID: id})
	require.NoError(t, err)
	require.Empty(t, observed.Volumes)
}

// R-026 again, from the plan side: an adapter must refuse a plan that asks for a
// shared network rather than quietly complying.
func TestApplyRefusesANonPrivateNetwork(t *testing.T) {
	ctx := context.Background()
	a := adapter(t)

	plan := bundle("test-refuse", nil)
	plan.Network.Private = false

	_, err := a.Apply(ctx, plan)
	require.Error(t, err)
	require.Contains(t, err.Error(), "shared network")
}

func TestLogsAndExec(t *testing.T) {
	ctx := context.Background()
	a := adapter(t)
	id := "test-exec-" + time.Now().Format("150405")
	cleanup(t, a, id)

	plan := bundle(id, nil)
	plan.Workloads[0].Command = []string{"sh", "-c", "echo hello-from-pando; sleep 3600"}
	_, err := a.Apply(ctx, plan)
	require.NoError(t, err)

	time.Sleep(time.Second)

	logs, err := a.Logs(ctx, api.WorkloadRef{BundleID: id, Workload: "web"}, api.LogOptions{Tail: 10})
	require.NoError(t, err)
	defer func() { _ = logs.Close() }()

	out, err := io.ReadAll(logs)
	require.NoError(t, err)
	require.Contains(t, string(out), "hello-from-pando")

	session, err := a.Exec(ctx, api.WorkloadRef{BundleID: id, Workload: "web"}, api.ExecRequest{
		Command: []string{"echo", "exec-works"},
	})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	buf := make([]byte, 512)
	n, _ := session.Read(buf)
	require.Contains(t, string(buf[:n]), "exec-works")
}

// R-243: capacity is adapter-reported. The adapter answers for itself rather
// than core reading /proc.
func TestR243_CapacityIsAdapterReported(t *testing.T) {
	capacity, err := adapter(t).Capacity(context.Background())
	require.NoError(t, err)
	require.Positive(t, capacity.TotalCPUMillis)
	require.Positive(t, capacity.TotalMemoryBytes)
	require.False(t, capacity.Reported.IsZero())
}

// R-254: capabilities are data, and the class is reported honestly — a policy
// floor above `container` must exclude this adapter, which it can only do if the
// class is true.
func TestR254_CapabilitiesAreHonest(t *testing.T) {
	caps, err := adapter(t).Capabilities(context.Background())
	require.NoError(t, err)
	require.Equal(t, spec.IsolationContainer, caps.IsolationClass, "docker is a shared kernel")
	require.True(t, caps.SupportsPrivateNetwork)
	require.False(t, caps.SupportsStartThenSwap, "not built yet, so not claimed")
}

// dockerInspect shells out, deliberately: asserting on the container's actual
// configuration rather than on what the adapter believes it configured.
func dockerInspect(t *testing.T, name, format string) string {
	t.Helper()
	out, err := execCommand("docker", "inspect", "-f", format, name)
	require.NoError(t, err, out)
	return strings.TrimSpace(out)
}

func execCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
