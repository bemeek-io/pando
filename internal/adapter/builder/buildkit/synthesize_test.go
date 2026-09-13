package buildkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
)

func repo(t *testing.T, dirs ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}
	return root
}

// TestR110_AStaticSiteNeedsNoDockerfileInTheRepository asserts the strategy
// works without the app carrying build instructions.
//
// A static site is the case R-103 is about — somebody who never thought about
// deployment. Requiring them to write a Dockerfile to serve a folder is the
// per-app setup burden R-002 exists to remove.
func TestR110_AStaticSiteNeedsNoDockerfileInTheRepository(t *testing.T) {
	root := repo(t, "dist")

	dir, name, err := synthesize(api.BuildRequest{
		Strategy: spec.BuildStatic, StaticDir: "dist",
	}, root)
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	require.Equal(t, "Dockerfile", name)
	body, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)

	content := string(body)
	require.Contains(t, content, staticServerImage)
	require.Contains(t, content, "COPY dist/ /usr/share/nginx/html/")

	// Client-side routing works. Without the fallback, reloading on a path the
	// app routes itself asks the server for a file that does not exist.
	require.Contains(t, content, "try_files")

	// Written outside the checkout: the app's own source is never modified by
	// building it.
	require.False(t, strings.HasPrefix(dir, root), "the Dockerfile is not written into the app's source")
}

// The repository root is the default when no directory is named.
func TestAStaticSiteWithNoDirectoryServesTheRoot(t *testing.T) {
	root := repo(t)

	dir, _, err := synthesize(api.BuildRequest{Strategy: spec.BuildStatic}, root)
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	body, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	require.NoError(t, err)
	require.Contains(t, string(body), "COPY ./ /usr/share/nginx/html/")
}

// A directory that is not there fails with the name in it, before a build runs.
func TestAMissingStaticDirectoryIsRefusedByName(t *testing.T) {
	_, _, err := synthesize(api.BuildRequest{
		Strategy: spec.BuildStatic, StaticDir: "public",
	}, repo(t, "dist"))

	require.Error(t, err)
	require.Contains(t, err.Error(), "public", "R-105: the message names what is missing")
}

// A static directory cannot escape the build context.
//
// Without the constraint, "../.." would copy whatever sits beside the checkout
// into a directory served to anyone who can reach the app.
func TestAStaticDirectoryCannotEscapeTheContext(t *testing.T) {
	root := repo(t, "dist")

	dir, _, err := synthesize(api.BuildRequest{
		Strategy: spec.BuildStatic, StaticDir: "../../etc",
	}, root)
	if err == nil {
		defer func() { _ = os.RemoveAll(dir) }()
		body, readErr := os.ReadFile(filepath.Join(dir, "Dockerfile"))
		require.NoError(t, readErr)
		require.NotContains(t, string(body), "..", "a path outside the context never reaches the COPY")
	}
}

// A strategy this builder does not implement is refused here rather than
// producing an empty Dockerfile.
func TestAnUnknownStrategyIsRefused(t *testing.T) {
	_, _, err := synthesize(api.BuildRequest{Strategy: spec.BuildBuildpack}, repo(t))
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildpack")
}
