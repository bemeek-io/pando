package console_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/console"
)

// The console is built by `make console` and embedded. A developer running
// `go run` without it gets a plain message rather than a panic at startup or a
// blank page — so both outcomes have to be correct, and which one a given
// checkout produces depends on whether dist/ holds a build.
func TestHandlerReportsWhetherTheConsoleWasBuilt(t *testing.T) {
	h, built := console.Handler()

	if !built {
		require.Nil(t, h, "an unbuilt console hands back nothing to mount")
		t.Skip("the console was not built into this binary; run `make console` to exercise the rest")
	}
	require.NotNil(t, h)
}

func serve(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	h, built := console.Handler()
	if !built {
		t.Skip("the console was not built into this binary")
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// Serving index.html for a console route is what makes a deep link work on a
// reload: the client router owns those paths, not the file server.
func TestAConsoleRouteServesTheAppSoADeepLinkSurvivesAReload(t *testing.T) {
	for _, path := range []string{"/", "/login", "/admin", "/admin/users", "/apps/app_01HQ8"} {
		got := serve(t, path)
		require.Equal(t, http.StatusOK, got.Code, path)
		require.Contains(t, got.Body.String(), "<", path)
		require.Equal(t, "no-cache", got.Header().Get("Cache-Control"), path)
	}
}

// Hashed assets are immutable by construction — the filename changes when the
// content does — so they are cached hard. index.html is not, or an upgraded
// Pando would serve an old console until every browser happened to revalidate.
func TestOnlyHashedAssetsAreCachedHard(t *testing.T) {
	got := serve(t, "/index.html")
	require.Equal(t, "no-cache", got.Header().Get("Cache-Control"))

	// An asset path is cached immutably whether or not that exact file exists:
	// the header is decided by the prefix, and a miss is still a 404.
	got = serve(t, "/assets/does-not-exist.js")
	require.Equal(t, "public, max-age=31536000, immutable", got.Header().Get("Cache-Control"))
}

// A traversal out of the embedded filesystem must not reach the host, and there
// is nothing above dist to reach anyway — it resolves back to the console.
func TestATraversalDoesNotEscapeTheEmbeddedFiles(t *testing.T) {
	for _, path := range []string{"/../../etc/passwd", "/assets/../../etc/passwd"} {
		got := serve(t, path)
		require.NotEqual(t, http.StatusInternalServerError, got.Code, path)
		require.NotContains(t, got.Body.String(), "root:", path)
	}
}
