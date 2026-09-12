//go:build integration

package acceptance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The acceptance tests drive a running Compose stack over HTTP, as a client
// would. Nothing is stubbed: the assertions are about the shipped topology.
//
// Bring the stack up first:
//
//	PANDO_PORT=8099 docker compose up -d
//	go test -tags=integration ./test/acceptance/
func baseURL() string {
	if v := os.Getenv("PANDO_TEST_URL"); v != "" {
		return v
	}
	return "http://localhost:8099/api/v1"
}

// requireStack skips rather than fails when the stack is not up, so a developer
// running the whole suite without Compose sees a skip instead of noise.
func requireStack(t *testing.T) {
	t.Helper()
	resp, err := http.Get(strings.TrimSuffix(baseURL(), "/api/v1") + "/healthz")
	if err != nil {
		t.Skipf("pando stack not running at %s: %v", baseURL(), err)
	}
	_ = resp.Body.Close()
}

type client struct {
	http   *http.Client
	cookie string
}

var passwordPattern = regexp.MustCompile(`"password":"([^"]+)"`)

// login signs in as the first-run administrator.
func login(t *testing.T) *client {
	t.Helper()
	requireStack(t)

	out, err := exec.Command("docker", "compose", "logs", "pando").CombinedOutput()
	require.NoError(t, err)

	matches := passwordPattern.FindStringSubmatch(string(out))
	require.Len(t, matches, 2,
		"could not find the first-run password in the server log — bring the stack up fresh with `docker compose down -v && docker compose up -d`")

	// Generous, because POST /deployments is not the quick call its 202 status
	// suggests. It resolves the app's ref to a commit before returning, and
	// resolving a ref means cloning — design 01 §2.1 is explicit that a deploy
	// never resolves a ref implicitly at runtime, so the work happens here. A
	// cold clone of a real repository takes longer than a conversational
	// timeout allows.
	c := &client{http: &http.Client{Timeout: 2 * time.Minute}}

	resp, err := c.http.Post(baseURL()+"/sessions", "application/json",
		strings.NewReader(fmt.Sprintf(`{"username":"admin","password":%q}`, matches[1])))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	for _, ck := range resp.Cookies() {
		if ck.Name == "pando_session" {
			c.cookie = ck.Value
		}
	}
	require.NotEmpty(t, c.cookie)
	return c
}

func (c *client) do(t *testing.T, method, path, body string) (string, int) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, baseURL()+path, reader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "pando_session", Value: c.cookie})

	resp, err := c.http.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(out), resp.StatusCode
}

func (c *client) postRaw(t *testing.T, path, body string) (string, int) {
	return c.do(t, http.MethodPost, path, body)
}

func (c *client) get(t *testing.T, path string) map[string]any {
	t.Helper()
	body, status := c.do(t, http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, status, body)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &out))
	return out
}

func (c *client) put(t *testing.T, path, body string) {
	t.Helper()
	out, status := c.do(t, http.MethodPut, path, body)
	require.Less(t, status, 300, out)
}

func (c *client) createApp(t *testing.T, name string) string {
	t.Helper()
	body, status := c.do(t, http.MethodPost, "/apps", fmt.Sprintf(`{"name":%q}`, name))
	require.Equal(t, http.StatusAccepted, status, body)

	var app map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &app))

	appID := app["id"].(string)
	cleanupBundle(t, appID)
	return appID
}

// cleanupBundle removes the containers a deployed test app leaves running.
//
// Nothing in Pando does this, and that is a real gap rather than a test
// convenience: deleting an app archives the row and never tells the runtime to
// destroy the bundle. The adapter implements Destroy and no caller invokes it.
// Convergence — including tearing down what should no longer exist — is the
// reconciler's job, and the reconciler is phase 7.
//
// Containers only. The bundle's private network is deliberately left alone:
// Pando's own container is joined to every one of them, because that is how the
// proxy reaches an app at all (R-023), and force-disconnecting a running Pando
// from a bridge network disturbs its routing badly enough that its next
// outbound clone hangs for minutes. That was a real failure here, and it looked
// exactly like a slow network rather than like the test suite sabotaging the
// server. Networks are reclaimed at suite start instead, by pruneStaleBundles,
// when the Pando attached to them is already gone.
func cleanupBundle(t *testing.T, appID string) {
	t.Helper()
	t.Cleanup(func() {
		out, _ := exec.Command("docker", "ps", "-aq",
			"--filter", "label=io.pando.app="+appID).Output()
		for _, id := range strings.Fields(string(out)) {
			_ = exec.Command("docker", "rm", "-f", id).Run()
		}
	})
}

