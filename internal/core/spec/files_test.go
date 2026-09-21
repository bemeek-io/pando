package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// TestR099a_ACarriedFileNeedsSomewhereToLand asserts R-099a.
//
// A file travels in the spec and is placed in the workload at every start, so
// the path is the whole of where it goes. A relative one has no meaning inside
// a container, and a directory is not a file.
func TestR099a_ACarriedFileNeedsSomewhereToLand(t *testing.T) {
	for name, path := range map[string]string{
		"relative":    "etc/caddy/Caddyfile",
		"a directory": "/etc/caddy/",
	} {
		t.Run(name, func(t *testing.T) {
			s := valid()
			s.Workloads[0].Files = []spec.File{{Path: path, Content: "x"}}

			err := spec.Validate(s)
			require.Error(t, err)
			require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
		})
	}

	ok := valid()
	ok.Workloads[0].Files = []spec.File{{Path: "/etc/caddy/Caddyfile", Content: ":80 { }"}}
	require.NoError(t, spec.Validate(ok))
}

// Two files for one path is two answers to one question, and which one the
// runtime ends up with is not defined.
func TestTwoFilesForOnePathIsRefused(t *testing.T) {
	s := valid()
	s.Workloads[0].Files = []spec.File{
		{Path: "/etc/app.conf", Content: "a"},
		{Path: "/etc/app.conf", Content: "b"},
	}

	err := spec.Validate(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "two files")
}

// Storage and a carried file at the same path is two mechanisms for one
// location: one is data the app writes, the other is configuration Pando
// places, and the runtime decides which wins.
func TestAMountAndAFileAtOnePathIsRefused(t *testing.T) {
	s := valid()
	s.Volumes = []spec.Volume{{ID: "data", Name: "data"}}
	s.Workloads[0].Mounts = []spec.Mount{{VolumeID: "data", Path: "/etc/app.conf"}}
	s.Workloads[0].Files = []spec.File{{Path: "/etc/app.conf", Content: "a"}}

	err := spec.Validate(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "both mounts storage and carries a file")
}

// The cap is what keeps a spec something a person reads and a database row
// holds. A file past it is a build input, and the message says so.
func TestAFileTooBigToCarryIsRefused(t *testing.T) {
	s := valid()
	s.Workloads[0].Files = []spec.File{{
		Path:    "/etc/app.conf",
		Content: strings.Repeat("x", spec.FileSizeLimit+1),
	}}

	err := spec.Validate(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "KB")

	// And one at the cap is fine: the limit is a limit, not a margin.
	atLimit := valid()
	atLimit.Workloads[0].Files = []spec.File{{
		Path:    "/etc/app.conf",
		Content: strings.Repeat("x", spec.FileSizeLimit),
	}}
	require.NoError(t, spec.Validate(atLimit))
}
