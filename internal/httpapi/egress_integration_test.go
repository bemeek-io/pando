//go:build integration

package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	adapterapi "github.com/trypando/pando/internal/adapter/api"
	corepolicy "github.com/trypando/pando/internal/core/policy"
	"github.com/trypando/pando/internal/core/spec"
)

// egressRuntime is a runtime that enforces egress rules (R-186) and has room
// for anything, so a plan gets as far as its egress.
type egressRuntime struct{ adapterapi.RuntimeAdapter }

func (egressRuntime) Kind() string                                     { return "docker" }
func (egressRuntime) Category() adapterapi.Category                    { return adapterapi.CategoryRuntime }
func (egressRuntime) Configure(context.Context, json.RawMessage) error { return nil }
func (egressRuntime) HealthCheck(context.Context) error                { return nil }
func (egressRuntime) Capabilities(context.Context) (adapterapi.RuntimeCapabilities, error) {
	return adapterapi.RuntimeCapabilities{
		IsolationClass:            spec.IsolationContainer,
		SupportsPersistentVolumes: true,
		SupportsMultipleWorkloads: true,
		SupportsPrivateNetwork:    true,
		SupportsResourceLimits:    true,
		SupportsEgressRestriction: true,
		LogRetention:              adapterapi.LogRetentionCapability{SupportsSizeCap: true},
	}, nil
}
func (egressRuntime) Capacity(context.Context) (adapterapi.Capacity, error) {
	return adapterapi.Capacity{TotalCPUMillis: 64000, TotalMemoryBytes: 256 << 30, TotalDiskBytes: 4 << 40, Reported: time.Now()}, nil
}

type subdomainRouting struct{ adapterapi.RoutingAdapter }

func (subdomainRouting) Kind() string                                     { return "loopback" }
func (subdomainRouting) Category() adapterapi.Category                    { return adapterapi.CategoryRouting }
func (subdomainRouting) Configure(context.Context, json.RawMessage) error { return nil }
func (subdomainRouting) HealthCheck(context.Context) error                { return nil }
func (subdomainRouting) Capabilities(context.Context) (adapterapi.RoutingCapabilities, error) {
	return adapterapi.RoutingCapabilities{Modes: []adapterapi.RoutingMode{spec.RoutingSubdomain}, DefaultMode: spec.RoutingSubdomain}, nil
}

// withEgressAdapters gives an install a runtime and a routing adapter, so a
// spec written to it plans.
func withEgressAdapters(t *testing.T, i *install) {
	t.Helper()
	reg := i.Server.Registry
	require.NoError(t, reg.Register("rt_docker", egressRuntime{}))
	require.NoError(t, reg.SetDefault(adapterapi.CategoryRuntime, "rt_docker"))
	require.NoError(t, reg.Register("rte_loopback", subdomainRouting{}))
	require.NoError(t, reg.SetDefault(adapterapi.CategoryRouting, "rte_loopback"))
}

func (i *install) setPolicy(change func(*corepolicy.Document)) {
	i.t.Helper()
	ctx := context.Background()
	doc, err := i.PolicyStore.Load(ctx)
	require.NoError(i.t, err)
	change(&doc)
	require.NoError(i.t, i.PolicyStore.Save(ctx, doc, i.AdminID))
}

// grantControl gives a user a control-plane role on an app.
func (i *install) grantControl(appID string, who *session, roleID string) {
	i.t.Helper()
	got := i.do(i.admin(), http.MethodPost, "/apps/"+appID+"/grants", map[string]any{
		"plane": "control", "principal_kind": "user", "principal_id": i.userID(who), "role_id": roleID,
	})
	require.Equal(i.t, http.StatusCreated, got.Code, got.String())
}

func specWithEgress(egress map[string]any) map[string]any {
	s := minimalSpec()
	s["egress"] = egress
	return s
}

func (r reply) envelope(t *testing.T) (message, remedy string) {
	t.Helper()
	var env struct {
		Message string `json:"message"`
		Remedy  string `json:"remedy"`
	}
	r.JSON(t, &env)
	return env.Message, env.Remedy
}

