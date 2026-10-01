package policy_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/policy"
	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/egress"
)

func on() *bool  { v := true; return &v }
func off() *bool { v := false; return &v }

// The three installs every case is read against.
func allowAllInstall() policy.Document { return policy.Document{} }

func denylistInstall() policy.Document {
	return policy.Document{EgressMode: spec.EgressDenylist, EgressList: []string{"evil.example", "10.0.0.0/8"}}
}

func allowlistInstall() policy.Document {
	return policy.Document{EgressMode: spec.EgressAllowlist, EgressList: []string{"api.github.com", "*.stripe.com"}}
}

func withBlockPrivate(d policy.Document) policy.Document {
	d.EgressBlockPrivate = true
	return d
}

type entryFrom = policy.EgressEntry

func kinds(ls []policy.Loosening) []string {
	out := []string{}
	for _, l := range ls {
		out = append(out, l.Kind+":"+l.Entry)
	}
	return out
}

// TestR182_AppEgressResolvesAgainstTheInstallation asserts R-181 and R-182:
// every combination of the installation's mode with what an app may do —
// add, remove, keep a list of its own, switch private-range blocking — and
// what each resolves to, including which parts loosen.
func TestR182_AppEgressResolvesAgainstTheInstallation(t *testing.T) {
	cases := []struct {
		name    string
		install policy.Document
		app     spec.Egress

		mode       spec.EgressMode
		list       []entryFrom
		appMode    spec.EgressMode
		appList    []string
		block      bool
		blockFrom  string
		loosenings []string
		unused     int
		restricted bool
		layers     int
	}{
		// --- allow_all -------------------------------------------------
		{name: "allow_all, inherit", install: allowAllInstall(), app: spec.Egress{},
			mode: spec.EgressAllowAll, list: []entryFrom{}, loosenings: []string{}},
		{name: "allow_all, add changes nothing", install: allowAllInstall(),
			app:  spec.Egress{Add: []string{"api.example.com"}},
			mode: spec.EgressAllowAll, list: []entryFrom{}, loosenings: []string{}, unused: 1},
		{name: "allow_all, remove changes nothing", install: allowAllInstall(),
			app:  spec.Egress{Remove: []string{"api.example.com"}},
			mode: spec.EgressAllowAll, list: []entryFrom{}, loosenings: []string{}, unused: 1},
		{name: "allow_all, own allowlist", install: allowAllInstall(),
			app:  spec.Egress{Mode: spec.EgressAllowlist, List: []string{"api.example.com"}},
			mode: spec.EgressAllowAll, list: []entryFrom{}, appMode: spec.EgressAllowlist, appList: []string{"api.example.com"},
			loosenings: []string{}, restricted: true, layers: 1},
		{name: "allow_all, own denylist", install: allowAllInstall(),
			app:  spec.Egress{Mode: spec.EgressDenylist, List: []string{"ads.example.com"}},
			mode: spec.EgressAllowAll, list: []entryFrom{}, appMode: spec.EgressDenylist, appList: []string{"ads.example.com"},
			loosenings: []string{}, restricted: true, layers: 1},
		{name: "allow_all, own empty denylist restricts nothing", install: allowAllInstall(),
			app:  spec.Egress{Mode: spec.EgressDenylist},
			mode: spec.EgressAllowAll, list: []entryFrom{}, appMode: spec.EgressDenylist, appList: []string{},
			loosenings: []string{}, layers: 1},
		{name: "allow_all, app blocks private", install: allowAllInstall(),
			app:  spec.Egress{BlockPrivate: on()},
			mode: spec.EgressAllowAll, list: []entryFrom{}, block: true, blockFrom: "app",
			loosenings: []string{}, restricted: true},
		{name: "allow_all, app block_private off where install has it off", install: allowAllInstall(),
			app:  spec.Egress{BlockPrivate: off()},
			mode: spec.EgressAllowAll, list: []entryFrom{}, loosenings: []string{}},
		{name: "allow_all + block_private, inherit", install: withBlockPrivate(allowAllInstall()),
			app:  spec.Egress{},
			mode: spec.EgressAllowAll, list: []entryFrom{}, block: true, blockFrom: "install",
			loosenings: []string{}, restricted: true},
		{name: "allow_all + block_private, app on as well", install: withBlockPrivate(allowAllInstall()),
			app:  spec.Egress{BlockPrivate: on()},
			mode: spec.EgressAllowAll, list: []entryFrom{}, block: true, blockFrom: "install",
			loosenings: []string{}, restricted: true},
		{name: "allow_all + block_private, app turns it off", install: withBlockPrivate(allowAllInstall()),
			app:  spec.Egress{BlockPrivate: off()},
			mode: spec.EgressAllowAll, list: []entryFrom{},
			loosenings: []string{"block_private_off:"}},

		// --- denylist --------------------------------------------------
		{name: "denylist, inherit", install: denylistInstall(), app: spec.Egress{},
			mode:       spec.EgressDenylist,
			list:       []entryFrom{{Entry: "evil.example", From: "install"}, {Entry: "10.0.0.0/8", From: "install"}},
			loosenings: []string{}, restricted: true, layers: 1},
		{name: "denylist, add tightens", install: denylistInstall(),
			app:        spec.Egress{Add: []string{"tracker.example"}},
			mode:       spec.EgressDenylist,
			list:       []entryFrom{{Entry: "evil.example", From: "install"}, {Entry: "10.0.0.0/8", From: "install"}, {Entry: "tracker.example", From: "app"}},
			loosenings: []string{}, restricted: true, layers: 1},
		{name: "denylist, add an entry it has changes nothing", install: denylistInstall(),
			app:        spec.Egress{Add: []string{"EVIL.example."}},
			mode:       spec.EgressDenylist,
			list:       []entryFrom{{Entry: "evil.example", From: "install"}, {Entry: "10.0.0.0/8", From: "install"}},
			loosenings: []string{}, unused: 1, restricted: true, layers: 1},
		{name: "denylist, remove loosens", install: denylistInstall(),
			app:        spec.Egress{Remove: []string{"evil.example"}},
			mode:       spec.EgressDenylist,
			list:       []entryFrom{{Entry: "10.0.0.0/8", From: "install"}},
			loosenings: []string{"denylist_remove:evil.example"}, restricted: true, layers: 1},
		{name: "denylist, remove however it is spelled", install: denylistInstall(),
			app:        spec.Egress{Remove: []string{"Evil.Example."}},
			mode:       spec.EgressDenylist,
			list:       []entryFrom{{Entry: "10.0.0.0/8", From: "install"}},
			loosenings: []string{"denylist_remove:evil.example"}, restricted: true, layers: 1},
		{name: "denylist, remove an entry it does not have", install: denylistInstall(),
			app:        spec.Egress{Remove: []string{"other.example"}},
			mode:       spec.EgressDenylist,
			list:       []entryFrom{{Entry: "evil.example", From: "install"}, {Entry: "10.0.0.0/8", From: "install"}},
			loosenings: []string{}, unused: 1, restricted: true, layers: 1},
		{name: "denylist, remove everything restricts nothing", install: denylistInstall(),
			app:  spec.Egress{Remove: []string{"evil.example", "10.0.0.0/8"}},
			mode: spec.EgressDenylist, list: []entryFrom{},
			loosenings: []string{"denylist_remove:evil.example", "denylist_remove:10.0.0.0/8"}, layers: 1},
		{name: "denylist, own allowlist on top", install: denylistInstall(),
			app:     spec.Egress{Mode: spec.EgressAllowlist, List: []string{"api.example.com"}},
			mode:    spec.EgressDenylist,
			list:    []entryFrom{{Entry: "evil.example", From: "install"}, {Entry: "10.0.0.0/8", From: "install"}},
			appMode: spec.EgressAllowlist, appList: []string{"api.example.com"},
			loosenings: []string{}, restricted: true, layers: 2},
		{name: "denylist + block_private, app turns it off and removes", install: withBlockPrivate(denylistInstall()),
			app:        spec.Egress{BlockPrivate: off(), Remove: []string{"10.0.0.0/8"}},
			mode:       spec.EgressDenylist,
			list:       []entryFrom{{Entry: "evil.example", From: "install"}},
			loosenings: []string{"denylist_remove:10.0.0.0/8", "block_private_off:"}, restricted: true, layers: 1},

		// --- allowlist -------------------------------------------------
		{name: "allowlist, inherit", install: allowlistInstall(), app: spec.Egress{},
			mode:       spec.EgressAllowlist,
			list:       []entryFrom{{Entry: "api.github.com", From: "install"}, {Entry: "*.stripe.com", From: "install"}},
			loosenings: []string{}, restricted: true, layers: 1},
		{name: "allowlist, add loosens", install: allowlistInstall(),
			app:        spec.Egress{Add: []string{"api.openai.example"}},
			mode:       spec.EgressAllowlist,
			list:       []entryFrom{{Entry: "api.github.com", From: "install"}, {Entry: "*.stripe.com", From: "install"}, {Entry: "api.openai.example", From: "app"}},
			loosenings: []string{"allowlist_add:api.openai.example"}, restricted: true, layers: 1},
		{name: "allowlist, remove tightens", install: allowlistInstall(),
			app:        spec.Egress{Remove: []string{"api.github.com"}},
			mode:       spec.EgressAllowlist,
			list:       []entryFrom{{Entry: "*.stripe.com", From: "install"}},
			loosenings: []string{}, restricted: true, layers: 1},
		{name: "allowlist, remove everything still restricts", install: allowlistInstall(),
			app:  spec.Egress{Remove: []string{"api.github.com", "*.stripe.com"}},
			mode: spec.EgressAllowlist, list: []entryFrom{},
			loosenings: []string{}, restricted: true, layers: 1},
		{name: "allowlist, own denylist on top", install: allowlistInstall(),
			app:     spec.Egress{Mode: spec.EgressDenylist, List: []string{"*.stripe.com"}},
			mode:    spec.EgressAllowlist,
			list:    []entryFrom{{Entry: "api.github.com", From: "install"}, {Entry: "*.stripe.com", From: "install"}},
			appMode: spec.EgressDenylist, appList: []string{"*.stripe.com"},
			loosenings: []string{}, restricted: true, layers: 2},
		{name: "allowlist, app blocks private", install: allowlistInstall(),
			app:   spec.Egress{BlockPrivate: on()},
			mode:  spec.EgressAllowlist,
			list:  []entryFrom{{Entry: "api.github.com", From: "install"}, {Entry: "*.stripe.com", From: "install"}},
			block: true, blockFrom: "app", loosenings: []string{}, restricted: true, layers: 1},

		// --- specs written before issue #79 ----------------------------
		{name: "legacy app allowlist is the app's own list", install: allowAllInstall(),
			app:  spec.Egress{Mode: spec.EgressAllowlist, Allowlist: []string{"api.example.com"}},
			mode: spec.EgressAllowAll, list: []entryFrom{}, appMode: spec.EgressAllowlist, appList: []string{"api.example.com"},
			loosenings: []string{}, restricted: true, layers: 1},
		{name: "legacy block_private mode is the switch", install: allowAllInstall(),
			app:  spec.Egress{Mode: spec.EgressBlockPrivate},
			mode: spec.EgressAllowAll, list: []entryFrom{}, block: true, blockFrom: "app",
			loosenings: []string{}, restricted: true},
		{name: "legacy allow_all mode inherits", install: denylistInstall(),
			app:        spec.Egress{Mode: spec.EgressAllowAll},
			mode:       spec.EgressDenylist,
			list:       []entryFrom{{Entry: "evil.example", From: "install"}, {Entry: "10.0.0.0/8", From: "install"}},
			loosenings: []string{}, restricted: true, layers: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eff := tc.install.EgressFor(tc.app)
			require.Equal(t, tc.mode, eff.Mode)
			require.Equal(t, tc.list, eff.List)
			require.Equal(t, tc.appMode, eff.AppMode)
			require.Equal(t, tc.appList, eff.AppList)
			require.Equal(t, tc.block, eff.BlockPrivate)
			require.Equal(t, tc.blockFrom, eff.BlockPrivateFrom)
			require.Equal(t, tc.loosenings, kinds(eff.Loosenings))
			require.Len(t, eff.Unused, tc.unused)
			require.Equal(t, tc.restricted, eff.Restricted)
			require.Equal(t, tc.restricted, eff.Rules.Restricted(), "what the plan says is what the runtime is given")
			require.Len(t, eff.Rules.Layers, tc.layers)
			require.Equal(t, tc.block, eff.Rules.BlockPrivate)
			for _, l := range eff.Loosenings {
				require.NotEmpty(t, l.Message, "R-105: every loosening says what it does")
			}
			if len(eff.Loosenings) > 0 {
				require.Equal(t, policy.EgressLooseningVerb, eff.Gate, "verb is the default gate")
			} else {
				require.Empty(t, eff.Gate)
			}
			_, err := eff.Rules.Compile()
			require.NoError(t, err)
		})
	}
}

