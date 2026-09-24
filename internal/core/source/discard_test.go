package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR224_ADeletedAppsUploadIsRemoved asserts R-224. The archive an app was
// uploaded as stayed on disk after the app was deleted (issue #55).
func TestR224_ADeletedAppsUploadIsRemoved(t *testing.T) {
	old := UploadDir
	UploadDir = t.TempDir()
	t.Cleanup(func() { UploadDir = old })

	_, err := StoreUpload("app_01GONE", strings.NewReader("archive"))
	require.NoError(t, err)
	_, err = StoreUpload("app_01KEEP", strings.NewReader("archive"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(UploadDir, "app_01GONE.tar.gz.partial"), []byte("x"), 0o600))

	require.NoError(t, DiscardUpload("app_01GONE"))
	require.NoFileExists(t, filepath.Join(UploadDir, "app_01GONE.tar.gz"))
	require.NoFileExists(t, filepath.Join(UploadDir, "app_01GONE.tar.gz.partial"))
	require.FileExists(t, filepath.Join(UploadDir, "app_01KEEP.tar.gz"))

	require.NoError(t, DiscardUpload("app_01GONE"), "an app with no upload is not an error")
	for _, bad := range []string{"", "../x", ".hidden", `a\b`} {
		require.Error(t, DiscardUpload(bad), bad)
	}
}
