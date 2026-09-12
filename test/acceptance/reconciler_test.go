//go:build integration

package acceptance_test

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Phase 7's "done when": killing a container by hand restores it, and killing
// it repeatedly reaches failed and stays there.
//
// Against the shipped stack with its real reconciler loop running, because the
// interesting part is not the algorithm — that has unit coverage — but whether
// the loop is actually running, actually observing, and actually converging on
// a machine where someone can reach in and break things.

// containersFor returns the running container IDs for an app's bundle.
func containersFor(t *testing.T, appID string) []string {
	t.Helper()
	out, err := exec.Command("docker", "ps", "-q",
		"--filter", "label=io.pando.app="+appID).Output()
	require.NoError(t, err)
	return strings.Fields(string(out))
}

// deployedApp brings up a real app and returns its ID.
func deployedApp(t *testing.T, c *client, name string) string {
	t.Helper()

	app := c.createApp(t, name)
	c.putSpec(t, app, fmt.Sprintf(`{
		"schema_version": 1,
		"source": {"type": "image", "image": "nginx:1.27-alpine"},
		"build": {"strategy": "prebuilt"},
		"workloads": [{"name": "web", "primary": true, "exposed": true,
			"image": "nginx:1.27-alpine", "ports": [{"number": 80, "protocol": "http", "source": "user"}]}],
		"routing": {"adapter_ref": "rte_loopback", "mode": "port", "port": %d},
		"runtime": {"adapter_ref": "rt_docker", "isolation_floor": 10},
		"deploy": {"strategy": "recreate"}
	}`, 9300+time.Now().Second()%50))
	c.pinSpec(t, app, 1)

	dep := c.deploy(t, app, 0)
	final := c.awaitDeployment(t, app, dep["id"].(string), 10*time.Minute)
	require.Equal(t, "succeeded", final["status"], c.deploymentLogs(t, app, dep["id"].(string)))

	require.Len(t, containersFor(t, app), 1)
	return app
}