// TestR182_OnlyThreeThingsLoosen asserts R-182: of everything an app can
// write, only adding to an allowlist, removing from a denylist and turning
// off the installation's private-range block loosen.
func TestR182_OnlyThreeThingsLoosen(t *testing.T) {
	seen := map[string]bool{}
	for _, install := range []policy.Document{
		withBlockPrivate(allowAllInstall()), withBlockPrivate(denylistInstall()), withBlockPrivate(allowlistInstall()),
	} {
		for _, app := range []spec.Egress{
			{Add: []string{"new.example", "evil.example", "api.github.com"}},
			{Remove: []string{"evil.example", "api.github.com", "missing.example"}},
			{Mode: spec.EgressAllowlist, List: []string{"x.example"}},
			{Mode: spec.EgressDenylist, List: []string{"x.example"}},
			{BlockPrivate: on()},
			{BlockPrivate: off()},
		} {
			for _, l := range install.EgressFor(app).Loosenings {
				seen[l.Kind] = true
			}
		}
	}
	require.Equal(t, map[string]bool{
		policy.LoosenAllowlistAdd: true, policy.LoosenDenylistRemove: true, policy.LoosenBlockPrivate: true,
	}, seen)
}

// TestR181_TheInstallationsModeIsReadFromEitherField asserts R-181 and the
// field from before issue #79: a policy that set only egress_allowlist is an
// allowlist, and an explicit mode wins over it.
func TestR181_TheInstallationsModeIsReadFromEitherField(t *testing.T) {
	legacy := policy.Document{EgressAllowlist: []string{"api.example.com"}}
	mode, list := legacy.EgressInstallMode()
	require.Equal(t, spec.EgressAllowlist, mode)
	require.Equal(t, []string{"api.example.com"}, list)

	eff := legacy.EgressFor(spec.Egress{})
	require.Equal(t, spec.EgressAllowlist, eff.Mode)
	require.Equal(t, []policy.EgressEntry{{Entry: "api.example.com", From: "install"}}, eff.List)
	require.True(t, eff.Restricted)

	explicit := policy.Document{EgressMode: spec.EgressAllowAll, EgressAllowlist: []string{"api.example.com"}}
	mode, _ = explicit.EgressInstallMode()
	require.Equal(t, spec.EgressAllowAll, mode, "an explicit allow_all is what its author chose")

	newer := policy.Document{EgressMode: spec.EgressDenylist, EgressList: []string{"evil.example"}, EgressAllowlist: []string{"api.example.com"}}
	mode, list = newer.EgressInstallMode()
	require.Equal(t, spec.EgressDenylist, mode)
	require.Equal(t, []string{"evil.example"}, list)

	require.Equal(t, policy.InstallEgress{
		Mode: spec.EgressAllowlist, List: []string{"api.example.com"}, Loosening: policy.EgressLooseningVerb,
	}, legacy.InstallEgress())
	require.Equal(t, policy.InstallEgress{
		Mode: spec.EgressAllowAll, List: []string{}, BlockPrivate: true, Loosening: policy.EgressLooseningForbidden,
	}, policy.Document{EgressBlockPrivate: true, EgressLoosening: policy.EgressLooseningForbidden}.InstallEgress())
}

