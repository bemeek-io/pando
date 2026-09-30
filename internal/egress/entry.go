// Package egress is what an egress rule means: how an entry is written, what
// it matches, and whether a set of rules lets a connection out (R-181 – R-187).
//
// A leaf package. Core resolves an app's rules against host policy
// (policy.Document.EgressFor), a runtime adapter carries the result to where it
// is enforced, and the gateway (Gateway) enforces it. All three read the same
// definitions from here, so "does this entry match" cannot mean one thing when
// an app's plan is shown and another when a connection is refused.
package egress

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Entry is one destination in an egress list (R-185).
//
// Exactly one of Host, Wildcard, Prefix or Any is set. A hostname entry
// matches the name an app asked for; an address or range entry matches where
// that name resolved.
type Entry struct {
	// Raw is the entry as written, which is how it is shown back.
	Raw string

	// Any is "*": every destination.
	Any bool

	// Host is an exact hostname, lower case, without a trailing dot.
	Host string

	// Wildcard is the suffix of "*.example.com" — "example.com" — and matches
	// any name strictly below it. Not the apex: "*.example.com" does not
	// match "example.com", which is how every certificate and every DNS
	// operator already reads a wildcard.
	Wildcard string

	// Prefix is an address or range. A bare address is a /32 or /128.
	Prefix netip.Prefix

	// Port restricts the entry to one port. Zero is every port.
	Port int
}

// Parse reads one entry.
//
// The error is written for somebody who typed the entry, because they are who
// will see it (R-105).
func Parse(raw string) (Entry, error) {
	s := strings.TrimSpace(raw)
	e := Entry{Raw: s}
	if s == "" {
		return e, fmt.Errorf("an egress entry is empty. Write a hostname such as api.example.com, a wildcard such as *.example.com, an address, or a range such as 10.0.0.0/8")
	}
	if s == "*" {
		e.Any = true
		return e, nil
	}

	host, port, err := splitPort(s)
	if err != nil {
		return e, err
	}
	e.Port = port

	// An address or a range.
	if strings.Contains(host, "/") {
		p, err := netip.ParsePrefix(host)
		if err != nil {
			return e, fmt.Errorf("%q is not a valid address range. Write it as an address and a prefix length, such as 10.0.0.0/8 or 2001:db8::/32", s)
		}
		e.Prefix = p.Masked()
		return e, nil
	}
	if a, err := netip.ParseAddr(host); err == nil {
		e.Prefix = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		return e, nil
	}

	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if strings.HasPrefix(host, "*.") {
		suffix := host[2:]
		if !validHostname(suffix) {
			return e, fmt.Errorf("%q is not a valid wildcard. A wildcard is *. followed by a domain, such as *.example.com", s)
		}
		e.Wildcard = suffix
		return e, nil
	}
	if !validHostname(host) {
		return e, fmt.Errorf("%q is not a hostname, an address or a range. Write a hostname such as api.example.com, a wildcard such as *.example.com, an address such as 203.0.113.7, or a range such as 10.0.0.0/8", s)
	}
	e.Host = host
	return e, nil
}

// ParseAll reads a list, reporting every entry that does not parse at once:
// somebody fixing a list should not learn about the second mistake only after
// fixing the first.
func ParseAll(raw []string) ([]Entry, []error) {
	out := make([]Entry, 0, len(raw))
	var problems []error
	for _, r := range raw {
		e, err := Parse(r)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		out = append(out, e)
	}
	return out, problems
}

// splitPort separates an optional port. IPv6 with a port is bracketed, as in
// a URL: [2001:db8::1]:443. A bare IPv6 address has colons and no port.
func splitPort(s string) (string, int, error) {
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return "", 0, fmt.Errorf("%q opens a bracket and never closes it. Write an IPv6 address with a port as [2001:db8::1]:443", s)
		}
		host, rest := s[1:end], s[end+1:]
		if rest == "" {
			return host, 0, nil
		}
		if !strings.HasPrefix(rest, ":") {
			return "", 0, fmt.Errorf("%q has something after the closing bracket that is not a port", s)
		}
		port, err := parsePort(s, rest[1:])
		return host, port, err
	}
	if strings.Count(s, ":") == 1 {
		i := strings.LastIndex(s, ":")
		port, err := parsePort(s, s[i+1:])
		return s[:i], port, err
	}
	return s, 0, nil
}

func parsePort(entry, p string) (int, error) {
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%q has a port that is not a number from 1 to 65535", entry)
	}
	return n, nil
}

func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i, r := range label {
			ok := r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
			if !ok || ((i == 0 || i == len(label)-1) && r == '-') {
				return false
			}
		}
	}
	return true
}

// MatchesName reports whether the entry matches a hostname the app asked for.
// Address and range entries never match a name: they match where it resolved.
func (e Entry) MatchesName(host string, port int) bool {
	if !e.portMatches(port) {
		return false
	}
	if e.Any {
		return true
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	switch {
	case e.Host != "":
		return host == e.Host
	case e.Wildcard != "":
		return strings.HasSuffix(host, "."+e.Wildcard)
	}
	return false
}

// MatchesAddr reports whether the entry matches an address a connection is
// going to. Hostname entries never match an address.
func (e Entry) MatchesAddr(a netip.Addr, port int) bool {
	if !e.portMatches(port) {
		return false
	}
	if e.Any {
		return true
	}
	return e.Prefix.IsValid() && e.Prefix.Contains(a.Unmap())
}

func (e Entry) portMatches(port int) bool { return e.Port == 0 || e.Port == port }

// Same reports whether two entries name the same destination, however each
// was written. Used to apply an app's removals to the install's list:
// removing "API.example.com." removes "api.example.com".
func (e Entry) Same(o Entry) bool {
	return e.Any == o.Any && e.Host == o.Host && e.Wildcard == o.Wildcard &&
		e.Prefix == o.Prefix && e.Port == o.Port
}

// Key is a canonical spelling, stable across ways of writing the same entry.
func (e Entry) Key() string {
	var s string
	switch {
	case e.Any:
		s = "*"
	case e.Host != "":
		s = e.Host
	case e.Wildcard != "":
		s = "*." + e.Wildcard
	case e.Prefix.IsValid():
		s = e.Prefix.String()
	}
	if e.Port != 0 {
		if e.Prefix.IsValid() && e.Prefix.Addr().Is6() {
			s = "[" + s + "]"
		}
		s += ":" + strconv.Itoa(e.Port)
	}
	return s
}

// Private reports whether an address is one R-181's private-range switch
// refuses: RFC 1918, loopback, link-local, unique-local, carrier-grade NAT and
// unspecified. Link-local covers 169.254.169.254, the cloud metadata address,
// which is the destination this switch most often exists to protect.
func Private(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() || a.IsUnspecified() || a.IsInterfaceLocalMulticast() {
		return true
	}
	return cgnat.Contains(a)
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")
