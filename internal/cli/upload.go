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
		// G122: the symlink TOCTOU this warns about needs an attacker who can
		// swap entries in the directory being walked. That directory is the
		// user's own working tree on their own machine, and the IsRegular check
		// above already declines to archive anything that is not a plain file.
		f, err := os.Open(path) //nolint:gosec
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
	return c.sendBytes("POST", "/apps/"+appID+"/source", "application/gzip", archive, nil)
}

// UploadIcon sets an app's tile image (R-340). The server decides what the
// bytes are; the content type sent is only a courtesy.
func (c *Client) UploadIcon(appID string, image []byte) error {
	return c.sendBytes("PUT", "/apps/"+appID+"/icon", "application/octet-stream", image, nil)
}

// sendBytes sends a body as-is rather than as JSON, for the endpoints whose
// body is a file. The response, when out is non-nil, is decoded into it.
func (c *Client) sendBytes(method, path, contentType string, body []byte, out any) error {
	req, err := http.NewRequest(method, c.BaseURL+"/api/v1"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
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
	if out != nil {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return decodeInto(raw, out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