// TestMain reclaims what previous runs left behind.
//
// Bundle networks are not Compose-managed, so `docker compose down -v` does not
// remove them and they accumulate one per deployed app across runs. Docker's
// default address pool holds about thirty; once it is full every deploy fails
// with a capacity error that has nothing to do with the test reporting it.
//
// Safe here in a way it is not mid-suite: the Pando that was attached to these
// networks belongs to a previous stack and is already gone.
func TestMain(m *testing.M) {
	pruneStaleBundles()
	os.Exit(m.Run())
}

func pruneStaleBundles() {
	out, err := exec.Command("docker", "network", "ls", "-q",
		"--filter", "label=io.pando.managed").Output()
	if err != nil {
		return
	}

	for _, network := range strings.Fields(string(out)) {
		attached, _ := exec.Command("docker", "network", "inspect", network,
			"--format", "{{range .Containers}}{{.Name}} {{end}}").Output()
		for _, name := range strings.Fields(string(attached)) {
			_ = exec.Command("docker", "network", "disconnect", "-f", network, name).Run()
		}
		_ = exec.Command("docker", "network", "rm", network).Run()
	}
}

func (c *client) putSpec(t *testing.T, appID, spec string) {
	t.Helper()
	body, status := c.do(t, http.MethodPost, fmt.Sprintf("/apps/%s/specs", appID), spec)
	require.Equal(t, http.StatusCreated, status, body)
}

func (c *client) pinSpec(t *testing.T, appID string, revision int) {
	t.Helper()
	body, status := c.do(t, http.MethodPost,
		fmt.Sprintf("/apps/%s/specs/%d/pin", appID, revision), "")
	require.Equal(t, http.StatusOK, status, body)
}

func (c *client) deploy(t *testing.T, appID string, revision int) map[string]any {
	t.Helper()
	payload := "{}"
	if revision > 0 {
		payload = fmt.Sprintf(`{"spec_revision":%d}`, revision)
	}
	body, status := c.do(t, http.MethodPost, fmt.Sprintf("/apps/%s/deployments", appID), payload)
	require.Equal(t, http.StatusAccepted, status, body)

	var dep map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &dep))
	return dep
}

// awaitDeployment polls until the deployment reaches a terminal status.
func (c *client) awaitDeployment(t *testing.T, appID, depID string, within time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(within)

	for {
		dep := c.get(t, fmt.Sprintf("/apps/%s/deployments/%s", appID, depID))
		switch dep["status"] {
		case "succeeded", "failed", "superseded":
			return dep
		}
		if time.Now().After(deadline) {
			t.Fatalf("deployment %s did not finish within %s (last status %v)\n%s",
				depID, within, dep["status"], c.deploymentLogs(t, appID, depID))
		}
		time.Sleep(2 * time.Second)
	}
}

// deploymentLogs reads the SSE stream to its end.
func (c *client) deploymentLogs(t *testing.T, appID, depID string) string {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/apps/%s/deployments/%s/logs", baseURL(), appID, depID), nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "pando_session", Value: c.cookie})

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var lines []string
	for _, line := range strings.Split(string(body), "\n") {
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			lines = append(lines, after)
		}
	}
	return strings.Join(lines, "\n")
}

// prebuiltSpec is a spec that needs no build, so a test can assert on the
// pipeline rather than on a build.
func prebuiltSpec(port int) string {
	return fmt.Sprintf(`{
		"schema_version": 1,
		"source": {"type": "image", "image": "nginx:alpine"},
		"build": {"strategy": "prebuilt"},
		"workloads": [{"name": "web", "primary": true, "exposed": true,
			"ports": [{"number": 80, "protocol": "http", "source": "user"}]}],
		"routing": {"adapter_ref": "rte_loopback", "mode": "port", "port": %d},
		"runtime": {"adapter_ref": "rt_docker", "isolation_floor": 10},
		"deploy": {"strategy": "recreate"}
	}`, port)
}

// createUser adds a local user and returns its ID.
func (c *client) createUser(t *testing.T, username string) string {
	t.Helper()
	body, status := c.do(t, http.MethodPost, "/users",
		fmt.Sprintf(`{"username":%q,"password":"correct-horse-battery"}`, username))
	require.Equal(t, http.StatusCreated, status, body)

	var user map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &user))
	return user["id"].(string)
}

// asUser signs in as another account and returns a client for it.
func (c *client) asUser(t *testing.T, userID string) *client {
	t.Helper()

	username := c.get(t, "/users/"+userID)["external_id"].(string)
	other := &client{http: &http.Client{Timeout: 30 * time.Second}}

	resp, err := other.http.Post(baseURL()+"/sessions", "application/json",
		strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"correct-horse-battery"}`, username)))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	for _, ck := range resp.Cookies() {
		if ck.Name == "pando_session" {
			other.cookie = ck.Value
		}
	}
	require.NotEmpty(t, other.cookie)
	return other
}
