// Package source fetches an app's source and exposes it read-only.
//
// R-020: the source tree is strictly read-only input. Pando looks at it and
// never asks it for permission — there is no pando.yaml, and nothing in a repo
// can change how Pando behaves.
package source

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// Checkout is a fetched source tree on disk.
type Checkout struct {
	// Dir is the root of the working tree.
	Dir string

	// Commit is the resolved SHA. R-120: Ref is what the user asked for, Commit
	// is what runs, and it is written back into the spec so a redeploy of the
	// same revision builds the same code.
	Commit string

	cleanup func()
}

// Close removes the checkout.
func (c *Checkout) Close() {
	if c.cleanup != nil {
		c.cleanup()
	}
}

// View returns a read-only view rooted at the source, honoring Subdir.
func (c *Checkout) View(subdir string) api.SourceView {
	root := c.Dir
	if subdir != "" {
		root = filepath.Join(c.Dir, filepath.Clean("/"+subdir))
	}
	return &dirView{root: root}
}

// Fetch clones an app's source.
//
// The caller must have checked the source allowlist first (R-092) — by the time
// this runs, disk has been written to, which is exactly why that check belongs
// before it and not inside it.
func Fetch(ctx context.Context, src spec.Source) (*Checkout, error) {
	switch src.Type {
	case spec.SourceGit:
		return fetchGit(ctx, src)
	case spec.SourceImage:
		// Nothing to fetch: a prebuilt image is run as it is.
		return &Checkout{Dir: "", Commit: src.Digest}, nil
	default:
		return nil, errs.Newf(errs.ValidInvalid,
			"Pando does not know how to fetch source of type %q.", src.Type)
	}
}

func fetchGit(ctx context.Context, src spec.Source) (*Checkout, error) {
	dir, err := os.MkdirTemp("", "pando-src-")
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not make room to fetch the source.", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	opts := &git.CloneOptions{
		URL:   src.URL,
		Depth: 1,

		// Submodules are not initialized. R-021: Pando fills declared slots and
		// never invents topology, and silently pulling in another repository's
		// contents is the same class of decision.
		RecurseSubmodules: git.NoRecurseSubmodules,
	}

	// A commit takes precedence over a ref: redeploying a pinned revision must
	// build the same code, not whatever the branch points at now (R-120).
	if src.Commit == "" && src.Ref != "" {
		opts.ReferenceName = referenceFor(src.Ref)
		opts.SingleBranch = true
	}

	repo, err := git.PlainCloneContext(ctx, dir, false, opts)
	if err != nil {
		cleanup()
		return nil, errs.Wrap(errs.ValidInvalid,
			"Pando could not fetch this app's source.", err).
			WithDetail("url", src.URL).
			WithRemedy("Check that the repository address and branch are correct, and that this installation can reach it.")
	}

	if src.Commit != "" {
		worktree, err := repo.Worktree()
		if err != nil {
			cleanup()
			return nil, errs.Wrap(errs.Internal, "Could not read the fetched source.", err)
		}
		if err := worktree.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(src.Commit)}); err != nil {
			cleanup()
			return nil, errs.Wrap(errs.ValidInvalid,
				"Pando could not find that commit in this app's repository.", err).
				WithDetail("commit", src.Commit)
		}
	}

	head, err := repo.Head()
	if err != nil {
		cleanup()
		return nil, errs.Wrap(errs.Internal, "Could not read the fetched source.", err)
	}

	return &Checkout{Dir: dir, Commit: head.Hash().String(), cleanup: cleanup}, nil
}

// referenceFor guesses whether a ref names a branch or a tag.
func referenceFor(ref string) plumbing.ReferenceName {
	if strings.HasPrefix(ref, "refs/") {
		return plumbing.ReferenceName(ref)
	}
	return plumbing.NewBranchReferenceName(ref)
}

// dirView is a read-only view of a directory.
//
// It has no write methods, structurally (R-020), and every path is resolved
// inside the root so a traversal cannot reach the host filesystem.
type dirView struct{ root string }

func (v *dirView) resolve(name string) (string, error) {
	clean := filepath.Clean("/" + name)
	full := filepath.Join(v.root, clean)
	if !strings.HasPrefix(full, filepath.Clean(v.root)) {
		return "", errs.Newf(errs.ValidInvalid, "%q is outside the app's source.", name)
	}
	return full, nil
}

// Root exposes the directory the view is rooted at.
//
// A builder that needs the source on a filesystem — BuildKit mounts it rather
// than reading it file by file — asks for this through a narrow interface
// assertion. It is deliberately not part of api.SourceView: a view is read-only
// by construction, and a builder that can reach the path can write to it. The
// assertion makes that capability explicit at the one call site that needs it.
func (v *dirView) Root() string { return v.root }

func (v *dirView) Open(name string) (io.ReadCloser, error) {
	full, err := v.resolve(name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, errs.Wrap(errs.NotFound, "That file is not in the app's source.", err)
	}
	return f, nil
}

func (v *dirView) Stat(name string) (api.FileInfo, error) {
	full, err := v.resolve(name)
	if err != nil {
		return api.FileInfo{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return api.FileInfo{}, errs.Wrap(errs.NotFound, "That file is not in the app's source.", err)
	}
	return api.FileInfo{Name: info.Name(), Size: info.Size(), IsDir: info.IsDir()}, nil
}

func (v *dirView) Glob(pattern string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(v.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			//nolint:nilerr // Returning nil continues the walk. One unreadable
			// entry in a repository must not fail the whole glob — detection
			// would then be defeated by a single bad symlink.
			return nil
		}
		rel, relErr := filepath.Rel(v.root, path)
		if relErr != nil {
			//nolint:nilerr // Same: skip this entry, keep walking.
			return nil
		}
		if match, _ := filepath.Match(pattern, rel); match {
			out = append(out, rel)
		}
		if match, _ := filepath.Match(pattern, d.Name()); match && !contains(out, rel) {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read the app's source.", err)
	}
	return out, nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

var _ api.SourceView = (*dirView)(nil)

// ResolveRef returns the commit a ref currently points at, without cloning.
//
// Auto-deploy runs this every five minutes for every app tracking a branch, and
// the answer is usually "the same as last time". Cloning to find that out would
// make Pando's largest source of network traffic a question it almost never
// needs to act on.
func ResolveRef(ctx context.Context, src spec.Source) (string, error) {
	if src.Type != spec.SourceGit || src.URL == "" {
		return "", nil
	}

	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin", URLs: []string{src.URL},
	})

	refs, err := remote.ListContext(ctx, &git.ListOptions{})
	if err != nil {
		return "", errs.Wrap(errs.ValidInvalid,
			"Pando could not reach this app's source to check for new commits.", err).
			WithDetail("url", src.URL)
	}

	wanted := referenceFor(src.Ref)
	for _, ref := range refs {
		if ref.Name() == wanted {
			return ref.Hash().String(), nil
		}
	}
	return "", nil
}
