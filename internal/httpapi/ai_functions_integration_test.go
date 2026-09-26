//go:build integration

package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
	corepolicy "github.com/bemeek-io/pando/internal/core/policy"
	"github.com/bemeek-io/pando/internal/errs"
)

func assignAll(t *testing.T, i *install, s *session, ref string) {
	t.Helper()
	for _, fn := range adapterapi.AIFunctions() {
		got := i.do(s, http.MethodPut, "/ai/functions/"+string(fn), map[string]any{"adapter_id": ref})
		require.Equal(t, http.StatusOK, got.Code, got.String())
	}
}

func auditActions(t *testing.T, i *install, s *session, action string) []map[string]any {
	t.Helper()
	got := i.do(s, http.MethodGet, "/audit?action="+action, nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var body struct {
		Events []map[string]any `json:"events"`
	}
	got.JSON(t, &body)
	return body.Events
}

// TestR343_AccessDraftProposesFromTheCatalogOnly asserts R-343, R-080 and
// R-082: a drafted role holds only catalog verbs of its own scope, never a
// built-in's name, and nothing is created until a person creates it.
func TestR343_AccessDraftProposesFromTheCatalogOnly(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	ai := withAI(t, i, "ai_anthropic", everything("anthropic"))
	assignAll(t, i, admin, "ai_anthropic")

	ai.access = adapterapi.AccessDraft{
		Role: &adapterapi.RoleDraft{Name: "Release manager", Scope: "app",
			Verbs: []string{"app.deploy", "app.restart", "install.policy.manage", "app.teleport"}},
		Group: &adapterapi.GroupDraft{Name: "Release managers", Members: []string{i.AdminID, "usr_nobody"}},
		Reply: "Drafted a release manager role and a group for it.",
	}
	got := i.do(admin, http.MethodPost, "/ai/access/draft", map[string]any{"description": "Release managers deploy and restart apps"})
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var draft struct {
		Role    adapterapi.RoleDraft  `json:"role"`
		Group   adapterapi.GroupDraft `json:"group"`
		Refused []string              `json:"refused"`
		Model   string                `json:"model"`
	}
	got.JSON(t, &draft)
	require.Equal(t, []string{"app.deploy", "app.restart"}, draft.Role.Verbs)
	require.Equal(t, []string{i.AdminID}, draft.Group.Members)
	require.Len(t, draft.Refused, 3, got.String())
	require.Equal(t, "anthropic-default", draft.Model)

	// The model was handed the catalog, including the install verbs the
	// administrator holds.
	var names []string
	for _, v := range ai.gotAccess.Verbs {
		names = append(names, v.Name)
	}
	require.Contains(t, names, "install.users.manage")
	require.Contains(t, names, "app.exec")

	// Drafted, not created.
	roles := i.do(admin, http.MethodGet, "/roles?scope=all", nil)
	require.NotContains(t, roles.String(), "Release manager")

	// A built-in's name is refused.
	ai.access = adapterapi.AccessDraft{Role: &adapterapi.RoleDraft{Name: "owner", Scope: "app", Verbs: []string{"app.view"}}}
	got = i.do(admin, http.MethodPost, "/ai/access/draft", map[string]any{"description": "owners"})
	require.Equal(t, http.StatusOK, got.Code, got.String())
	require.NotContains(t, got.String(), `"role"`)
	require.Contains(t, got.String(), "already exists")

	// Recorded as sent, with the adapter and model; never the content.
	events := auditActions(t, i, admin, "ai.draft_access")
	require.Len(t, events, 2)
	require.NotContains(t, events[0]["detail"], "description")

	// Behind the verb that creates roles.
	someone := i.user("someone")
	require.Equal(t, http.StatusForbidden,
		i.do(someone, http.MethodPost, "/ai/access/draft", map[string]any{"description": "x"}).Code)
}

// TestR271_AIPolicyDraftRefusesConfigFixedField asserts R-271 and R-344: a
// policy draft that changes a field the config file fixes is declined by
// core, citing the file and key, whatever the adapter proposed.
func TestR271_AIPolicyDraftRefusesConfigFixedField(t *testing.T) {
	overlay, err := corepolicy.NewOverlay([]corepolicy.Setting{{
		Key: "disabled_verbs", Value: []any{"app.exec"},
		Source: corepolicy.Source{Kind: "file", Name: "/etc/pando/pando.yaml", Key: "policy.disabled_verbs"},
	}})
	require.NoError(t, err)
	i := newInstallWith(t, overlay, nil)
	admin := i.admin()
	ai := withAI(t, i, "ai_anthropic", everything("anthropic"))
	assignAll(t, i, admin, "ai_anthropic")

	ai.policy = adapterapi.PolicyDraft{
		Changes: map[string]json.RawMessage{
			"disabled_verbs":         json.RawMessage(`[]`),
			"allow_anonymous_grants": json.RawMessage(`false`),
			"no_such_field":          json.RawMessage(`1`),
		},
		Reply: "Turned off anonymous sharing and re-enabled exec.",
	}
	got := i.do(admin, http.MethodPost, "/ai/policy/draft", map[string]any{"description": "No anonymous sharing, and allow exec"})
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var draft struct {
		Proposed corepolicy.Document `json:"proposed"`
		Changes  []struct {
			Key string `json:"key"`
		} `json:"changes"`
		Declined []struct {
			Key    string `json:"key"`
			Reason string `json:"reason"`
		} `json:"declined"`
		Refused []string `json:"refused"`
	}
	got.JSON(t, &draft)

	require.Len(t, draft.Declined, 1)
	require.Equal(t, "disabled_verbs", draft.Declined[0].Key)
	require.Contains(t, draft.Declined[0].Reason, "/etc/pando/pando.yaml (policy.disabled_verbs)")
	require.Contains(t, draft.Declined[0].Reason, "can't be changed here")
	require.Equal(t, []string{"app.exec"}, draft.Proposed.DisabledVerbs, "the fixed value stays")

	require.Len(t, draft.Changes, 1)
	require.Equal(t, "allow_anonymous_grants", draft.Changes[0].Key)
	require.NotNil(t, draft.Proposed.AllowAnonymousGrants)
	require.False(t, *draft.Proposed.AllowAnonymousGrants)
	require.Len(t, draft.Refused, 1)

	// The model was told which fields are fixed.
	for _, f := range ai.gotPolicy.Fields {
		require.Equal(t, f.Key == "disabled_verbs", f.Fixed, f.Key)
	}

	// Proposed, not saved; and what was proposed saves as it stands.
	current := i.do(admin, http.MethodGet, "/policy", nil)
	require.NotContains(t, current.String(), `"allow_anonymous_grants":false`)
	saved := i.do(admin, http.MethodPut, "/policy", draft.Proposed)
	require.Equal(t, http.StatusOK, saved.Code, saved.String())
}

// TestR344_PolicyDraftRefusesWhatWouldNotReadAndSavesNothing asserts R-344:
// a verb list naming a verb Pando does not have, or a value of the wrong type,
// is refused rather than proposed, and asking changes nothing that is stored.
func TestR344_PolicyDraftRefusesWhatWouldNotReadAndSavesNothing(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	ai := withAI(t, i, "ai_anthropic", everything("anthropic"))
	assignAll(t, i, admin, "ai_anthropic")
	before := i.do(admin, http.MethodGet, "/policy", nil).String()

	ai.policy = adapterapi.PolicyDraft{Changes: map[string]json.RawMessage{
		"disabled_verbs":         json.RawMessage(`["app.teleport"]`),
		"allow_anonymous_grants": json.RawMessage(`"sometimes"`),
	}}
	got := i.do(admin, http.MethodPost, "/ai/policy/draft", map[string]any{"description": "lock it down"})
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var draft struct {
		Changes []any    `json:"changes"`
		Refused []string `json:"refused"`
	}
	got.JSON(t, &draft)
	require.Empty(t, draft.Changes)
	require.Len(t, draft.Refused, 2, got.String())
	require.Contains(t, got.String(), `\"app.teleport\" is not a permission Pando has`)

	require.Equal(t, before, i.do(admin, http.MethodGet, "/policy", nil).String(), "nothing was saved")
	someone := i.user("someone")
	require.Equal(t, http.StatusForbidden,
		i.do(someone, http.MethodPost, "/ai/policy/draft", map[string]any{"description": "x"}).Code)
}

// TestR345_AuditSearchRunsTheFilterInCore asserts R-345 and R-027: the
// adapter returns a filter, core runs it with the caller's authority, and
// the summary is written from the records core found.
func TestR345_AuditSearchRunsTheFilterInCore(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	ai := withAI(t, i, "ai_anthropic", everything("anthropic"))
	assignAll(t, i, admin, "ai_anthropic")
	i.createApp(admin, "notes")
	i.createApp(admin, "wiki")

	ai.search = adapterapi.AuditSearch{
		Filter: adapterapi.AuditFilter{Actions: []string{"app.create", "app.delete"}, PrincipalID: i.AdminID},
		Note:   "A successful use of an app is not recorded.",
	}
	ai.summary = adapterapi.AuditSummary{Summary: "The administrator created two apps."}

	got := i.do(admin, http.MethodPost, "/ai/audit/search", map[string]any{"question": "Which apps did the admin add or delete?"})
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var out struct {
		Filter  adapterapi.AuditFilter `json:"filter"`
		Summary string                 `json:"summary"`
		Note    string                 `json:"note"`
		Matched int                    `json:"matched"`
	}
	got.JSON(t, &out)
	require.Equal(t, 2, out.Matched)
	require.Equal(t, "The administrator created two apps.", out.Summary)
	require.Equal(t, []string{"app.create", "app.delete"}, out.Filter.Actions)
	require.NotEmpty(t, out.Note)

	// The summary saw exactly the records the filter found.
	require.Len(t, ai.gotSummary.Records, 2)
	for _, r := range ai.gotSummary.Records {
		require.Equal(t, "app.create", r.Action)
	}
	require.Contains(t, ai.gotSearch.Actions, "app.create", "the actions the log holds")
	require.False(t, ai.gotSearch.Now.IsZero())

	// The same filter, as ordinary parameters, reads the same records.
	plain := i.do(admin, http.MethodGet, "/audit?action=app.create&action=app.delete&principal_id="+i.AdminID, nil)
	require.Equal(t, http.StatusOK, plain.Code, plain.String())
	var page struct {
		Events []map[string]any `json:"events"`
	}
	plain.JSON(t, &page)
	require.Len(t, page.Events, 2)

	// Only for someone who can read the log.
	someone := i.user("someone")
	require.Equal(t, http.StatusForbidden,
		i.do(someone, http.MethodPost, "/ai/audit/search", map[string]any{"question": "x"}).Code)
}

// TestR346_ReferenceAnswersCiteOnlyTheReference asserts R-346: the answer
// comes from the generated reference, and a citation that is not in it is
// dropped.
func TestR346_ReferenceAnswersCiteOnlyTheReference(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	ai := withAI(t, i, "ai_anthropic", everything("anthropic"))
	assignAll(t, i, admin, "ai_anthropic")

	ai.reference = adapterapi.ReferenceAnswer{
		Answer:  "Create a group with `POST /api/v1/groups`.",
		Cites:   []string{"POST /api/v1/groups", "POST /api/v1/teleport"},
		Covered: true,
	}
	someone := i.user("someone")
	got := i.do(someone, http.MethodPost, "/ai/reference/answer", map[string]any{"question": "How can I make a group?"})
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var out struct {
		Answer  string   `json:"answer"`
		Cites   []string `json:"cites"`
		Covered bool     `json:"covered"`
	}
	got.JSON(t, &out)
	require.Equal(t, []string{"POST /api/v1/groups"}, out.Cites)
	require.True(t, out.Covered)
	require.Contains(t, ai.gotReference.Reference, "/api/v1/groups")
	require.Contains(t, ai.gotReference.Reference, "pando_list_audit", "the MCP tools are in it too")

	require.Equal(t, http.StatusUnauthorized,
		i.anon(http.MethodPost, "/ai/reference/answer", map[string]any{"question": "x"}).Code)
}

// TestR106_AnUnassignedAIFunctionSaysHowToTurnItOn asserts R-106: with no
// adapter assigned, each function answers with what to do instead, not a
// dead end.
func TestR106_AnUnassignedAIFunctionSaysHowToTurnItOn(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	got := i.do(admin, http.MethodPost, "/ai/reference/answer", map[string]any{"question": "How can I make a group?"})
	require.Equal(t, http.StatusBadGateway, got.Code, got.String())
	require.Equal(t, errs.AdapterUnavailable, errs.Code(got.ErrorCode()))
	require.Contains(t, got.String(), "Reference help is not assigned")
	require.Contains(t, got.String(), "PUT /api/v1/ai/functions/answer_reference")
}
