package policy

import (
	"fmt"

	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/egress"
)

// EgressLoosening is what an app needs to loosen the install's egress rules
// (R-183).
type EgressLoosening string

const (
	// EgressLooseningVerb: somebody holding app.egress.loosen may. The
	// default, because Pando ships permissive (R-270) and this is what an
	// install had before the setting existed.
	EgressLooseningVerb EgressLoosening = "verb"
	// EgressLooseningApproval: the deploy that carries it needs approval
	// (R-154). Anybody who may change the app's egress may propose one.
	EgressLooseningApproval EgressLoosening = "approval"
	// EgressLooseningForbidden: no app may, and a deploy that would is
	// refused at plan time.
	EgressLooseningForbidden EgressLoosening = "forbidden"
)

// Valid refuses a value that is none of the three. Empty is unset.
func (l EgressLoosening) Valid() error {
	switch l {
	case "", EgressLooseningVerb, EgressLooseningApproval, EgressLooseningForbidden:
		return nil
	}
	return fmt.Errorf("%q is not an egress_loosening setting; use verb, approval or forbidden", string(l))
}

// EgressLooseningRule is the rule in force, with the default applied.
func (d Document) EgressLooseningRule() EgressLoosening {
	if d.EgressLoosening == "" {
		return EgressLooseningVerb
	}
	return d.EgressLoosening
}

// EgressInstallMode is the install's egress mode in force, reading the field
// from before issue #79 when the new one is unset.
func (d Document) EgressInstallMode() (spec.EgressMode, []string) {
	switch d.EgressMode {
	case spec.EgressDenylist, spec.EgressAllowlist:
		return d.EgressMode, d.EgressList
	case "", spec.EgressAllowAll:
		if d.EgressMode == "" && len(d.EgressAllowlist) > 0 {
			return spec.EgressAllowlist, d.EgressAllowlist
		}
	}
	return spec.EgressAllowAll, nil
}

// InstallEgress is the installation's egress rules as an app's owner edits
// against them (R-182): the mode and list in force, whether private addresses
// are blocked, and what loosening them needs (R-183).
type InstallEgress struct {
	Mode         spec.EgressMode `json:"mode"`
	List         []string        `json:"list"`
	BlockPrivate bool            `json:"block_private"`
	Loosening    EgressLoosening `json:"loosening"`
}

// InstallEgress is the installation's egress rules in force, with the
// defaults and the field from before issue #79 read the way EgressFor reads
// them, so what an owner is shown is what their app is held to.
func (d Document) InstallEgress() InstallEgress {
	mode, list := d.EgressInstallMode()
	return InstallEgress{
		Mode:         mode,
		List:         nonNil(append([]string(nil), list...)),
		BlockPrivate: d.EgressBlockPrivate,
		Loosening:    d.EgressLooseningRule(),
	}
}

// ValidateEgress checks the install's egress settings can be read.
func (d Document) ValidateEgress() error {
	switch d.EgressMode {
	case "", spec.EgressAllowAll, spec.EgressDenylist, spec.EgressAllowlist:
	default:
		return fmt.Errorf("%q is not an egress_mode; use allow_all, denylist or allowlist", string(d.EgressMode))
	}
	if err := d.EgressLoosening.Valid(); err != nil {
		return err
	}
	for _, list := range [][]string{d.EgressList, d.EgressAllowlist} {
		if _, problems := egress.ParseAll(list); len(problems) > 0 {
			return fmt.Errorf("in the installation's egress list, %w", problems[0])
		}
	}
	return nil
}

// Loosening kinds: the three ways an app can loosen the install's rules
// (R-182). Nothing else an app can write loosens.
const (
	LoosenAllowlistAdd   = "allowlist_add"
	LoosenDenylistRemove = "denylist_remove"
	LoosenBlockPrivate   = "block_private_off"
)

// Loosening is one way an app's egress is looser than the install's.
type Loosening struct {
	Kind  string `json:"kind"`
	Entry string `json:"entry,omitempty"`
	// Message is written for the app's owner (R-105).
	Message string `json:"message"`
}

// EgressEntry is one entry of the merged list, and whose it is.
type EgressEntry struct {
	Entry string `json:"entry"`
	From  string `json:"from"` // install | app
}

// EffectiveEgress is the rules an app runs with, merged, with where each part
// came from (R-188). What the plan shows and what the runtime enforces are
// both read from here, so they cannot disagree.
type EffectiveEgress struct {
	// Mode and List are the install's rules with the app's additions and
	// removals applied.
	Mode spec.EgressMode `json:"mode"`
	List []EgressEntry   `json:"list"`

	// AppMode and AppList are the app's own list, applied on top. Empty when
	// the app has none.
	AppMode spec.EgressMode `json:"app_mode,omitempty"`
	AppList []string        `json:"app_list,omitempty"`

	BlockPrivate     bool   `json:"block_private"`
	BlockPrivateFrom string `json:"block_private_from,omitempty"` // install | app

	// Loosenings is every way this app is looser than the install, and Gate
	// is what policy says they need. Gate is empty when there are none.
	Loosenings []Loosening     `json:"loosenings,omitempty"`
	Gate       EgressLoosening `json:"gate,omitempty"`

	// Unused is the app's additions and removals that change nothing — adding
	// to an install with no list, removing an entry it does not have. Shown as
	// a warning (never a blocker): somebody wrote them expecting an effect.
	Unused []string `json:"unused,omitempty"`

	// Restricted is whether anything is refused at all. When it is not, the
	// app runs exactly as it would with no egress controls (R-186).
	Restricted bool `json:"restricted"`

	// Rules is what a runtime adapter enforces.
	Rules egress.Rules `json:"-"`
}

