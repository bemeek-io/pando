package egress

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func compile(t *testing.T, r Rules) *Compiled {
	t.Helper()
	c, err := r.Compile()
	require.NoError(t, err)
	return c
}

func addr(s string) netip.Addr { return netip.MustParseAddr(s) }

// TestR186_NoRulesIsNoRestriction asserts R-186: rules that refuse nothing
// are not a restriction, so a runtime puts nothing in the app's path.
func TestR186_NoRulesIsNoRestriction(t *testing.T) {
	t.Parallel()
	require.False(t, Rules{}.Restricted())
	require.False(t, Rules{Layers: []Layer{{Mode: Denylist}}}.Restricted(),
		"an empty denylist refuses nothing")
	require.False(t, Rules{Layers: []Layer{{Mode: Denylist, From: "install"}, {Mode: Denylist, List: []string{}, From: "app"}}}.Restricted())

	require.True(t, Rules{BlockPrivate: true}.Restricted())
	require.True(t, Rules{Layers: []Layer{{Mode: Denylist, List: []string{"evil.example"}}}}.Restricted())
	require.True(t, Rules{Layers: []Layer{{Mode: Allowlist}}}.Restricted(), "an empty allowlist refuses everything")
	require.True(t, Rules{Layers: []Layer{{Mode: Allowlist, List: []string{"*"}}}}.Restricted())

	// And with no rules, everything is let out.
	c := compile(t, Rules{})
	require.True(t, c.AllowsName("anything.example", 443).Allowed)
	require.True(t, c.Allows("anything.example", 443, addr("10.0.0.1")).Allowed)
}

func TestCompileRefusesWhatItCannotEnforce(t *testing.T) {
	t.Parallel()
	_, err := Rules{Layers: []Layer{{Mode: "maybe", List: []string{"a.example"}}}}.Compile()
	require.ErrorContains(t, err, "maybe")

	// A denylist entry that does not parse is not dropped: the gateway would
	// let out exactly what it was written to stop.
	_, err = Rules{Layers: []Layer{{Mode: Denylist, List: []string{"ok.example", "not valid"}}}}.Compile()
	require.ErrorContains(t, err, "not valid")
}

// TestR185_AnAllowlistLetsOutOnlyWhatItNames asserts R-185 and R-181.
func TestR185_AnAllowlistLetsOutOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	c := compile(t, Rules{Layers: []Layer{{
		Mode: Allowlist, From: "install",
		List: []string{"api.example.com:443", "*.cdn.example", "203.0.113.0/24"},
	}}})

	require.True(t, c.Allows("api.example.com", 443, addr("198.51.100.1")).Allowed,
		"a hostname entry matches the name, wherever it resolved")
	d := c.Allows("api.example.com", 80, addr("198.51.100.1"))
	require.False(t, d.Allowed, "the entry names port 443")
	require.Equal(t, "api.example.com on port 80 is not on the installation's allowlist", d.Reason)

	require.True(t, c.Allows("img.cdn.example", 443, addr("198.51.100.2")).Allowed)
	require.False(t, c.Allows("cdn.example", 443, addr("198.51.100.2")).Allowed, "a wildcard is not its apex")

	require.True(t, c.Allows("anything.example", 22, addr("203.0.113.9")).Allowed,
		"a range entry matches where an unlisted name resolved")
	require.True(t, c.Allows("203.0.113.9", 22, addr("203.0.113.9")).Allowed, "and an address asked for directly")
	require.False(t, c.Allows("anything.example", 22, addr("198.51.100.9")).Allowed)
}

// TestR185_AnAllowlistRefusesANameBeforeResolvingIt: a name nothing on an
// allowlist could match is refused before it is looked up, so a refused name
// never becomes a DNS query leaving the gateway.
func TestR185_AnAllowlistRefusesANameBeforeResolvingIt(t *testing.T) {
	t.Parallel()
	names := compile(t, Rules{Layers: []Layer{{Mode: Allowlist, From: "app", List: []string{"api.example.com", "*.github.com:443"}}}})
	require.True(t, names.AllowsName("api.example.com", 443).Allowed)
	require.True(t, names.AllowsName("codeload.github.com", 443).Allowed)
	require.False(t, names.AllowsName("codeload.github.com", 22).Allowed, "the wildcard names port 443")
	d := names.AllowsName("secret-data.attacker.example", 443)
	require.False(t, d.Allowed)
	require.Contains(t, d.Reason, "app's allowlist")

	// With a range on the list, any name might resolve into it, so the name
	// is let through to be decided per address.
	ranges := compile(t, Rules{Layers: []Layer{{Mode: Allowlist, List: []string{"api.example.com", "10.0.0.0/8:5432"}}}})
	require.True(t, ranges.AllowsName("db.internal", 5432).Allowed)
	require.False(t, ranges.AllowsName("db.internal", 80).Allowed, "the range names port 5432 only")
}