// TestR184_WritingASpecGatesItsEgressChanges asserts R-183 and R-184
// through POST /apps/{id}/specs, against the built-in roles: Operator holds
// app.egress.tighten, Owner both, and what loosening needs is host policy's.
func TestR184_WritingASpecGatesItsEgressChanges(t *testing.T) {
	t.Parallel()
	i := newInstall(t)
	admin := i.admin()
	appID := i.appWithSpec(admin, "notes")

	olive := i.user("olive")
	i.grantControl(appID, olive, "role_operator")

	i.setPolicy(func(d *corepolicy.Document) {
		d.EgressMode = spec.EgressAllowlist
		d.EgressList = []string{"api.github.com", "*.stripe.com"}
	})

	// Tightening: Operator may.
	i.writeSpec(olive, appID, specWithEgress(map[string]any{"remove": []string{"*.stripe.com"}, "block_private": true}))

	// Loosening under the default gate (verb): Operator may not, and is told
	// which entry and which verb.
	refused := i.do(olive, http.MethodPost, "/apps/"+appID+"/specs",
		specWithEgress(map[string]any{"add": []string{"api.openai.example"}}))
	require.Equal(t, http.StatusForbidden, refused.Code, refused.String())
	require.Equal(t, "PERM_VERB_REQUIRED", refused.ErrorCode())
	msg, remedy := refused.envelope(t)
	require.Contains(t, msg, "api.openai.example")
	require.Contains(t, msg, "app.egress.loosen")
	require.NotEmpty(t, remedy)

	// That the refusal is audited is asserted in specgate's tests: this
	// harness wires the authorizer without an auditor.

	// The owner may.
	i.writeSpec(admin, appID, specWithEgress(map[string]any{"add": []string{"api.openai.example"}}))

	// approval: Operator may propose a loosening.
	i.setPolicy(func(d *corepolicy.Document) { d.EgressLoosening = corepolicy.EgressLooseningApproval })
	i.writeSpec(olive, appID, specWithEgress(map[string]any{"add": []string{"api.anthropic.example"}}))

	// forbidden: nobody may, the owner included.
	i.setPolicy(func(d *corepolicy.Document) { d.EgressLoosening = corepolicy.EgressLooseningForbidden })
	refused = i.do(admin, http.MethodPost, "/apps/"+appID+"/specs",
		specWithEgress(map[string]any{"add": []string{"api.example.org"}, "block_private": false}))
	require.GreaterOrEqual(t, refused.Code, 400, refused.String())
	require.Equal(t, "PLAN_EGRESS_LOOSENING_FORBIDDEN", refused.ErrorCode())
	msg, _ = refused.envelope(t)
	require.Contains(t, msg, "api.example.org")

	// An entry that does not parse is a validation error, before any gate.
	refused = i.do(admin, http.MethodPost, "/apps/"+appID+"/specs",
		specWithEgress(map[string]any{"add": []string{"not a host"}}))
	require.Equal(t, "VALID_INVALID", refused.ErrorCode(), refused.String())

	// Somebody who may edit the spec but not egress cannot change egress, and
	// can still change everything else.
	viewerRole := i.customRole(admin, "Spec editor", "app", "app.view", "app.spec.edit")
	ed := i.user("ed")
	i.grantControl(appID, ed, viewerRole)
	refused = i.do(ed, http.MethodPost, "/apps/"+appID+"/specs",
		specWithEgress(map[string]any{"remove": []string{"api.github.com"}}))
	require.Equal(t, "PERM_VERB_REQUIRED", refused.ErrorCode(), refused.String())
	msg, _ = refused.envelope(t)
	require.Contains(t, msg, "app.egress.tighten")
	i.writeSpec(ed, appID, minimalSpec())
}

// TestR158_WritingASpecRefusesAutoDeployUnderApproval asserts R-158 through
// POST /apps/{id}/specs: turning on auto-deploy for an app whose deploys need
// approval is refused, and the refusal says why.
func TestR158_WritingASpecRefusesAutoDeployUnderApproval(t *testing.T) {
	t.Parallel()
	i := newInstall(t)
	admin := i.admin()
	appID := i.appWithSpec(admin, "notes")

	auto := minimalSpec()
	auto["deploy"] = map[string]any{"strategy": "recreate", "auto_deploy": map[string]any{"enabled": true, "trigger": "branch_updated"}}
	i.writeSpec(admin, appID, auto)

	i.setPolicy(func(d *corepolicy.Document) { d.DeployApprovalApps = []string{appID} })
	refused := i.do(admin, http.MethodPost, "/apps/"+appID+"/specs", auto)
	require.Equal(t, "VALID_INVALID", refused.ErrorCode(), refused.String())
	msg, remedy := refused.envelope(t)
	require.Contains(t, msg, "approval")
	require.Contains(t, remedy, "administrator")
	require.False(t, strings.HasPrefix(msg, "Error"))
}

