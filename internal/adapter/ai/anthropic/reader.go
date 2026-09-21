package anthropic

import (
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// reader is the repository, bounded.
//
// Every read goes through here rather than straight to the SourceView, for two
// reasons that are not the same. The budget (R-339) is one: a screening has a
// ceiling in files and bytes and something has to hold the running total. The
// other is that this is the list of what left the host, which R-337 says the
// audit event carries — and a count kept beside the reads is a count that
// cannot disagree with them.
type reader struct {
	src      api.SourceView
	maxFiles int
	maxBytes int64

	mu    sync.Mutex
	read  map[string]bool
	order []string
	bytes int64
}

func newReader(src api.SourceView, maxFiles int, maxBytes int64) *reader {
	return &reader{src: src, maxFiles: maxFiles, maxBytes: maxBytes, read: map[string]bool{}}
}

var errBudget = errors.New("budget spent")

// open reads one file, or says why it did not.
func (r *reader) open(name string) (string, error) {
	clean, err := safe(name)
	if err != nil {
		return "", err
	}

	r.mu.Lock()
	already := r.read[clean]
	files, used := len(r.read), r.bytes
	r.mu.Unlock()

	// A re-read is free and does not count: the model asking for a file twice
	// is a model that lost track, not one spending a second file of budget.
	if !already {
		if files >= r.maxFiles {
			return "", fmt.Errorf("%w: %d files is this screening's limit", errBudget, r.maxFiles)
		}
		if used >= r.maxBytes {
			return "", fmt.Errorf("%w: %d bytes is this screening's limit", errBudget, r.maxBytes)
		}
	}

	f, err := r.src.Open(clean)
	if err != nil {
		return "", fmt.Errorf("%s could not be read: %w", clean, err)
	}
	defer f.Close() //nolint:errcheck // read-only; a close error says nothing useful.

	// Bounded per file as well as in total, so one enormous lockfile cannot
	// spend the whole budget in a single call.
	limit := int64(maxFileBytes)
	if remaining := r.maxBytes - used; remaining < limit {
		limit = remaining
	}
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", fmt.Errorf("%s could not be read: %w", clean, err)
	}

	truncated := int64(len(body)) > limit
	if truncated {
		body = body[:limit]
	}

	r.mu.Lock()
	if !r.read[clean] {
		r.read[clean] = true
		r.order = append(r.order, clean)
		r.bytes += int64(len(body))
	}
	r.mu.Unlock()

	out := string(body)
	if truncated {
		out += "\n\n[truncated: this file is longer than a screening reads]"
	}
	return out, nil
}

// glob lists paths without counting against the file budget.
//
// Listing is not reading. The budget exists to bound what content leaves the
// host, and a list of names is how the model decides which of them to spend the
// budget on — charging for it would make an efficient screening impossible.
func (r *reader) glob(pattern string) ([]string, error) {
	clean, err := safe(pattern)
	if err != nil {
		return nil, err
	}
	matches, err := r.src.Glob(clean)
	if err != nil {
		return nil, fmt.Errorf("%s could not be listed: %w", clean, err)
	}
	sort.Strings(matches)
	if len(matches) > maxGlobResults {
		matches = matches[:maxGlobResults]
	}
	return matches, nil
}

const maxGlobResults = 200

// files is what was read, in the order it was read, for the audit event.
func (r *reader) files() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

// safe refuses a path that leaves the checkout.
//
// The SourceView is already rooted at the checkout, so this is the second lock
// on a door that is bolted — and it stays, because "the other layer handles it"
// is how both layers end up not handling it.
func safe(name string) (string, error) {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return "", errors.New("no path was given")
	}
	clean = strings.TrimPrefix(clean, "./")
	if strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("%s is an absolute path; paths are relative to the repository root", name)
	}
	if clean != "." {
		clean = path.Clean(clean)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%s points outside the repository", name)
	}
	return clean, nil
}
