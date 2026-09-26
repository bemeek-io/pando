package assist

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/policy"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
)

// MaxAsk is the longest description or question accepted, in characters. A
// few sentences is what these functions are for; a page is a document.
const MaxAsk = 2000

// DefaultTimeout bounds one function's calls to its adapter.
const DefaultTimeout = 90 * time.Second

// auditSearchLimit is how many records a search reads, and summaryLimit how
// many of them go to the adapter for the summary.
const (
	auditSearchLimit = 200
	summaryLimit     = 100
)

// Service runs the administrative AI functions (R-343 … R-346).
//
// Each reads only what its caller could read through the ordinary endpoints,
// and returns a draft, a filter or an answer. Nothing here writes: creating a
// role, saving a policy, reading the audit log's pages — each is the caller's
// own request, under the caller's own authority.
type Service struct {
	Registry *api.Registry

	Users interface {
		List(ctx context.Context) ([]state.User, error)
	}
	Apps interface {
		ListAll(ctx context.Context) ([]state.App, error)
	}
	Roles interface {
		List(ctx context.Context) ([]state.RoleRow, error)
	}
	Groups interface {
		List(ctx context.Context) ([]state.Group, error)
	}

	// Verbs is what a principal holds install-wide, which bounds what an
	// access draft may grant.
	Verbs interface {
		InstallVerbsFor(ctx context.Context, p authz.Principal) ([]string, error)
	}

	// Policy reads the effective policy document, startup fields included;
	// Overlay says which fields those are and where each was set.
	Policy interface {
		Load(ctx context.Context) (policy.Document, error)
	}
	Overlay *policy.Overlay

	Audit interface {
		List(ctx context.Context, q audit.Query) ([]audit.Record, error)
		ActionNames(ctx context.Context) ([]string, error)
	}

	// Reference is the generated reference in Markdown.
	Reference func() string

	// Now is the clock. Nil is the system clock.
	Now func() time.Time

	Timeout time.Duration
}

// Ran is which adapter and model answered, for the reply and the audit event
// recording that data left the host.
type Ran struct {
	AdapterID string `json:"adapter_id"`
	Model     string `json:"model"`
}