// TestR181_AllowAllWithNoBlockIsNoRestriction asserts R-181 and R-186: the
// default installation restricts nothing, so nothing is placed in an app's
// path.
func TestR181_AllowAllWithNoBlockIsNoRestriction(t *testing.T) {
	eff := policy.Default().EgressFor(spec.Egress{})
	require.False(t, eff.Restricted)
	require.Empty(t, eff.Rules.Layers)
	require.False(t, eff.Rules.BlockPrivate)
	require.Empty(t, eff.Loosenings)
}

// TestR183_TheGateIsWhatPolicySays asserts R-183: every loosening carries
// what policy says it needs, and Forbidden and NeedsApproval read it.
func TestR183_TheGateIsWhatPolicySays(t *testing.T) {
	app := spec.Egress{Add: []string{"api.openai.example"}}
	for _, tc := range []struct {
		rule      policy.EgressLoosening
		want      policy.EgressLoosening
		forbidden bool
		approval  bool
	}{
		{"", policy.EgressLooseningVerb, false, false},
		{policy.EgressLooseningVerb, policy.EgressLooseningVerb, false, false},
		{policy.EgressLooseningApproval, policy.EgressLooseningApproval, false, true},
		{policy.EgressLooseningForbidden, policy.EgressLooseningForbidden, true, false},
	} {
		doc := allowlistInstall()
		doc.EgressLoosening = tc.rule
		eff := doc.EgressFor(app)
		require.Equal(t, tc.want, eff.Gate, string(tc.rule))
		require.Equal(t, tc.forbidden, eff.Forbidden(), string(tc.rule))
		require.Equal(t, tc.approval, eff.NeedsApproval(), string(tc.rule))

		// Tightening is never gated, whatever the rule.
		tight := doc.EgressFor(spec.Egress{Remove: []string{"api.github.com"}})
		require.Empty(t, tight.Gate)
		require.False(t, tight.Forbidden())
		require.False(t, tight.NeedsApproval())
	}

	require.Error(t, policy.EgressLoosening("sometimes").Valid())
	require.Error(t, policy.Document{EgressMode: "everything"}.ValidateEgress())
	require.Error(t, policy.Document{EgressMode: spec.EgressDenylist, EgressList: []string{"not a host"}}.ValidateEgress())
	require.NoError(t, denylistInstall().ValidateEgress())
}

