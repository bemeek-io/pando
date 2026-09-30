package egress

import (
	"fmt"
	"net/netip"
)

// Mode is how one layer of rules reads its list.
type Mode string

const (
	// Allowlist lets out only what the list names.
	Allowlist Mode = "allowlist"
	// Denylist lets out everything except what the list names.
	Denylist Mode = "denylist"
)

// Layer is one list and how to read it.
type Layer struct {
	Mode Mode     `json:"mode"`
	List []string `json:"list"`
	// From says whose layer this is: "install" or "app". Shown in a refusal,
	// so an app owner reading "refused by the installation's allowlist" knows
	// the fix is not in their own settings.
	From string `json:"from,omitempty"`
}

// Rules is everything that decides whether an app's connection may leave.
//
// Every layer must allow a destination (R-182): the install's rules, with the
// app's additions and removals applied, and then the app's own list on top.
// A destination one layer refuses is refused. No layers and no private-range
// blocking is no restriction at all, and a runtime adapter puts nothing in the
// app's path (R-186).
type Rules struct {
	Layers       []Layer `json:"layers,omitempty"`
	BlockPrivate bool    `json:"block_private,omitempty"`
}

// Restricted reports whether these rules refuse anything.
func (r Rules) Restricted() bool {
	if r.BlockPrivate {
		return true
	}
	for _, l := range r.Layers {
		// A denylist with nothing on it refuses nothing, and treating it as a
		// restriction would put a gateway in front of an app for no reason.
		if l.Mode == Denylist && len(l.List) == 0 {
			continue
		}
		return true
	}
	return false
}

// Validate checks every entry parses and every mode is known.
func (r Rules) Validate() error {
	for _, l := range r.Layers {
		if l.Mode != Allowlist && l.Mode != Denylist {
			return fmt.Errorf("egress mode %q is not allowlist or denylist", l.Mode)
		}
		if _, problems := ParseAll(l.List); len(problems) > 0 {
			return problems[0]
		}
	}
	return nil
}

// Compiled is Rules with every entry parsed, ready to decide connections.
type Compiled struct {
	layers       []compiledLayer
	blockPrivate bool
}

type compiledLayer struct {
	mode    Mode
	from    string
	entries []Entry
}

// Compile parses every entry once. An entry that does not parse is an error
// rather than skipped: a gateway that silently dropped a denylist entry would
// let out exactly what somebody tried to stop.
func (r Rules) Compile() (*Compiled, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	c := &Compiled{blockPrivate: r.BlockPrivate}
	for _, l := range r.Layers {
		entries, _ := ParseAll(l.List)
		c.layers = append(c.layers, compiledLayer{mode: l.Mode, from: l.From, entries: entries})
	}
	return c, nil
}

// Decision is whether one connection may leave, and why not.
type Decision struct {
	Allowed bool
	// Reason is written for the app's owner reading a refused connection in
	// the gateway's log.
	Reason string
}

// AllowsName reports whether a hostname could be let out on some address —
// the check made before resolving it. A denylist that names the host refuses
// it outright; an allowlist lets it through to be decided per address, since
// an address or range entry may still allow it.
func (c *Compiled) AllowsName(host string, port int) Decision {
	for _, l := range c.layers {
		if l.mode != Denylist {
			continue
		}
		for _, e := range l.entries {
			if e.MatchesName(host, port) {
				return Decision{Reason: fmt.Sprintf("%s is on the %s denylist (%s)", host, owner(l.from), e.Raw)}
			}
		}
	}
	return Decision{Allowed: true}
}

// Allows decides one connection: to host (as the app asked for it, which may
// be an address literal), on port, at the address it resolved to.
func (c *Compiled) Allows(host string, port int, addr netip.Addr) Decision {
	addr = addr.Unmap()
	if c.blockPrivate && Private(addr) {
		return Decision{Reason: fmt.Sprintf("%s resolves to %s, a private address, and private addresses are blocked", host, addr)}
	}
	for _, l := range c.layers {
		matched := ""
		for _, e := range l.entries {
			if e.MatchesName(host, port) || e.MatchesAddr(addr, port) {
				matched = e.Raw
				break
			}
		}
		switch l.mode {
		case Denylist:
			if matched != "" {
				return Decision{Reason: fmt.Sprintf("%s is on the %s denylist (%s)", host, owner(l.from), matched)}
			}
		case Allowlist:
			if matched == "" {
				return Decision{Reason: fmt.Sprintf("%s is not on the %s allowlist", host, owner(l.from))}
			}
		}
	}
	return Decision{Allowed: true}
}

func owner(from string) string {
	switch from {
	case "install":
		return "installation's"
	case "app":
		return "app's"
	default:
		return "egress"
	}
}