// call resolves fn to its adapter and a bounded context.
func (s *Service) call(ctx context.Context, fn api.AIFunction) (api.AIAdapter, string, Ran, context.Context, context.CancelFunc, error) {
	ai, a, ok := s.Registry.AIFor(fn)
	if !ok {
		msg := fmt.Sprintf("%s is not assigned to an AI adapter on this installation.", fn.Title())
		if a.AdapterRef != "" {
			msg = fmt.Sprintf("%s is assigned to the adapter %s, which is not running.", fn.Title(), a.AdapterRef)
		}
		return nil, "", Ran{}, nil, nil, errs.New(errs.AdapterUnavailable, msg).
			WithRemedy(fmt.Sprintf("An administrator can assign it under Adapters in the console, or with PUT /api/v1/ai/functions/%s.", fn))
	}
	caps, err := ai.Capabilities(ctx)
	if err != nil {
		return nil, "", Ran{}, nil, nil, errs.Newf(errs.AdapterUnavailable, "Pando could not reach the adapter %s.", a.AdapterRef)
	}
	if !caps.Does(fn) {
		return nil, "", Ran{}, nil, nil, errs.Newf(errs.AdapterUnavailable,
			"%s is assigned to the adapter %s, which no longer performs it.", fn.Title(), a.AdapterRef).
			WithRemedy(fmt.Sprintf("Assign it to another adapter with PUT /api/v1/ai/functions/%s.", fn))
	}
	model := a.Model
	if !caps.ChoosesModel {
		model = ""
	}
	ran := Ran{AdapterID: a.AdapterRef, Model: model}
	if ran.Model == "" {
		ran.Model = caps.Model
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	return ai, model, ran, callCtx, cancel, nil
}

func failed(fn api.AIFunction, err error) error {
	return errs.Newf(errs.AdapterFailed, "The AI adapter could not finish %s: %s", strings.ToLower(fn.Title()), err.Error()).
		WithRemedy("Try again in a moment, or do this without AI.")
}

func ask(what, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errs.Newf(errs.ValidInvalid, "Say %s.", what)
	}
	if utf8.RuneCountInString(text) > MaxAsk {
		return "", errs.Newf(errs.ValidInvalid, "That is longer than %d characters. Say it in a few sentences.", MaxAsk)
	}
	return text, nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// ---------------------------------------------------------------------------
// Access (R-343)

// AccessResult is a checked access draft.
type AccessResult struct {
	Role  *api.RoleDraft  `json:"role,omitempty"`
	Group *api.GroupDraft `json:"group,omitempty"`
	Reply string          `json:"reply"`

	// Refused is what the adapter proposed and Pando would not draft, each
	// with its reason.
	Refused []string `json:"refused,omitempty"`
	Ran
}

// DraftAccess drafts a role and a group from a description.
//
// The verb catalog handed over is what the caller could grant: every app
// verb, which POST /roles lets them put in an app role, and only the install
// verbs they hold themselves. A draft is refused any verb outside it, a mix of
// scopes (R-080), and the name of a role that exists, built-in or not (R-082).
func (s *Service) DraftAccess(ctx context.Context, p authz.Principal, description string, current *api.AccessDraft) (AccessResult, error) {
	description, err := ask("who should be able to do what", description)
	if err != nil {
		return AccessResult{}, err
	}

	held := map[string]bool{}
	if s.Verbs != nil {
		verbs, err := s.Verbs.InstallVerbsFor(ctx, p)
		if err != nil {
			return AccessResult{}, err
		}
		for _, v := range verbs {
			held[v] = true
		}
	}
	catalog := map[string]string{}
	var verbs []api.VerbInfo
	for _, v := range authz.Verbs {
		scope := "app"
		if authz.InstallScoped(v) {
			scope = "install"
			if !held[string(v)] {
				continue
			}
		}
		catalog[string(v)] = scope
		verbs = append(verbs, api.VerbInfo{Name: string(v), Scope: scope})
	}

	roleRows, err := s.Roles.List(ctx)
	if err != nil {
		return AccessResult{}, err
	}
	roles := make([]api.RoleInfo, 0, len(roleRows))
	roleNames := map[string]bool{}
	for _, r := range roleRows {
		roles = append(roles, api.RoleInfo{ID: r.ID, Name: r.Name, Scope: r.Scope, Builtin: r.Builtin, Verbs: r.Verbs})
		roleNames[strings.ToLower(r.Name)] = true
	}
	groupRows, err := s.Groups.List(ctx)
	if err != nil {
		return AccessResult{}, err
	}
	groups := make([]api.GroupInfo, 0, len(groupRows))
	groupNames := map[string]bool{}
	for _, g := range groupRows {
		groups = append(groups, api.GroupInfo{ID: g.ID, Name: g.Name})
		groupNames[strings.ToLower(g.Name)] = true
	}
	people, err := s.people(ctx)
	if err != nil {
		return AccessResult{}, err
	}
	known := map[string]bool{}
	for _, person := range people {
		known[person.ID] = true
	}

	ai, model, ran, callCtx, cancel, err := s.call(ctx, api.AIFunctionDraftAccess)
	if err != nil {
		return AccessResult{}, err
	}
	defer cancel()
	draft, err := ai.DraftAccess(callCtx, api.AccessRequest{
		Description: description, Verbs: verbs, Roles: roles, Groups: groups, People: people,
		Current: current, Model: model,
	})
	if err != nil {
		return AccessResult{}, failed(api.AIFunctionDraftAccess, err)
	}
	if draft.Model != "" {
		ran.Model = draft.Model
	}

	out := AccessResult{Reply: strings.TrimSpace(draft.Reply), Ran: ran}
	if r := draft.Role; r != nil {
		role, refused := checkRole(*r, catalog, roleNames)
		out.Refused = append(out.Refused, refused...)
		out.Role = role
	}
	if g := draft.Group; g != nil {
		name := strings.TrimSpace(g.Name)
		switch {
		case name == "":
			out.Refused = append(out.Refused, "The group had no name, so it was left out.")
		case groupNames[strings.ToLower(name)]:
			out.Refused = append(out.Refused, fmt.Sprintf("A group called %q already exists, so no new one was drafted. Add people to it instead.", name))
		default:
			group := &api.GroupDraft{Name: name}
			seen := map[string]bool{}
			for _, m := range g.Members {
				m = strings.TrimSpace(m)
				if seen[m] {
					continue
				}
				seen[m] = true
				if !known[m] {
					out.Refused = append(out.Refused, fmt.Sprintf("%q is not an account on this installation, so it was left out of the group.", m))
					continue
				}
				group.Members = append(group.Members, m)
			}
			out.Group = group
		}
	}
	return out, nil
}

func checkRole(r api.RoleDraft, catalog map[string]string, existing map[string]bool) (*api.RoleDraft, []string) {
	var refused []string
	name := strings.TrimSpace(r.Name)
	if name == "" {
		return nil, []string{"The role had no name, so it was left out."}
	}
	if existing[strings.ToLower(name)] {
		return nil, []string{fmt.Sprintf("A role called %q already exists, so it was not drafted again. Grant that role instead, or ask for a different name.", name)}
	}
	if r.Scope != "install" && r.Scope != "app" {
		return nil, []string{fmt.Sprintf("The role %q had the scope %q, which is not install or app, so it was left out.", name, r.Scope)}
	}
	role := &api.RoleDraft{Name: name, Scope: r.Scope}
	seen := map[string]bool{}
	for _, v := range r.Verbs {
		if seen[v] {
			continue
		}
		seen[v] = true
		scope, ok := catalog[v]
		switch {
		case !ok:
			refused = append(refused, fmt.Sprintf("%s is not a permission you can grant, so it was left out of %q.", v, name))
		case scope != r.Scope:
			// R-080: a role holds verbs of one scope.
			refused = append(refused, fmt.Sprintf("%s is an %s permission and %q is an %s role, so it was left out. A role holds permissions of one scope.", v, scope, name, r.Scope))
		default:
			role.Verbs = append(role.Verbs, v)
		}
	}
	if len(role.Verbs) == 0 {
		return nil, append(refused, fmt.Sprintf("The role %q had no permission Pando could draft, so it was left out.", name))
	}
	sort.Strings(role.Verbs)
	return role, refused
}

// ---------------------------------------------------------------------------
// Policy (R-344)

// PolicyChange is one field a policy draft changes.
type PolicyChange struct {
	Key  string          `json:"key"`
	From json.RawMessage `json:"from"`
	To   json.RawMessage `json:"to"`
}

// PolicyDeclined is a change Pando declined because the startup configuration
// fixes the field (R-271).
type PolicyDeclined struct {
	Key    string        `json:"key"`
	Reason string        `json:"reason"`
	Source policy.Source `json:"source"`
}

// PolicyResult is a checked policy draft: the document as it would be saved,
// and what changed.
type PolicyResult struct {
	Proposed policy.Document  `json:"proposed"`
	Changes  []PolicyChange   `json:"changes"`
	Declined []PolicyDeclined `json:"declined,omitempty"`
	Refused  []string         `json:"refused,omitempty"`
	Reply    string           `json:"reply"`
	Ran
}

// DraftPolicy proposes a policy from a description.
//
// A field the startup configuration fixes is declined here, citing where it
// was set, whatever the adapter returned: the refusal does not depend on the
// model's cooperation.
//
// proposed, when set, is the document so far while a person refines a
// proposal. The model changes it, and the changes reported are relative to
// the stored policy, so what the person kept earlier is still listed. Fields
// the startup configuration fixes are put back whatever proposed says.
func (s *Service) DraftPolicy(ctx context.Context, description string, proposed *policy.Document) (PolicyResult, error) {
	description, err := ask("what the policy should be", description)
	if err != nil {
		return PolicyResult{}, err
	}
	current, err := s.Policy.Load(ctx)
	if err != nil {
		return PolicyResult{}, err
	}
	currentRaw, _ := json.Marshal(current)
	fixed := map[string]policy.Fixed{}
	for _, f := range s.Overlay.Fixed() {
		fixed[f.Key] = f
	}
	var fields []api.PolicyField
	for _, key := range policy.Fields() {
		t, _ := policy.FieldType(key)
		_, isFixed := fixed[key]
		fields = append(fields, api.PolicyField{Key: key, Type: t, Meaning: policy.Describe(key), Fixed: isFixed})
	}
	var verbs []api.VerbInfo
	for _, v := range authz.Verbs {
		scope := "app"
		if authz.InstallScoped(v) {
			scope = "install"
		}
		verbs = append(verbs, api.VerbInfo{Name: string(v), Scope: scope})
	}

	ai, model, ran, callCtx, cancel, err := s.call(ctx, api.AIFunctionDraftPolicy)
	if err != nil {
		return PolicyResult{}, err
	}
	defer cancel()
	// The base the model's changes land on: the stored policy, or the draft
	// so far with the startup fields laid back over it.
	baseRaw := currentRaw
	var draftRaw json.RawMessage
	if proposed != nil {
		baseRaw, _ = json.Marshal(s.Overlay.Apply(*proposed))
		draftRaw = baseRaw
	}
	draft, err := ai.DraftPolicy(callCtx, api.PolicyRequest{
		Description: description, Current: currentRaw, Draft: draftRaw, Fields: fields, Verbs: verbs, Model: model,
	})
	if err != nil {
		return PolicyResult{}, failed(api.AIFunctionDraftPolicy, err)
	}
	if draft.Model != "" {
		ran.Model = draft.Model
	}

	stored := map[string]json.RawMessage{}
	_ = json.Unmarshal(currentRaw, &stored)
	doc := map[string]json.RawMessage{}
	_ = json.Unmarshal(baseRaw, &doc)
	out := PolicyResult{Reply: strings.TrimSpace(draft.Reply), Changes: []PolicyChange{}, Ran: ran}

	keys := make([]string, 0, len(draft.Changes))
	for k := range draft.Changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if f, isFixed := fixed[key]; isFixed {
			out.Declined = append(out.Declined, PolicyDeclined{Key: key, Source: f.Source, Reason: fixedReason(f)})
			continue
		}
		value, err := policy.CheckValue(key, draft.Changes[key])
		if err != nil {
			out.Refused = append(out.Refused, fmt.Sprintf("%s was left unchanged: %s.", key, err.Error()))
			continue
		}
		if reason := badVerbs(key, value); reason != "" {
			out.Refused = append(out.Refused, reason)
			continue
		}
		doc[key] = value
	}

	// Every field that differs from the stored policy, whichever call changed
	// it, so a refined proposal still lists what was kept from before.
	for _, key := range policy.Fields() {
		if !sameValue(stored[key], doc[key]) {
			out.Changes = append(out.Changes, PolicyChange{Key: key, From: orNull(stored[key]), To: orNull(doc[key])})
		}
	}

	raw, _ := json.Marshal(doc)
	if err := json.Unmarshal(raw, &out.Proposed); err != nil {
		return PolicyResult{}, errs.Wrap(errs.Internal, "The proposed policy could not be assembled.", err)
	}
	return out, nil
}