// TestR183_OnlyNewLooseningsAreAskedAbout asserts R-183: a loosening the
// running spec already carries was allowed when it was written, and keeping
// it does not ask again.
func TestR183_OnlyNewLooseningsAreAskedAbout(t *testing.T) {
	doc := withBlockPrivate(denylistInstall())
	prev := doc.EgressFor(spec.Egress{Remove: []string{"evil.example"}})
	next := doc.EgressFor(spec.Egress{Remove: []string{"evil.example", "10.0.0.0/8"}, BlockPrivate: off()})

	require.Equal(t, []string{"denylist_remove:10.0.0.0/8", "block_private_off:"}, kinds(policy.NewLoosenings(prev, next)))
	require.Empty(t, policy.NewLoosenings(next, next), "nothing new")
	require.Empty(t, policy.NewLoosenings(next, prev), "removing a loosening is not one")
	require.Equal(t, []string{"denylist_remove:evil.example"}, kinds(policy.NewLoosenings(policy.EffectiveEgress{}, prev)),
		"from nothing, every loosening is new")
}

// TestR185_TheMergedRulesDecideConnections asserts R-182 and R-185 end to
// end on the resolved rules: both layers must allow, and the private-range
// block is checked against where a name resolved.
func TestR185_TheMergedRulesDecideConnections(t *testing.T) {
	doc := withBlockPrivate(allowlistInstall())
	eff := doc.EgressFor(spec.Egress{
		Add:  []string{"api.openai.example:443"},
		Mode: spec.EgressDenylist, List: []string{"api.github.com"},
	})
	c, err := eff.Rules.Compile()
	require.NoError(t, err)

	public := netip.MustParseAddr("203.0.113.7")
	require.True(t, c.Allows("payments.stripe.com", 443, public).Allowed, "on the installation's allowlist")
	require.True(t, c.Allows("api.openai.example", 443, public).Allowed, "added by the app")
	require.False(t, c.Allows("api.openai.example", 80, public).Allowed, "added for one port only")
	require.False(t, c.Allows("api.github.com", 443, public).Allowed, "on the app's own denylist")
	require.False(t, c.Allows("example.org", 443, public).Allowed, "on neither list")
	d := c.Allows("payments.stripe.com", 443, netip.MustParseAddr("10.1.2.3"))
	require.False(t, d.Allowed, "a public name resolving to a private address")
	require.Contains(t, d.Reason, "private")

	require.True(t, egress.Private(netip.MustParseAddr("169.254.169.254")), "the metadata address")
}
