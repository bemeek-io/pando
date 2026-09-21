package docker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// A carried file's digest is what makes an edited one a container that no
// longer matches its plan. The content is copied in after create and leaves no
// trace in the container's own configuration, so without this a spec whose only
// change was a Caddyfile would converge to "already running".
func TestACarriedFileChangesTheWorkloadsDigest(t *testing.T) {
	one := []api.FilePlan{{Path: "/etc/caddy/Caddyfile", Content: ":80 { respond \"ok\" }"}}
	edited := []api.FilePlan{{Path: "/etc/caddy/Caddyfile", Content: ":80 { respond \"no\" }"}}

	require.NotEmpty(t, fileDigest(one))
	require.NotEqual(t, fileDigest(one), fileDigest(edited))

	// A file made executable is a different container from the same file that
	// is not.
	mode := []api.FilePlan{{Path: "/etc/caddy/Caddyfile", Content: ":80 { respond \"ok\" }", Mode: 0o755}}
	require.NotEqual(t, fileDigest(one), fileDigest(mode))
}

// Order is not a change. The plan's files come from a spec, and a spec that
// listed them the other way round is the same app.
func TestTheDigestDoesNotDependOnOrder(t *testing.T) {
	a := []api.FilePlan{
		{Path: "/etc/one.conf", Content: "one"},
		{Path: "/etc/two.conf", Content: "two"},
	}
	b := []api.FilePlan{
		{Path: "/etc/two.conf", Content: "two"},
		{Path: "/etc/one.conf", Content: "one"},
	}
	require.Equal(t, fileDigest(a), fileDigest(b))
}

// No files is the empty digest, which is what every container created before
// this existed carries. A non-empty digest there would recreate every workload
// on the host once, for nothing.
func TestNoFilesIsNoDigest(t *testing.T) {
	require.Empty(t, fileDigest(nil))
	require.Empty(t, fileDigest([]api.FilePlan{}))
}

// The directories a file needs, outermost first, because a tar stream creates
// them in the order it carries them. Docker creates none of them itself.
func TestTheDirectoriesAboveAFileAreCreatedOutermostFirst(t *testing.T) {
	require.Equal(t, []string{"/etc", "/etc/caddy"}, ancestors("/etc/caddy"))
	require.Equal(t, []string{"/usr", "/usr/local", "/usr/local/bin"}, ancestors("/usr/local/bin"))

	// Nothing above a file at the root.
	require.Empty(t, ancestors("/"))
	require.Empty(t, ancestors("."))
	require.Empty(t, ancestors(""))
}