// fixedReason is the R-105 sentence for a field the startup config sets.
func fixedReason(f policy.Fixed) string {
	switch f.Source.Kind {
	case "env":
		return fmt.Sprintf("%s is set by the environment variable %s and can't be changed here. "+
			"Unset it and restart Pando to manage it from the console.", f.Key, f.Source.Name)
	case "file":
		return fmt.Sprintf("%s is set in %s (%s) and can't be changed here. "+
			"Remove it from that file and restart Pando to manage it from the console.", f.Key, f.Source.Name, f.Source.Key)
	}
	return fmt.Sprintf("%s is set in Pando's startup configuration and can't be changed here.", f.Key)
}

// badVerbs refuses a verb list naming something that is not a verb: policy
// only denies (R-272), so a typo would deny nothing and look like a rule.
func badVerbs(key string, value json.RawMessage) string {
	if key != "disabled_verbs" && key != "agent_disabled_verbs" {
		return ""
	}
	var list []string
	_ = json.Unmarshal(value, &list)
	for _, v := range list {
		if !authz.IsVerb(authz.Verb(v)) {
			return fmt.Sprintf("%s was left unchanged: %q is not a permission Pando has.", key, v)
		}
	}
	return ""
}

func sameValue(a, b json.RawMessage) bool {
	var x, y any
	_ = json.Unmarshal(orNull(a), &x)
	_ = json.Unmarshal(orNull(b), &y)
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}

