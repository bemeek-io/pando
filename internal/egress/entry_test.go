package egress

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR185_AnEntryNamesADestination asserts R-185: an entry is a hostname, a
// wildcard of its subdomains, an address or a range, each optionally with a
// port, or "*" for everywhere.
func TestR185_AnEntryNamesADestination(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want Entry
		key  string
	}{
		{"*", Entry{Any: true}, "*"},
		{"api.example.com", Entry{Host: "api.example.com"}, "api.example.com"},
		{"  API.Example.COM. ", Entry{Host: "api.example.com"}, "api.example.com"},
		{"api.example.com:443", Entry{Host: "api.example.com", Port: 443}, "api.example.com:443"},
		{"*.example.com", Entry{Wildcard: "example.com"}, "*.example.com"},
		{"*.example.com:8443", Entry{Wildcard: "example.com", Port: 8443}, "*.example.com:8443"},
		{"localhost", Entry{Host: "localhost"}, "localhost"},
		{"my_service.internal", Entry{Host: "my_service.internal"}, "my_service.internal"},
		{"203.0.113.7", Entry{Prefix: netip.MustParsePrefix("203.0.113.7/32")}, "203.0.113.7/32"},
		{"203.0.113.7:443", Entry{Prefix: netip.MustParsePrefix("203.0.113.7/32"), Port: 443}, "203.0.113.7/32:443"},
		{"10.1.2.3/8", Entry{Prefix: netip.MustParsePrefix("10.0.0.0/8")}, "10.0.0.0/8"},
		{"10.0.0.0/8:5432", Entry{Prefix: netip.MustParsePrefix("10.0.0.0/8"), Port: 5432}, "10.0.0.0/8:5432"},
		{"2001:db8::1", Entry{Prefix: netip.MustParsePrefix("2001:db8::1/128")}, "2001:db8::1/128"},
		{"[2001:db8::1]", Entry{Prefix: netip.MustParsePrefix("2001:db8::1/128")}, "2001:db8::1/128"},
		{"[2001:db8::1]:443", Entry{Prefix: netip.MustParsePrefix("2001:db8::1/128"), Port: 443}, "[2001:db8::1/128]:443"},
		{"2001:db8::/32", Entry{Prefix: netip.MustParsePrefix("2001:db8::/32")}, "2001:db8::/32"},
		{"[2001:db8::/32]:443", Entry{Prefix: netip.MustParsePrefix("2001:db8::/32"), Port: 443}, "[2001:db8::/32]:443"},
		{"::ffff:203.0.113.7", Entry{Prefix: netip.MustParsePrefix("203.0.113.7/32")}, "203.0.113.7/32"},
		{"::ffff:10.0.0.0/104", Entry{Prefix: netip.MustParsePrefix("10.0.0.0/8")}, "10.0.0.0/8"},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got, err := Parse(c.raw)
			require.NoError(t, err)
			got.Raw = ""
			require.Equal(t, c.want, got)
			require.Equal(t, c.key, got.Key())

			// The canonical spelling reads back as the same destination.
			again, err := Parse(got.Key())
			require.NoError(t, err)
			require.True(t, again.Same(got), "%q re-parsed from %q", got.Key(), c.raw)
		})
	}
}

// TestR185_AnEntryThatDoesNotParseSaysWhy asserts R-185 and R-105: a mistyped
// entry is refused with a message that says what to write instead.
func TestR185_AnEntryThatDoesNotParseSaysWhy(t *testing.T) {
	t.Parallel()
	for raw, says := range map[string]string{
		"":                      "empty",
		"   ":                   "empty",
		"api.example.com:0":     "port",
		"api.example.com:http":  "port",
		"api.example.com:":      "port",
		"api.example.com:70000": "port",
		"[2001:db8::1":          "bracket",
		"[2001:db8::1]x":        "not a port",
		"[2001:db8::1]:99999":   "port",
		"10.0.0.0/33":           "range",
		"10.0.0/8":              "range",
		"*.":                    "wildcard",
		"*.-bad.com":            "wildcard",
		"*.*.example.com":       "wildcard",
		"exa mple.com":          "hostname",
		"-bad.example.com":      "hostname",
		"bad-.example.com":      "hostname",
		"a..b":                  "hostname",
		"http://example.com":    "hostname",
		"example.com/path":      "range",
		"fe80::1%eth0":          "interface",
	} {
		_, err := Parse(raw)
		require.Error(t, err, "%q", raw)
		require.Contains(t, err.Error(), says, "%q", raw)
	}
}

func TestParseAllReportsEveryProblem(t *testing.T) {
	t.Parallel()
	entries, problems := ParseAll([]string{"a.example.com", "bad entry", "10.0.0.0/8", "also bad:x"})
	require.Len(t, entries, 2)
	require.Len(t, problems, 2)
}

// TestR185_AWildcardDoesNotMatchItsApex asserts R-185: *.example.com names the
// subdomains of example.com, not example.com itself, as every certificate and
// DNS operator reads a wildcard.
func TestR185_AWildcardDoesNotMatchItsApex(t *testing.T) {
	t.Parallel()
	e, err := Parse("*.example.com")
	require.NoError(t, err)

	require.True(t, e.MatchesName("api.example.com", 443))
	require.True(t, e.MatchesName("a.b.example.com", 443), "any depth below")
	require.True(t, e.MatchesName("API.EXAMPLE.COM.", 443), "case and a trailing dot do not matter")
	require.False(t, e.MatchesName("example.com", 443), "not the apex")
	require.False(t, e.MatchesName("badexample.com", 443), "not a name that merely ends the same way")
	require.False(t, e.MatchesName("example.com.evil.net", 443))
}