// Forbidden reports whether the app loosens rules that policy says may not
// be loosened. Refused at plan time (R-183).
func (e EffectiveEgress) Forbidden() bool {
	return len(e.Loosenings) > 0 && e.Gate == EgressLooseningForbidden
}

// NeedsApproval reports whether the app's loosenings make its deploy need
// approval (R-154).
func (e EffectiveEgress) NeedsApproval() bool {
	return len(e.Loosenings) > 0 && e.Gate == EgressLooseningApproval
}

// EgressFor resolves an app's egress against the installation (R-182).
//
// Entries that do not parse are left out rather than failing here: the spec
// and the policy are each validated when written, and a plan shown to
// somebody should not fail on a list it was already told about.
func (d Document) EgressFor(app spec.Egress) EffectiveEgress {
	app.Normalize()
	mode, installList := d.EgressInstallMode()

	out := EffectiveEgress{Mode: mode, List: []EgressEntry{}}

	// The install's list, with the app's removals and additions applied.
	installEntries, _ := egress.ParseAll(installList)
	removes, _ := egress.ParseAll(app.Remove)
	adds, _ := egress.ParseAll(app.Add)

	removed := make([]bool, len(removes))
	var merged []egress.Entry
	for _, e := range installEntries {
		gone := false
		for i, r := range removes {
			if e.Same(r) {
				removed[i] = true
				gone = true
			}
		}
		if gone {
			if mode == spec.EgressDenylist {
				out.Loosenings = append(out.Loosenings, Loosening{
					Kind: LoosenDenylistRemove, Entry: e.Raw,
					Message: fmt.Sprintf("Removes %s from the installation's denylist, so this app can reach it.", e.Raw),
				})
			}
			continue
		}
		merged = append(merged, e)
		out.List = append(out.List, EgressEntry{Entry: e.Raw, From: "install"})
	}
	for i, r := range removes {
		if !removed[i] {
			out.Unused = append(out.Unused, fmt.Sprintf("Removing %s changes nothing: the installation's list does not have it.", r.Raw))
		}
	}
	for _, a := range adds {
		if mode == spec.EgressAllowAll {
			out.Unused = append(out.Unused, fmt.Sprintf("Adding %s changes nothing: the installation has no egress list. To limit this app, give it a list of its own.", a.Raw))
			continue
		}
		dup := false
		for _, e := range merged {
			if e.Same(a) {
				dup = true
			}
		}
		if dup {
			out.Unused = append(out.Unused, fmt.Sprintf("Adding %s changes nothing: the installation's list already has it.", a.Raw))
			continue
		}
		merged = append(merged, a)
		out.List = append(out.List, EgressEntry{Entry: a.Raw, From: "app"})
		if mode == spec.EgressAllowlist {
			out.Loosenings = append(out.Loosenings, Loosening{
				Kind: LoosenAllowlistAdd, Entry: a.Raw,
				Message: fmt.Sprintf("Adds %s to the installation's allowlist, so this app can reach it.", a.Raw),
			})
		}
	}

	// Private ranges: the app may turn it on, or off.
	out.BlockPrivate = d.EgressBlockPrivate
	if d.EgressBlockPrivate {
		out.BlockPrivateFrom = "install"
	}
	if app.BlockPrivate != nil && *app.BlockPrivate != d.EgressBlockPrivate {
		out.BlockPrivate = *app.BlockPrivate
		out.BlockPrivateFrom = "app"
		if !*app.BlockPrivate {
			out.BlockPrivateFrom = ""
			out.Loosenings = append(out.Loosenings, Loosening{
				Kind:    LoosenBlockPrivate,
				Message: "Turns off the installation's block on private addresses, so this app can reach addresses on the local network.",
			})
		}
	}

	// The rules the runtime enforces: the merged install layer, then the
	// app's own on top.
	var raw []string
	for _, e := range merged {
		raw = append(raw, e.Raw)
	}
	switch mode {
	case spec.EgressAllowlist:
		out.Rules.Layers = append(out.Rules.Layers, egress.Layer{Mode: egress.Allowlist, List: nonNil(raw), From: "install"})
	case spec.EgressDenylist:
		out.Rules.Layers = append(out.Rules.Layers, egress.Layer{Mode: egress.Denylist, List: nonNil(raw), From: "install"})
	}
	if app.Mode == spec.EgressAllowlist || app.Mode == spec.EgressDenylist {
		own, _ := egress.ParseAll(app.List)
		var list []string
		for _, e := range own {
			list = append(list, e.Raw)
		}
		out.AppMode = app.Mode
		out.AppList = nonNil(list)
		out.Rules.Layers = append(out.Rules.Layers, egress.Layer{Mode: egress.Mode(app.Mode), List: nonNil(list), From: "app"})
	}
	out.Rules.BlockPrivate = out.BlockPrivate
	out.Restricted = out.Rules.Restricted()

	if len(out.Loosenings) > 0 {
		out.Gate = d.EgressLooseningRule()
	}
	return out
}

// NewLoosenings is what next loosens that prev did not: the part of a change
// the loosening gate is asked about (R-183). A loosening already running was
// already allowed.
func NewLoosenings(prev, next EffectiveEgress) []Loosening {
	have := make(map[string]bool, len(prev.Loosenings))
	for _, l := range prev.Loosenings {
		have[l.Kind+"\x00"+l.Entry] = true
	}
	var out []Loosening
	for _, l := range next.Loosenings {
		if !have[l.Kind+"\x00"+l.Entry] {
			out = append(out, l)
		}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