func orNull(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return json.RawMessage("null")
	}
	return r
}

// ---------------------------------------------------------------------------
// Audit search (R-345)

// AuditResult is a question turned into a filter, the filter's results
// counted, and a summary of them.
type AuditResult struct {
	Filter    api.AuditFilter `json:"filter"`
	Note      string          `json:"note,omitempty"`
	Summary   string          `json:"summary"`
	Matched   int             `json:"matched"`
	Truncated bool            `json:"truncated"`
	Ran
}

// SearchAudit answers a question about the audit log. The caller must be
// able to read the log (install.audit.read); the adapter sees the records
// that caller could see and nothing more, and never queries the log itself
// (R-027): it returns a filter, and core runs it.
func (s *Service) SearchAudit(ctx context.Context, question string) (AuditResult, error) {
	question, err := ask("what you want to find in the audit log", question)
	if err != nil {
		return AuditResult{}, err
	}
	people, err := s.people(ctx)
	if err != nil {
		return AuditResult{}, err
	}
	apps, err := s.apps(ctx)
	if err != nil {
		return AuditResult{}, err
	}
	actions, err := s.Audit.ActionNames(ctx)
	if err != nil {
		return AuditResult{}, err
	}

	ai, model, ran, callCtx, cancel, err := s.call(ctx, api.AIFunctionSearchAudit)
	if err != nil {
		return AuditResult{}, err
	}
	defer cancel()
	search, err := ai.SearchAudit(callCtx, api.AuditSearchRequest{
		Question: question, Now: s.now(), People: people, Apps: apps, Actions: actions, Model: model,
	})
	if err != nil {
		return AuditResult{}, failed(api.AIFunctionSearchAudit, err)
	}
	if search.Model != "" {
		ran.Model = search.Model
	}

	f, err := cleanFilter(search.Filter)
	if err != nil {
		return AuditResult{}, err
	}
	q := audit.Query{
		Actions: f.Actions, AppID: f.AppID, PrincipalID: f.PrincipalID, PrincipalKind: f.PrincipalKind,
		TargetKind: f.TargetKind, TargetID: f.TargetID, Involving: f.Involving, Limit: auditSearchLimit,
	}
	if f.Since != nil {
		q.Since = *f.Since
	}
	if f.Until != nil {
		q.Until = *f.Until
	}
	records, err := s.Audit.List(ctx, q)
	if err != nil {
		return AuditResult{}, err
	}

	out := AuditResult{Filter: f, Note: strings.TrimSpace(search.Note), Matched: len(records),
		Truncated: len(records) >= auditSearchLimit, Ran: ran}
	views := make([]api.AuditRecordView, 0, min(len(records), summaryLimit))
	for i, r := range records {
		if i >= summaryLimit {
			break
		}
		views = append(views, api.AuditRecordView{
			At: r.OccurredAt.UTC(), Action: r.Action, PrincipalKind: r.PrincipalKind, PrincipalID: r.PrincipalID,
			AppID: r.AppID, TargetKind: r.TargetKind, TargetID: r.TargetID, Detail: r.Detail,
		})
	}

	summary, err := ai.SummarizeAudit(callCtx, api.AuditSummaryRequest{
		Question: question, Filter: f, Records: views, Truncated: len(records) > len(views),
		People: people, Apps: apps, Model: model,
	})
	if err != nil {
		return AuditResult{}, failed(api.AIFunctionSearchAudit, err)
	}
	out.Summary = strings.TrimSpace(summary.Summary)
	return out, nil
}

