package buildkit

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bemeek-io/pando/internal/errs"
)

// Forget removes a deleted app's build cache.
//
// Every build exports its cache to a directory on Pando's own disk, one per
// workload under the app's ID, and nothing removed it: a deleted app's cache
// stayed forever, hundreds of megabytes for an ordinary Python app. The GC
// calls this when it tears the app down.
func (a *Adapter) Forget(_ context.Context, namespace string) error {
	app := bundleOf(namespace)
	if app == "" || strings.ContainsAny(app, `/\`) || app == "." || app == ".." {
		return errs.Newf(errs.ValidInvalid, "%q does not name an app's build cache.", namespace)
	}
	if err := os.RemoveAll(cachePath(app)); err != nil {
		return errs.Wrap(errs.AdapterFailed, "Could not remove the app's build cache.", err)
	}
	return nil
}

// digestRef finds every digest a manifest or cache config mentions.
var digestRef = regexp.MustCompile(`sha256:[0-9a-f]{64}`)

// maxManifestBytes bounds how much of a blob is read to look for references.
// Manifests and BuildKit's cache config are kilobytes; layers are gzip, and
// are never parsed.
const maxManifestBytes = 16 << 20

// pruneCacheDir removes blobs that nothing in the cache refers to any more.
//
// BuildKit's local cache export adds the layers of each build and rewrites
// index.json to point at them, and never removes what the previous build
// wrote. An app's cache grew with every build it ever had. What index.json
// reaches — directly, or through a manifest or cache config it names — is kept;
// the rest is an earlier build's.
//
// Conservative by construction: anything a JSON blob mentions is kept, and a
// cache whose index cannot be read is left alone rather than guessed at.
func pruneCacheDir(dir string) (removed int, err error) {
	index, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	blobs := filepath.Join(dir, "blobs", "sha256")
	keep := map[string]bool{}
	queue := digestRef.FindAllString(string(index), -1)
	if len(queue) == 0 {
		return 0, nil
	}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		hex := strings.TrimPrefix(d, "sha256:")
		if keep[hex] {
			continue
		}
		keep[hex] = true
		for _, ref := range referencesIn(filepath.Join(blobs, hex)) {
			if !keep[strings.TrimPrefix(ref, "sha256:")] {
				queue = append(queue, ref)
			}
		}
	}

	entries, err := os.ReadDir(blobs)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || len(name) != 64 || keep[name] {
			continue
		}
		if os.Remove(filepath.Join(blobs, name)) == nil {
			removed++
		}
	}
	return removed, nil
}

// referencesIn returns the digests a JSON blob mentions. Anything that is not
// JSON — a layer — refers to nothing.
func referencesIn(path string) []string {
	// The name is a hex digest digestRef matched, joined under the cache's own
	// blobs directory, so it cannot leave it.
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	head := make([]byte, 1)
	if _, err := io.ReadFull(f, head); err != nil || head[0] != '{' {
		return nil
	}
	rest, err := io.ReadAll(io.LimitReader(f, maxManifestBytes))
	if err != nil {
		return nil
	}
	return digestRef.FindAllString(string(head)+string(rest), -1)
}
