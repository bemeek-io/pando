package approval_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/approval"
	"github.com/trypando/pando/internal/core/policy"
	"github.com/trypando/pando/internal/core/spec"
)

const appID = "app_01HQ8"

func plainSpec() *spec.AppSpec { return &spec.AppSpec{AppID: appID} }

func requiringApproval() *spec.AppSpec {
	s := plainSpec()
	s.Deploy.RequireApproval = true
	return s
}

func addingTo(entries ...string) *spec.AppSpec {
	s := plainSpec()
	s.Egress = spec.Egress{Add: entries}
	return s
}

func allowlistUnder(rule policy.EgressLoosening) policy.Document {
	return policy.Document{EgressMode: spec.EgressAllowlist, EgressList: []string{"api.github.com"}, EgressLoosening: rule}
}

// TestR154_ReasonsADeployNeedsApproval asserts R-154: each of the four
// reasons, alone and together, and that none applies by default (R-002).
func TestR154_ReasonsADeployNeedsApproval(t *testing.T) {
	cases := []struct {
		name    string
		doc     policy.Document
		running *spec.AppSpec
		next    *spec.AppSpec
		want    []approval.Reason
	}{
		{"nothing by default", policy.Default(), plainSpec(), plainSpec(), nil},
		{"first deploy, nothing", policy.Default(), nil, plainSpec(), nil},
		{"install-wide", policy.Document{DeployApprovalRequired: true}, plainSpec(), plainSpec(),
			[]approval.Reason{approval.ReasonInstall}},
		{"this app, by policy", policy.Document{DeployApprovalApps: []string{"app_other", appID}}, plainSpec(), plainSpec(),
			[]approval.Reason{approval.ReasonAppPolicy}},
		{"another app, by policy", policy.Document{DeployApprovalApps: []string{"app_other"}}, plainSpec(), plainSpec(), nil},
		{"the next spec asks", policy.Default(), plainSpec(), requiringApproval(),
			[]approval.Reason{approval.ReasonAppSpec}},
		{"the running spec asks, so turning it off is approved", policy.Default(), requiringApproval(), plainSpec(),
			[]approval.Reason{approval.ReasonAppSpec}},
		{"a new loosening under approval", allowlistUnder(policy.EgressLooseningApproval), plainSpec(), addingTo("api.openai.example"),
			[]approval.Reason{approval.ReasonEgressLoosening}},
		{"a first deploy that loosens under approval", allowlistUnder(policy.EgressLooseningApproval), nil, addingTo("api.openai.example"),
			[]approval.Reason{approval.ReasonEgressLoosening}},
		{"a loosening under verb is not approval's", allowlistUnder(policy.EgressLooseningVerb), plainSpec(), addingTo("api.openai.example"), nil},
		{"a tightening is not a loosening", allowlistUnder(policy.EgressLooseningApproval), plainSpec(),
			&spec.AppSpec{AppID: appID, Egress: spec.Egress{Remove: []string{"api.github.com"}}}, nil},
		{"every reason at once",
			func() policy.Document {
				d := allowlistUnder(policy.EgressLooseningApproval)
				d.DeployApprovalRequired = true
				d.DeployApprovalApps = []string{appID}
				return d
			}(),
			plainSpec(),
			func() *spec.AppSpec { s := addingTo("api.openai.example"); s.Deploy.RequireApproval = true; return s }(),
			[]approval.Reason{approval.ReasonInstall, approval.ReasonAppPolicy, approval.ReasonAppSpec, approval.ReasonEgressLoosening}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, approval.Reasons(tc.doc, appID, tc.running, tc.next))
		})
	}
}

// TestR154_AnEgressLooseningAlreadyRunningDoesNotNeedApprovalAgain asserts
// R-154 with R-183: the loosening was approved when it was deployed, and a
// later deploy that keeps it is not asking for anything more. One that adds
// another loosening is.
func TestR154_AnEgressLooseningAlreadyRunningDoesNotNeedApprovalAgain(t *testing.T) {
	doc := allowlistUnder(policy.EgressLooseningApproval)
	running := addingTo("api.openai.example")

	require.Empty(t, approval.Reasons(doc, appID, running, addingTo("api.openai.example")))
	require.Equal(t, []approval.Reason{approval.ReasonEgressLoosening},
		approval.Reasons(doc, appID, running, addingTo("api.openai.example", "api.anthropic.example")))
	require.Empty(t, approval.Reasons(doc, appID, running, plainSpec()), "dropping a loosening tightens")
}

// TestR158_WhatBlocksAutoDeploy asserts R-158: every reason that applies to
// every deploy blocks auto-deploy, and an egress loosening — which needs
// approval only for the deploy that brings it — does not.
func TestR158_WhatBlocksAutoDeploy(t *testing.T) {
	require.False(t, approval.BlocksAutoDeploy(policy.Default(), appID, plainSpec()))
	require.True(t, approval.BlocksAutoDeploy(policy.Document{DeployApprovalRequired: true}, appID, plainSpec()))
	require.True(t, approval.BlocksAutoDeploy(policy.Document{DeployApprovalApps: []string{appID}}, appID, plainSpec()))
	require.False(t, approval.BlocksAutoDeploy(policy.Document{DeployApprovalApps: []string{"app_other"}}, appID, plainSpec()))
	require.True(t, approval.BlocksAutoDeploy(policy.Default(), appID, requiringApproval()))
	require.False(t, approval.BlocksAutoDeploy(allowlistUnder(policy.EgressLooseningApproval), appID, addingTo("api.openai.example")))
}

// TestR154_EveryReasonSaysWhatItMeans asserts R-105 for the reasons an
// approver is shown.
func TestR154_EveryReasonSaysWhatItMeans(t *testing.T) {
	for _, r := range []approval.Reason{approval.ReasonInstall, approval.ReasonAppPolicy, approval.ReasonAppSpec, approval.ReasonEgressLoosening} {
		m := r.Message()
		require.NotEqual(t, string(r), m)
		require.True(t, len(m) > 20 && m[len(m)-1] == '.', m)
	}
}