// cleanFilter trims a filter and refuses one that would not read as a query.
func cleanFilter(f api.AuditFilter) (api.AuditFilter, error) {
	out := api.AuditFilter{
		AppID: strings.TrimSpace(f.AppID), PrincipalID: strings.TrimSpace(f.PrincipalID),
		PrincipalKind: strings.TrimSpace(f.PrincipalKind), TargetKind: strings.TrimSpace(f.TargetKind),
		TargetID: strings.TrimSpace(f.TargetID), Involving: strings.TrimSpace(f.Involving),
		Since: f.Since, Until: f.Until,
	}
	for _, a := range f.Actions {
		if a = strings.TrimSpace(a); a != "" && len(out.Actions) < 10 {
			out.Actions = append(out.Actions, a)
		}
	}
	switch out.PrincipalKind {
	case "", "user", "token", "system", "anonymous":
	default:
		out.PrincipalKind = ""
	}
	if out.Since != nil && out.Until != nil && !out.Until.After(*out.Since) {
		return api.AuditFilter{}, errs.New(errs.AdapterFailed,
			"The AI adapter produced a time range that ends before it starts, so Pando did not search with it.").
			WithRemedy("Ask again with the dates spelled out, or set the filters on the audit log yourself.")
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Reference (R-346)

// ReferenceResult is an answer from the reference.
type ReferenceResult struct {
	Answer  string   `json:"answer"`
	Cites   []string `json:"cites"`
	Covered bool     `json:"covered"`
	Ran
}

// AnswerReference answers "How can I…" from the generated reference. It
// describes; nothing here acts. A citation that is not in the reference is
// dropped, the cheapest check on whether the answer came from the reference
// or from memory.
func (s *Service) AnswerReference(ctx context.Context, question string) (ReferenceResult, error) {
	question, err := ask("what you want to do", question)
	if err != nil {
		return ReferenceResult{}, err
	}
	ref := ""
	if s.Reference != nil {
		ref = s.Reference()
	}

	ai, model, ran, callCtx, cancel, err := s.call(ctx, api.AIFunctionAnswerReference)
	if err != nil {
		return ReferenceResult{}, err
	}
	defer cancel()
	answer, err := ai.AnswerReference(callCtx, api.ReferenceRequest{Question: question, Reference: ref, Model: model})
	if err != nil {
		return ReferenceResult{}, failed(api.AIFunctionAnswerReference, err)
	}
	if answer.Model != "" {
		ran.Model = answer.Model
	}

	out := ReferenceResult{Answer: strings.TrimSpace(answer.Answer), Covered: answer.Covered, Cites: []string{}, Ran: ran}
	for _, c := range answer.Cites {
		if c = strings.TrimSpace(c); c != "" && strings.Contains(ref, c) {
			out.Cites = append(out.Cites, c)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------

func (s *Service) people(ctx context.Context) ([]api.PersonInfo, error) {
	users, err := s.Users.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.PersonInfo, 0, len(users))
	for _, u := range users {
		out = append(out, api.PersonInfo{ID: u.ID, Name: u.DisplayName, Email: u.Email})
	}
	return out, nil
}

func (s *Service) apps(ctx context.Context) ([]api.AppInfo, error) {
	apps, err := s.Apps.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.AppInfo, 0, len(apps))
	for _, a := range apps {
		out = append(out, api.AppInfo{ID: a.ID, Name: a.Name})
	}
	return out, nil
}
