package console_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// anAssetName returns the name of one file the console build actually emitted.
//
// Read from dist/ on disk rather than from the embedded FS, which is
// unexported: the two are the same bytes, because the embed directive is what
// put them there. Any asset will do — the assertion is about the prefix rule,
// not about a particular file, and hashed names change on every build.
func anAssetName(t *testing.T) string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join("dist", "assets"))
	require.NoError(t, err, "the console is built, so dist/assets exists")

	for _, e := range entries {
		if !e.IsDir() {
			return e.Name()
		}
	}
	t.Fatal("dist/assets holds no files")
	return ""
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

	// A real asset is cached immutably. Its name contains the hash of its
	// contents, so the file behind a given URL can never change.
	name := anAssetName(t)
	got = serve(t, "/assets/"+name)
	require.Equal(t, http.StatusOK, got.Code, name)
	require.Equal(t, "public, max-age=31536000, immutable", got.Header().Get("Cache-Control"))

	// A miss carries no caching header at all.
	//
	// This used to assert the opposite — that the prefix decided the header
	// whether or not the file existed. Go's http.Error strips Cache-Control
	// (along with Etag and Last-Modified) before writing an error body, so the
	// 404 comes back uncached, and that is the better answer: an asset added by
	// a later build would otherwise be shadowed for a year by a cached 404 for
	// the same URL.
	got = serve(t, "/assets/does-not-exist.js")
	require.Equal(t, http.StatusNotFound, got.Code)
	require.Empty(t, got.Header().Get("Cache-Control"))
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