func TestR185_AHostnameEntryMatchesOnlyThatName(t *testing.T) {
	t.Parallel()
	e, err := Parse("api.example.com")
	require.NoError(t, err)
	require.True(t, e.MatchesName("api.example.com", 80))
	require.True(t, e.MatchesName("Api.Example.Com.", 443))
	require.False(t, e.MatchesName("x.api.example.com", 443), "a host entry is not a wildcard")
	require.False(t, e.MatchesName("example.com", 443))
	require.False(t, e.MatchesAddr(netip.MustParseAddr("203.0.113.7"), 443),
		"a hostname entry never matches an address")
}

// TestR185_APortNarrowsAnEntry asserts R-185: an entry with a port matches
// only that port, and one without matches every port.
func TestR185_APortNarrowsAnEntry(t *testing.T) {
	t.Parallel()
	withPort, _ := Parse("api.example.com:443")
	require.True(t, withPort.MatchesName("api.example.com", 443))
	require.False(t, withPort.MatchesName("api.example.com", 80))

	anyPort, _ := Parse("api.example.com")
	require.True(t, anyPort.MatchesName("api.example.com", 80))
	require.True(t, anyPort.MatchesName("api.example.com", 8080))

	v6, _ := Parse("[2001:db8::1]:443")
	require.True(t, v6.MatchesAddr(netip.MustParseAddr("2001:db8::1"), 443))
	require.False(t, v6.MatchesAddr(netip.MustParseAddr("2001:db8::1"), 22))

	everywhere443, _ := Parse("*")
	require.True(t, everywhere443.MatchesName("anything.example", 1))
	require.True(t, everywhere443.MatchesAddr(netip.MustParseAddr("192.0.2.1"), 1))
}

// TestR185_AnAddressOrRangeMatchesWhereANameResolved asserts R-185.
func TestR185_AnAddressOrRangeMatchesWhereANameResolved(t *testing.T) {
	t.Parallel()
	r, _ := Parse("10.0.0.0/8")
	require.True(t, r.MatchesAddr(netip.MustParseAddr("10.20.30.40"), 5432))
	require.True(t, r.MatchesAddr(netip.MustParseAddr("::ffff:10.20.30.40"), 5432),
		"an IPv4 address written as IPv6 is the same address")
	require.False(t, r.MatchesAddr(netip.MustParseAddr("11.0.0.1"), 5432))
	require.False(t, r.MatchesName("10.20.30.40", 5432), "a range never matches a name")

	single, _ := Parse("203.0.113.7")
	require.True(t, single.MatchesAddr(netip.MustParseAddr("203.0.113.7"), 1))
	require.False(t, single.MatchesAddr(netip.MustParseAddr("203.0.113.8"), 1))

	v6, _ := Parse("2001:db8::/32")
	require.True(t, v6.MatchesAddr(netip.MustParseAddr("2001:db8:ffff::1"), 443))
	require.False(t, v6.MatchesAddr(netip.MustParseAddr("2001:db9::1"), 443))
	require.False(t, v6.MatchesAddr(netip.MustParseAddr("10.0.0.1"), 443))
}

func TestSameIgnoresHowAnEntryWasWritten(t *testing.T) {
	t.Parallel()
	a, _ := Parse("API.example.com.")
	b, _ := Parse("api.example.com")
	c, _ := Parse("api.example.com:443")
	require.True(t, a.Same(b))
	require.False(t, b.Same(c), "a port is part of the destination")

	r1, _ := Parse("10.9.9.9/8")
	r2, _ := Parse("10.0.0.0/8")
	require.True(t, r1.Same(r2))
}

// TestR181_PrivateCoversTheRangesBlockingExistsFor asserts R-181 and R-185:
// the private-range switch covers RFC 1918, loopback, link-local (and so the
// cloud metadata address), unique-local, carrier-grade NAT and unspecified —
// however the address is spelled.
func TestR181_PrivateCoversTheRangesBlockingExistsFor(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"10.0.0.1", "172.16.0.1", "172.31.255.254", "192.168.1.1",
		"127.0.0.1", "127.8.8.8", "::1",
		"169.254.169.254", "169.254.0.1", "fe80::1",
		"fc00::1", "fd12:3456::1",
		"100.64.0.1", "100.127.255.254",
		"0.0.0.0", "0.1.2.3", "::",
		"::ffff:10.0.0.1", "::ffff:169.254.169.254", "::ffff:127.0.0.1",
		"64:ff9b::a00:1", "64:ff9b::a9fe:a9fe",
		"fec0::1", "255.255.255.255",
	} {
		require.True(t, Private(netip.MustParseAddr(s)), s)
	}
	for _, s := range []string{
		"8.8.8.8", "1.1.1.1", "203.0.113.7", "172.32.0.1", "100.128.0.1", "100.63.255.255",
		"2001:4860:4860::8888", "64:ff9b::808:808", "::ffff:8.8.8.8",
	} {
		require.False(t, Private(netip.MustParseAddr(s)), s)
	}
}
