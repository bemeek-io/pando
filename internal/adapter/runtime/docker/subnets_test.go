package docker

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func blocks(pool string, used []string, n int) []string {
	var parsed []netip.Prefix
	for _, u := range used {
		parsed = append(parsed, netip.MustParsePrefix(u))
	}
	var out []string
	for b := range freeBlocks(netip.MustParsePrefix(pool), parsed) {
		out = append(out, b.String())
		if len(out) == n {
			break
		}
	}
	return out
}

// TestR025_AppNetworksAreSmallAndSkipWhatIsTaken asserts R-025.
//
// Docker's default pool held about thirty networks and Pando took one per app,
// so a busy host ran out (issue #55). Each app now takes a /26 from Pando's own
// range, and never one that overlaps a network that already exists.
func TestR025_AppNetworksAreSmallAndSkipWhatIsTaken(t *testing.T) {
	require.Equal(t, []string{"10.213.0.0/26", "10.213.0.64/26", "10.213.0.128/26"},
		blocks("10.213.0.0/16", nil, 3))

	require.Equal(t, []string{"10.213.1.0/26"},
		blocks("10.213.0.0/16", []string{"10.213.0.0/24", "172.17.0.0/16"}, 1),
		"a block anything already holds is skipped")

	require.Len(t, blocks("10.213.0.0/16", nil, 5000), 1024, "a /16 holds 1,024 app networks")
}

// Another install's app networks are not this one's to join or reclaim. Two
// installs on one Docker host each rejoined the other's apps and removed the
// other's empty networks at startup (issue #55).
func TestR025_AnotherInstallsNetworksAreLeftAlone(t *testing.T) {
	mine := func(bundle string) bool { return bundle == "app_mine" }
	require.True(t, ownedBundle(mine, "app_mine"))
	require.False(t, ownedBundle(mine, "app_theirs"))
	require.False(t, ownedBundle(mine, ""), "a trial's network removes itself")
	require.True(t, ownedBundle(nil, "app_any"), "no predicate owns every bundle, as before")
}

func TestTheNetworkPoolCanBeTurnedOffOrMoved(t *testing.T) {
	a := &Adapter{}
	pool, ok := a.networkPool()
	require.True(t, ok)
	require.Equal(t, defaultNetworkPool, pool.String())

	a.config.NetworkPool = "off"
	_, ok = a.networkPool()
	require.False(t, ok)

	a.config.NetworkPool = "10.50.0.0/20"
	pool, ok = a.networkPool()
	require.True(t, ok)
	require.Equal(t, "10.50.0.0/20", pool.String())

	a.config.NetworkPool = "not a range"
	_, ok = a.networkPool()
	require.False(t, ok, "a range that cannot be read falls back to Docker's")
}