// awaitState polls until the app reaches one of the given states.
func awaitState(t *testing.T, c *client, appID string, within time.Duration, states ...string) string {
	t.Helper()
	deadline := time.Now().Add(within)

	var last string
	for time.Now().Before(deadline) {
		last = c.get(t, "/apps/"+appID)["state"].(string)
		for _, s := range states {
			if last == s {
				return last
			}
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("app %s stayed in %q, never reached %v", appID, last, states)
	return ""
}

// R-148: drift the reconciler can correct, it corrects.
func TestR148_AKilledContainerIsRestored(t *testing.T) {
	c := login(t)
	app := deployedApp(t, c, "rec-restore-"+stamp())

	before := containersFor(t, app)
	require.Len(t, before, 1)

	// Reach in and break it, exactly as a person would.
	require.NoError(t, exec.Command("docker", "rm", "-f", before[0]).Run())
	require.Empty(t, containersFor(t, app))

	// Wait for the container, not for the state. The app's state is still
	// `running` the instant after the container is killed — Pando has not
	// looked yet — so waiting on state would pass without the reconciler
	// having done anything at all.
	var after []string
	require.Eventually(t, func() bool {
		after = containersFor(t, app)
		return len(after) == 1
	}, 3*time.Minute, 2*time.Second, "the reconciler never brought the container back")

	require.NotEqual(t, before[0], after[0], "it is a new container, not the old one resurrected")
	require.Equal(t, "running", awaitState(t, c, app, 2*time.Minute, "running"),
		"and it settles back to running once the replacement is observed healthy")
}

// R-150 and R-151: an app that keeps dying reaches failed, and stays there.
//
// A crash-looping container is the ordinary shape of "killing it repeatedly":
// the workload exists, it has exited, Pando recreates it, it exits again. Apply
// succeeds every time — the container really is created — which is exactly the
// case that showed the failure counter was measuring the wrong thing.
//
// The second half is the one that matters. `failed` is terminal because there
// is no code path out of it, and the only way to show that is to wait past
// every backoff interval and find nothing has happened.
func TestR151_ACrashLoopingAppReachesFailedAndStaysThere(t *testing.T) {
	c := login(t)

	app := c.createApp(t, "rec-failed-"+stamp())
	c.putSpec(t, app, `{
		"schema_version": 1,
		"source": {"type": "image", "image": "alpine:3.20"},
		"build": {"strategy": "prebuilt"},
		"workloads": [{"name": "web", "primary": true, "exposed": true,
			"image": "alpine:3.20", "command": ["sh", "-c", "exit 1"],
			"ports": [{"number": 80, "protocol": "http", "source": "user"}]}],
		"routing": {"adapter_ref": "rte_loopback", "mode": "port", "port": 9399},
		"runtime": {"adapter_ref": "rt_docker", "isolation_floor": 10},
		"deploy": {"strategy": "recreate"}
	}`)
	c.pinSpec(t, app, 1)

	dep := c.deploy(t, app, 0)
	c.awaitDeployment(t, app, dep["id"].(string), 10*time.Minute)

	// Sized from the schedule the stack is actually running, rather than from
	// the production numbers. At those, this one test is forty minutes of a
	// forty-three minute suite — and it is asserting the state machine, not the
	// durations. See PANDO_RECONCILER_BACKOFF in the README.
	reachFailed, stayFailed := crashLoopDeadlines()

	state := awaitState(t, c, app, reachFailed, "failed")
	require.Equal(t, "failed", state)

	// And stays. The window is past every retry interval the loop has, so if
	// anything were still trying, it would have tried by now.
	deadline := time.Now().Add(stayFailed)
	for time.Now().Before(deadline) {
		require.Equal(t, "failed", c.get(t, "/apps/"+app)["state"],
			"a failed app stays failed until a person intervenes (R-151)")
		time.Sleep(stayFailed / 6)
	}
}

// crashLoopDeadlines works out how long to wait from the schedule the server is
// running, read from the same environment variables that configured it.
//
// Derived rather than hardcoded so the test cannot quietly pass for the wrong
// reason: with a compressed schedule a fixed 40-minute deadline would still
// pass, but it would also pass if the give-up rule had stopped working and the
// app reached failed by some other route.
func crashLoopDeadlines() (reachFailed, stayFailed time.Duration) {
	schedule := []time.Duration{0, 5 * time.Second, 15 * time.Second, 60 * time.Second, 5 * time.Minute}
	if raw := os.Getenv("PANDO_RECONCILER_BACKOFF"); raw != "" {
		var parsed []time.Duration
		ok := true
		for _, part := range strings.Split(raw, ",") {
			d, err := time.ParseDuration(strings.TrimSpace(part))
			if err != nil {
				ok = false
				break
			}
			parsed = append(parsed, d)
		}
		if ok && len(parsed) > 0 {
			schedule = parsed
		}
	}

	threshold := 10
	if raw := os.Getenv("PANDO_RECONCILER_FAILURE_THRESHOLD"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			threshold = n
		}
	}

	// The sum of the first `threshold` steps, extending the last one.
	var total time.Duration
	for i := 0; i < threshold; i++ {
		step := schedule[len(schedule)-1]
		if i < len(schedule) {
			step = schedule[i]
		}
		total += step
	}

	// Double it, plus the reconcile tick and the time each attempt takes to
	// actually fail. Generous, because a flaky deadline in this test is worse
	// than a slow one.
	reachFailed = 2*total + 2*time.Minute
	stayFailed = schedule[len(schedule)-1]*2 + 30*time.Second
	return reachFailed, stayFailed
}

// Design 05 §2.1.1: Pando stopping does not stop apps, and coming back does not
// restart what never stopped.
func TestPandoRestartingLeavesARunningAppAlone(t *testing.T) {
	c := login(t)
	app := deployedApp(t, c, "rec-survive-"+stamp())

	before := containersFor(t, app)
	require.Len(t, before, 1)

	out, err := execCompose("restart", "pando")
	require.NoError(t, err, out)

	require.Eventually(t, func() bool {
		_, err := execCompose("exec", "-T", "pando", "true")
		return err == nil
	}, 2*time.Minute, 3*time.Second)

	// Same container, still running: it kept serving while Pando was away, and
	// Pando converged to it rather than restarting it for tidiness.
	require.Equal(t, before, containersFor(t, app))

	c2 := login(t)
	require.Equal(t, "running", awaitState(t, c2, app, 2*time.Minute, "running"))
	require.Equal(t, before, containersFor(t, app))
}
