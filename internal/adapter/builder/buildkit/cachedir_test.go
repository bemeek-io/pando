package buildkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeBlob(t *testing.T, dir, content string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	name := hex.EncodeToString(sum[:])
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "blobs", "sha256"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blobs", "sha256", name), []byte(content), 0o644))
	return name
}

// TestR224_ABuildCacheKeepsOnlyWhatItsIndexReaches asserts R-224. BuildKit's
// local cache export adds every build's layers and never removes the last
// build's, so an app's cache grew for as long as the app existed.
func TestR224_ABuildCacheKeepsOnlyWhatItsIndexReaches(t *testing.T) {
	dir := t.TempDir()

	layer := writeBlob(t, dir, "\x1f\x8b current layer")
	config := writeBlob(t, dir, fmt.Sprintf(`{"layers":[{"blob":"sha256:%s"}]}`, layer))
	manifest := writeBlob(t, dir, fmt.Sprintf(
		`{"manifests":[{"digest":"sha256:%s"},{"digest":"sha256:%s"}]}`, layer, config))
	stale := writeBlob(t, dir, "\x1f\x8b an earlier build's layer")
	staleManifest := writeBlob(t, dir, fmt.Sprintf(`{"manifests":[{"digest":"sha256:%s"}]}`, stale))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"),
		[]byte(fmt.Sprintf(`{"manifests":[{"digest":"sha256:%s"}]}`, manifest)), 0o644))

	removed, err := pruneCacheDir(dir)
	require.NoError(t, err)
	require.Equal(t, 2, removed)

	for _, kept := range []string{layer, config, manifest} {
		require.FileExists(t, filepath.Join(dir, "blobs", "sha256", kept))
	}
	for _, gone := range []string{stale, staleManifest} {
		require.NoFileExists(t, filepath.Join(dir, "blobs", "sha256", gone))
	}
}

// A cache Pando cannot read the index of is left alone rather than guessed at.
func TestAnUnreadableCacheIndexPrunesNothing(t *testing.T) {
	dir := t.TempDir()
	blob := writeBlob(t, dir, "\x1f\x8b a layer")

	removed, err := pruneCacheDir(dir)
	require.NoError(t, err, "no index yet: nothing to do")
	require.Zero(t, removed)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), []byte("not json"), 0o644))
	removed, err = pruneCacheDir(dir)
	require.NoError(t, err)
	require.Zero(t, removed)
	require.FileExists(t, filepath.Join(dir, "blobs", "sha256", blob))
}

// TestR224_ADeletedAppsBuildCacheIsRemoved asserts R-224: a deleted app's
// cache stayed on disk forever (issue #55).
func TestR224_ADeletedAppsBuildCacheIsRemoved(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PANDO_BUILD_CACHE_DIR", root)

	for _, ns := range []string{"app_01GONE/web", "app_01GONE/worker", "app_01KEEP"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, ns, "blobs"), 0o755))
	}

	a := &Adapter{}
	require.NoError(t, a.Forget(context.Background(), "app_01GONE"))
	require.NoDirExists(t, filepath.Join(root, "app_01GONE"))
	require.DirExists(t, filepath.Join(root, "app_01KEEP"), "another app's cache is not touched")

	require.NoError(t, a.Forget(context.Background(), "app_01GONE"), "forgetting twice is not an error")

	for _, bad := range []string{"", "..", "../etc", `a\b`} {
		require.Error(t, a.Forget(context.Background(), bad), bad)
	}
	require.DirExists(t, root)
}
