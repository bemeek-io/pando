// Package aitest holds what the AI adapters' tests share: a repository in
// memory. Imported by tests only.
package aitest

import (
	"io"
	"path"
	"sort"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// Source is a repository in memory, keyed by path.
type Source map[string]string

var _ api.SourceView = Source{}

func (m Source) Open(name string) (io.ReadCloser, error) {
	content, ok := m[path.Clean(name)]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (m Source) Stat(name string) (api.FileInfo, error) {
	if content, ok := m[path.Clean(name)]; ok {
		return api.FileInfo{Name: path.Base(name), Size: int64(len(content))}, nil
	}
	return api.FileInfo{}, io.EOF
}

func (m Source) Glob(pattern string) ([]string, error) {
	var out []string
	for name := range m {
		if ok, _ := path.Match(pattern, name); ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Request is a repair request over a small Node app that binds loopback.
func Request() api.ScreenRequest {
	return api.ScreenRequest{
		Source: Source{
			"package.json": `{"scripts":{"start":"node server.js"}}`,
			"server.js":    "app.listen(3000, '127.0.0.1')",
		},
		Evidence: []string{"package.json declares a start script"},
		Trial:    api.TrialSummary{Ran: true, Crashed: true, Log: "listening on 127.0.0.1:3000"},
		Budget:   api.ScreenBudget{MaxFiles: 10, MaxBytes: 1 << 20},
	}
}
