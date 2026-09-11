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

	c := &client{http: &http.Client{Timeout: 30 * time.Second}}

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
	return app["id"].(string)
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
