package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Packing a directory for `pando deploy ./` (design 04 §4).

// skipped are directories never worth uploading.
//
// Not a .gitignore parser. This is the short list of things that are always
// wrong to send — dependency trees a build will recreate, VCS metadata a deploy
// never reads (R-020), and local editor state. Everything else goes, because
// guessing which of someone's files matter is how an upload silently omits the
// one that did.
var skipped = map[string]bool{
	".git":         true,
	"node_modules": true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	".terraform":   true,
	"vendor":       true,
	".DS_Store":    true,
	".idea":        true,
	".vscode":      true,
}

// PackDirectory writes dir as a gzipped tar, returning the archive and how many
// files it holds.
func PackDirectory(dir string) ([]byte, int, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, 0, fmt.Errorf("reading %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, 0, fmt.Errorf("%s is not a directory", dir)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	count := 0

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if path != root && skipped[name] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}

		// Directories are not written as entries: the server creates parents as
		// it extracts, and an empty directory carries nothing a build needs.
		if d.IsDir() {
			return nil
		}
		// Regular files only. A symlink in the archive is a symlink the server
		// would have to decide whether to follow, and the safe answer there is
		// to not have the question.
		if !d.Type().IsRegular() {
			return nil
		}

		fi, err := d.Info()
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()

		if err := tw.WriteHeader(&tar.Header{
			Name: filepath.ToSlash(rel), Mode: int64(fi.Mode().Perm()),
			Size: fi.Size(), ModTime: fi.ModTime(), Typeflag: tar.TypeReg,
		}); err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		count++
		return nil
	})
	if walkErr != nil {
		return nil, 0, walkErr
	}

	if err := tw.Close(); err != nil {
		return nil, 0, err
	}
	if err := gz.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), count, nil
}

// UploadSource sends a packed directory as an app's source.
func (c *Client) UploadSource(appID string, archive []byte) error {
	req, err := http.NewRequest("POST",
		c.BaseURL+"/api/v1/apps/"+appID+"/source", bytes.NewReader(archive))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/gzip")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("reaching %s: %w", c.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		apiErr := &APIError{Status: resp.StatusCode}
		raw, _ := io.ReadAll(resp.Body)
		if err := decodeInto(raw, apiErr); err != nil || apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(string(raw))
			if apiErr.Message == "" {
				apiErr.Message = fmt.Sprintf("The server returned %d.", resp.StatusCode)
			}
		}
		return apiErr
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