// TestR188_ThePlanAndTheEgressEndpointShowTheMergedRules asserts R-188 and
// R-154 through the API: the plan carries the merged rules, its notes and
// whether the deploy needs approval, and GET /egress shows an app owner the
// installation's rules without install.view.
func TestR188_ThePlanAndTheEgressEndpointShowTheMergedRules(t *testing.T) {
	t.Parallel()
	i := newInstall(t)
	withEgressAdapters(t, i)
	admin := i.admin()

	i.setPolicy(func(d *corepolicy.Document) {
		d.EgressMode = spec.EgressAllowlist
		d.EgressList = []string{"api.github.com"}
		d.EgressLoosening = corepolicy.EgressLooseningApproval
	})

	appID := i.createApp(admin, "notes")
	i.pinSpec(admin, appID, i.writeSpec(admin, appID, specWithEgress(map[string]any{"add": []string{"api.openai.example"}})))

	got := i.do(admin, http.MethodPost, "/apps/"+appID+"/plan", map[string]any{})
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var plan struct {
		Egress struct {
			Mode string `json:"mode"`
			List []struct {
				Entry string `json:"entry"`
				From  string `json:"from"`
			} `json:"list"`
			Loosenings []struct {
				Kind  string `json:"kind"`
				Entry string `json:"entry"`
			} `json:"loosenings"`
			Gate       string `json:"gate"`
			Restricted bool   `json:"restricted"`
		} `json:"egress"`
		Notes    []string `json:"notes"`
		Approval struct {
			Required bool `json:"required"`
			Reasons  []struct {
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"reasons"`
		} `json:"approval"`
	}
	got.JSON(t, &plan)
	require.Equal(t, "allowlist", plan.Egress.Mode)
	require.Len(t, plan.Egress.List, 2)
	require.Equal(t, "app", plan.Egress.List[1].From)
	require.Len(t, plan.Egress.Loosenings, 1)
	require.Equal(t, "approval", plan.Egress.Gate)
	require.True(t, plan.Egress.Restricted)
	require.NotEmpty(t, plan.Notes)
	require.True(t, plan.Approval.Required, "a new loosening under approval, against an app that has never run")
	require.Equal(t, "egress_loosening", plan.Approval.Reasons[0].Reason)
	require.NotEmpty(t, plan.Approval.Reasons[0].Message)

	// An app owner who holds no installation verb sees the rules too.
	carol := i.user("carol")
	i.grantControl(appID, carol, "role_owner")
	got = i.do(carol, http.MethodGet, "/apps/"+appID+"/egress", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var view struct {
		Install struct {
			Mode         string   `json:"mode"`
			List         []string `json:"list"`
			BlockPrivate bool     `json:"block_private"`
			Loosening    string   `json:"loosening"`
		} `json:"install"`
		Effective *struct {
			Mode string `json:"mode"`
		} `json:"effective"`
		Spec *struct {
			Mode string   `json:"mode"`
			Add  []string `json:"add"`
		} `json:"spec"`
	}
	got.JSON(t, &view)
	require.Equal(t, "allowlist", view.Install.Mode)
	require.Equal(t, []string{"api.github.com"}, view.Install.List)
	require.Equal(t, "approval", view.Install.Loosening)
	require.NotNil(t, view.Effective)
	require.Equal(t, "allowlist", view.Effective.Mode)
	require.NotNil(t, view.Spec)
	require.Equal(t, "inherit", view.Spec.Mode)
	require.Equal(t, []string{"api.openai.example"}, view.Spec.Add)

	// A saved change, not yet pinned or deployed: ?revision reads it.
	rev := i.writeSpec(carol, appID, specWithEgress(map[string]any{"add": []string{"api.openai.example", "api.anthropic.example"}}))
	type revView struct {
		Revision  *int `json:"revision"`
		Effective *struct {
			Loosenings []struct {
				Entry string `json:"entry"`
			} `json:"loosenings"`
		} `json:"effective"`
	}
	var pinnedView, latest, byNumber revView
	i.do(carol, http.MethodGet, "/apps/"+appID+"/egress", nil).JSON(t, &pinnedView)
	require.Equal(t, rev-1, *pinnedView.Revision, "the pinned one by default")
	require.Len(t, pinnedView.Effective.Loosenings, 1)
	i.do(carol, http.MethodGet, "/apps/"+appID+"/egress?revision=latest", nil).JSON(t, &latest)
	require.Equal(t, rev, *latest.Revision)
	require.Len(t, latest.Effective.Loosenings, 2)
	i.do(carol, http.MethodGet, fmt.Sprintf("/apps/%s/egress?revision=%d", appID, rev), nil).JSON(t, &byNumber)
	require.Equal(t, latest, byNumber)

	got = i.do(carol, http.MethodGet, "/apps/"+appID+"/egress?revision=99", nil)
	require.Equal(t, "NOT_FOUND", got.ErrorCode(), got.String())
	got = i.do(carol, http.MethodGet, "/apps/"+appID+"/egress?revision=newest", nil)
	require.Equal(t, "VALID_INVALID", got.ErrorCode(), got.String())

	// Nothing pinned: the installation's rules, and nothing else.
	bare := i.createApp(admin, "wiki")
	got = i.do(admin, http.MethodGet, "/apps/"+bare+"/egress", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	require.Contains(t, got.String(), `"effective":null`)

	// Somebody with no grant on the app sees nothing.
	got = i.do(i.user("mallory"), http.MethodGet, "/apps/"+appID+"/egress", nil)
	require.GreaterOrEqual(t, got.Code, 400)
	require.NotEqual(t, http.StatusInternalServerError, got.Code)
}
