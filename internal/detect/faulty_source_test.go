package detect_test

import (
	"errors"
	"io"
	"path"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// faultySource is a memSource that fails where a test says it should.
//
// A real checkout can refuse to open a file it just listed (a permission, a
// file removed underneath the read) or list something the reader did not ask
// for, and detection has to carry on past each of those rather than stop. The
// in-memory source never does any of that on its own.
type faultySource struct {
	memSource
	openFails map[string]bool     // Open refuses these paths
	statFails map[string]bool     // Stat refuses these paths
	globFails map[string]bool     // Glob refuses these patterns
	globExtra map[string][]string // Glob also returns these for a pattern
}

var errFaulty = errors.New("refused by the test source")

func (f faultySource) Open(name string) (io.ReadCloser, error) {
	if f.openFails[path.Clean(name)] {
		return nil, errFaulty
	}
	return f.memSource.Open(name)
}

func (f faultySource) Stat(name string) (api.FileInfo, error) {
	if f.statFails[path.Clean(name)] {
		return api.FileInfo{}, errFaulty
	}
	return f.memSource.Stat(name)
}

func (f faultySource) Glob(pattern string) ([]string, error) {
	if f.globFails[pattern] {
		return nil, errFaulty
	}
	out, err := f.memSource.Glob(pattern)
	return append(out, f.globExtra[pattern]...), err
}
