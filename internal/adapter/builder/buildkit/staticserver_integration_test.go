//go:build integration

package buildkit

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/adapter/api"
)

// TestR160_AStaticSitesDirectoryRedirectKeepsItsPort asserts R-160.
//
// The generated static server is built and run as it would be deployed, and
// asked for a directory without its trailing slash on an address with a port.
// nginx's default absolute redirect dropped the port and sent a port-mode app's
// visitors to port 80, where nothing listens (issue #67).
func TestR160_AStaticSitesDirectoryRedirectKeepsItsPort(t *testing.T) {
	root := pagesSite(t)
	dir, name, err := staticDockerfile(api.BuildRequest{StaticDir: "."}, root)
	require.NoError(t, err)

	tag := fmt.Sprintf("pando-test-static-%d", time.Now().UnixNano())
	docker(t, "build", "-q", "-t", tag, "-f", dir+"/"+name, root)
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", "-f", tag).Run() })

	container := docker(t, "run", "-d", "-p", "127.0.0.1::80", tag)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })
	hostPort := docker(t, "port", container, "80/tcp")
	hostPort = strings.Split(hostPort, "\n")[0]

	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	var resp *http.Response
	require.Eventually(t, func() bool {
		req, _ := http.NewRequest(http.MethodGet, "http://"+hostPort+"/solutions", nil)
		// The address a port-mode app is reached at: a name and a port.
		req.Host = "localhost:9001"
		resp, err = client.Do(req) //nolint:bodyclose // closed below
		return err == nil
	}, 30*time.Second, 200*time.Millisecond)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusMovedPermanently, resp.StatusCode)
	location := resp.Header.Get("Location")
	require.Equal(t, "/solutions/", location,
		fmt.Sprintf("the redirect must not name a host without its port; got %q", location))
}

func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	require.NoError(t, err, "docker %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}