// TestR181_ADenylistLetsOutEverythingElse asserts R-181.
func TestR181_ADenylistLetsOutEverythingElse(t *testing.T) {
	t.Parallel()
	c := compile(t, Rules{Layers: []Layer{{
		Mode: Denylist, From: "install",
		List: []string{"tracker.example", "*.ads.example", "198.51.100.0/24", "smtp.example:25"},
	}}})

	d := c.AllowsName("tracker.example", 443)
	require.False(t, d.Allowed, "a denylisted name is refused before it is resolved")
	require.Equal(t, "tracker.example is on the installation's denylist (tracker.example)", d.Reason)
	require.False(t, c.Allows("tracker.example", 443, addr("203.0.113.1")).Allowed)
	require.False(t, c.AllowsName("x.ads.example", 80).Allowed)
	require.True(t, c.AllowsName("ads.example", 80).Allowed, "a wildcard is not its apex")

	require.True(t, c.AllowsName("innocent.example", 443).Allowed, "a range cannot be judged by name")
	d = c.Allows("innocent.example", 443, addr("198.51.100.7"))
	require.False(t, d.Allowed, "a name that resolves into a denied range is refused")
	require.Contains(t, d.Reason, "198.51.100.0/24")

	require.False(t, c.Allows("smtp.example", 25, addr("203.0.113.1")).Allowed)
	require.True(t, c.Allows("smtp.example", 587, addr("203.0.113.1")).Allowed, "the entry names port 25 only")

	require.True(t, c.Allows("example.org", 443, addr("203.0.113.1")).Allowed)
	require.True(t, c.Allows("10.0.0.5", 443, addr("10.0.0.5")).Allowed,
		"a denylist does not block private addresses on its own")
}

// TestR182_EveryLayerMustAllow asserts R-182: an app's own list is applied on
// top of the install's, and a destination is reachable only if both allow it.
func TestR182_EveryLayerMustAllow(t *testing.T) {
	t.Parallel()
	c := compile(t, Rules{Layers: []Layer{
		{Mode: Denylist, From: "install", List: []string{"blocked.example"}},
		{Mode: Allowlist, From: "app", List: []string{"api.stripe.com", "blocked.example"}},
	}})

	require.True(t, c.Allows("api.stripe.com", 443, addr("203.0.113.1")).Allowed)

	d := c.Allows("blocked.example", 443, addr("203.0.113.2"))
	require.False(t, d.Allowed, "the app's allowlist cannot reopen what the install denies")
	require.Contains(t, d.Reason, "installation's denylist")

	d = c.Allows("example.org", 443, addr("203.0.113.3"))
	require.False(t, d.Allowed, "the install allows it, the app's own list does not")
	require.Contains(t, d.Reason, "app's allowlist")

	both := compile(t, Rules{Layers: []Layer{
		{Mode: Allowlist, From: "install", List: []string{"*.example.com"}},
		{Mode: Denylist, From: "app", List: []string{"admin.example.com"}},
	}})
	require.True(t, both.Allows("api.example.com", 443, addr("203.0.113.1")).Allowed)
	require.False(t, both.Allows("admin.example.com", 443, addr("203.0.113.1")).Allowed)
	require.False(t, both.Allows("example.org", 443, addr("203.0.113.1")).Allowed)
	require.False(t, both.AllowsName("admin.example.com", 443).Allowed)
	require.False(t, both.AllowsName("example.org", 443).Allowed)
}

// TestR185_BlockingPrivateRangesIsCheckedWhereANameResolved asserts R-185: a
// public name that resolves to a private address is refused too, and the
// cloud metadata address is private.
func TestR185_BlockingPrivateRangesIsCheckedWhereANameResolved(t *testing.T) {
	t.Parallel()
	c := compile(t, Rules{BlockPrivate: true})

	require.True(t, c.AllowsName("rebind.example", 80).Allowed, "the name alone says nothing")
	d := c.Allows("rebind.example", 80, addr("169.254.169.254"))
	require.False(t, d.Allowed)
	require.Equal(t, "rebind.example resolves to 169.254.169.254, a private address, and private addresses are blocked", d.Reason)

	d = c.Allows("10.0.0.1", 80, addr("10.0.0.1"))
	require.False(t, d.Allowed)
	require.Equal(t, "10.0.0.1 is a private address, and private addresses are blocked", d.Reason)

	require.False(t, c.Allows("x.example", 80, addr("::ffff:192.168.0.1")).Allowed)
	require.False(t, c.Allows("x.example", 80, addr("100.64.1.1")).Allowed)
	require.True(t, c.Allows("x.example", 80, addr("203.0.113.1")).Allowed)

	// Even when an allowlist names the address: the switch is independent of
	// the mode, and the list cannot reopen what it closes.
	listed := compile(t, Rules{BlockPrivate: true, Layers: []Layer{{Mode: Allowlist, List: []string{"10.0.0.0/8", "db.internal"}}}})
	require.False(t, listed.Allows("db.internal", 5432, addr("10.0.0.5")).Allowed)
	require.False(t, listed.Allows("10.0.0.5", 5432, addr("10.0.0.5")).Allowed)
}
