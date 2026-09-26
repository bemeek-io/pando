package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// The administrative functions (R-343 … R-346). Each is one request with one
// tool the model must call, because each has one answer: a draft, a filter, a
// summary, an answer. Nothing is read from a repository and there is no loop.
//
// Every value these return is checked again by core. The schemas below are
// how the model is told what shape to answer in, not what Pando accepts.

const toolSubmit = "submit"

// voice is the house style every answer here is held to (R-105), said once.
const voice = "Write for the person who asked, in plain sentences. Be specific and self-contained: " +
	"someone reading only your answer should understand it. No apology, no exclamation mark, " +
	"no \"Error:\" prefix, and do not restate the question."

// DraftAccess drafts a role and, optionally, a group (R-343).
func (a *Adapter) DraftAccess(ctx context.Context, req api.AccessRequest) (api.AccessDraft, error) {
	system := "You draft access for Pando, a self-hosted app platform, from an administrator's description. " +
		"You may draft one custom role, one group, or both. A role has a scope, install or app, and holds " +
		"verbs of that scope only — never a mix. Use only verbs from the catalog given, by exact name. " +
		"Never reuse the name of an existing role. Group members are account IDs from the list given; " +
		"leave out anyone you cannot identify and say so. If the description asks for something the " +
		"catalog cannot express, draft what it can and say what it cannot. You draft; an administrator " +
		"reviews and creates. " + voice

	user := "## Description\n\n" + req.Description +
		"\n\n## Verb catalog\n\n" + jsonBlock(req.Verbs) +
		"\n\n## Existing roles\n\n" + jsonBlock(req.Roles) +
		"\n\n## Existing groups\n\n" + jsonBlock(req.Groups) +
		"\n\n## Accounts\n\n" + jsonBlock(req.People)
	if req.Current != nil {
		user = "## The draft so far\n\nA person is refining this draft; some of it they may have changed by hand. " +
			"The description below is the change they want. Submit the whole draft again with that change made, " +
			"and keep everything else as it is.\n\n" + jsonBlock(req.Current) + "\n\n" + user
	}

	verbs := make([]string, 0, len(req.Verbs))
	for _, v := range req.Verbs {
		verbs = append(verbs, v.Name)
	}
	verbItems := map[string]any{"type": "string"}
	if len(verbs) > 0 {
		verbItems["enum"] = verbs
	}

	schema := map[string]any{
		"role": map[string]any{
			"type":        "object",
			"description": "The role to create. Omit when the description needs no new role.",
			"properties": map[string]any{
				"name":  map[string]any{"type": "string", "description": "A short name, such as \"Release manager\"."},
				"scope": map[string]any{"type": "string", "enum": []string{"install", "app"}},
				"verbs": map[string]any{"type": "array", "items": verbItems},
			},
			"required": []string{"name", "scope", "verbs"},
		},
		"group": map[string]any{
			"type":        "object",
			"description": "The group to create. Omit when the description names no group of people.",
			"properties": map[string]any{
				"name":    map[string]any{"type": "string"},
				"members": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Account IDs."},
			},
			"required": []string{"name"},
		},
		"reply": map[string]any{"type": "string", "description": "One to three sentences: what you drafted and why, and anything you could not draft."},
	}

	var out api.AccessDraft
	model := a.model(req.Model)
	if err := a.submit(ctx, model, system, user, schema, []string{"reply"}, &out); err != nil {
		return api.AccessDraft{}, err
	}
	out.Model = model
	return out, nil
}

// DraftPolicy proposes changes to host policy (R-344).
func (a *Adapter) DraftPolicy(ctx context.Context, req api.PolicyRequest) (api.PolicyDraft, error) {
	system := "You propose changes to the host policy of Pando, a self-hosted app platform, from an " +
		"administrator's description. Propose only fields from the list given, with values of the " +
		"field's type. A field marked fixed is set in Pando's startup configuration and cannot be " +
		"changed here: never propose it, and say in your reply that it is fixed. Change nothing the " +
		"description does not ask for. You propose; an administrator reviews and saves. " + voice

	user := "## Description\n\n" + req.Description +
		"\n\n## Current policy\n\n```json\n" + string(req.Current) + "\n```" +
		"\n\n## Fields\n\n" + jsonBlock(req.Fields) +
		"\n\n## Verbs, for disabled_verbs\n\n" + jsonBlock(req.Verbs)
	if len(req.Draft) > 0 {
		user += "\n\n## The draft so far\n\nA person is refining a proposal. This is the policy with the " +
			"changes they have kept so far. The description is the change they want now: propose changes " +
			"relative to this draft, and only for what the description asks.\n\n```json\n" + string(req.Draft) + "\n```"
	}

	schema := map[string]any{
		"changes": map[string]any{
			"type": "object",
			"description": "Each field to change, keyed by field name, with its new value. " +
				"An empty object when nothing should change.",
		},
		"reply": map[string]any{"type": "string", "description": "One to three sentences: what you changed and why, and anything you did not change."},
	}

	var out api.PolicyDraft
	model := a.model(req.Model)
	if err := a.submit(ctx, model, system, user, schema, []string{"changes", "reply"}, &out); err != nil {
		return api.PolicyDraft{}, err
	}
	out.Model = model
	return out, nil
}

// SearchAudit turns a question into one audit filter (R-345).
func (a *Adapter) SearchAudit(ctx context.Context, req api.AuditSearchRequest) (api.AuditSearch, error) {
	system := "You turn a question about Pando's audit log into one filter. Resolve people to account " +
		"IDs and apps to app IDs from the lists given; resolve relative times against the current time " +
		"given, in UTC, and write times in RFC 3339. Actions are names or prefixes from the list given; " +
		"any of several matches. Leave a field out rather than guess it. Pando records a successful use " +
		"of an app nowhere: it records sign-ins (session.create) and refused uses (app.use.denied). " +
		"When the question asks what someone accessed or used, filter on what is recorded and say in " +
		"the note that a successful use of an app is not recorded. " + voice

	user := "## Question\n\n" + req.Question +
		"\n\n## Current time\n\n" + req.Now.UTC().Format(time.RFC3339) +
		"\n\n## Accounts\n\n" + jsonBlock(req.People) +
		"\n\n## Apps\n\n" + jsonBlock(req.Apps) +
		"\n\n## Actions\n\n" + jsonBlock(req.Actions)

	str := map[string]any{"type": "string"}
	schema := map[string]any{
		"filter": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"actions":        map[string]any{"type": "array", "items": str},
				"app_id":         str,
				"principal_id":   str,
				"principal_kind": map[string]any{"type": "string", "enum": []string{"user", "token", "system"}},
				"target_kind":    str,
				"target_id":      str,
				"involving":      map[string]any{"type": "string", "description": "An ID that appears as the principal, the app or the target."},
				"since":          map[string]any{"type": "string", "description": "RFC 3339, UTC."},
				"until":          map[string]any{"type": "string", "description": "RFC 3339, UTC."},
			},
		},
		"note": map[string]any{"type": "string", "description": "What this filter cannot answer, if anything. Empty otherwise."},
	}

	var out api.AuditSearch
	model := a.model(req.Model)
	if err := a.submit(ctx, model, system, user, schema, []string{"filter"}, &out); err != nil {
		return api.AuditSearch{}, err
	}
	out.Model = model
	return out, nil
}

// SummarizeAudit summarizes the records core found (R-345).
func (a *Adapter) SummarizeAudit(ctx context.Context, req api.AuditSummaryRequest) (api.AuditSummary, error) {
	system := "You summarize audit records from Pando for the person who asked a question of the audit " +
		"log. Use only the records given: say nothing they do not show. Name people and apps as the " +
		"lists given name them. If the records are truncated, say the summary covers the most recent " +
		"ones. If there are none, say that nothing matched. Two to five sentences. " + voice

	user := "## Question\n\n" + req.Question +
		"\n\n## Filter\n\n" + jsonBlock(req.Filter) +
		"\n\n## Records, newest first\n\n" + jsonBlock(req.Records) +
		fmt.Sprintf("\n\nTruncated: %t", req.Truncated) +
		"\n\n## Accounts\n\n" + jsonBlock(req.People) +
		"\n\n## Apps\n\n" + jsonBlock(req.Apps)

	schema := map[string]any{"summary": map[string]any{"type": "string"}}

	var out api.AuditSummary
	model := a.model(req.Model)
	if err := a.submit(ctx, model, system, user, schema, []string{"summary"}, &out); err != nil {
		return api.AuditSummary{}, err
	}
	out.Model = model
	return out, nil
}

// AnswerReference answers from the generated reference (R-346).
func (a *Adapter) AnswerReference(ctx context.Context, req api.ReferenceRequest) (api.ReferenceAnswer, error) {
	system := "You answer \"How can I…\" questions about Pando, a self-hosted app platform, from its " +
		"generated reference: the HTTP API, the pando CLI and the MCP tools. Answer only from the " +
		"reference given. Say which endpoint, command or tool to use and how, with a short example " +
		"where it helps. Describe how to do it; you do nothing yourself. If the reference does not " +
		"cover the question, set covered to false and say so plainly rather than guess. " + voice

	user := "## Question\n\n" + req.Question + "\n\n## Reference\n\n" + req.Reference

	schema := map[string]any{
		"answer":  map[string]any{"type": "string", "description": "Markdown. Short."},
		"cites":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Each endpoint, command or tool the answer relies on, as the reference writes it."},
		"covered": map[string]any{"type": "boolean"},
	}

	var out api.ReferenceAnswer
	model := a.model(req.Model)
	if err := a.submit(ctx, model, system, user, schema, []string{"answer", "covered"}, &out); err != nil {
		return api.ReferenceAnswer{}, err
	}
	out.Model = model
	return out, nil
}

// submit makes one request whose answer is a call to the submit tool, and
// decodes that call's input into out.
func (a *Adapter) submit(ctx context.Context, model, system, user string, properties map[string]any, required []string, out any) error {
	if !a.ready {
		return errors.New("anthropic: not configured")
	}

	tool := anthropic.ToolParam{
		Name:        toolSubmit,
		Description: anthropic.String("Submit your answer. Call this exactly once."),
		InputSchema: anthropic.ToolInputSchemaParam{Properties: properties, Required: required},
	}

	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{{
			Text:         system,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Tools:      []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: anthropic.ToolChoiceParamOfTool(toolSubmit),
		Messages:   []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(user))},
	})
	if err != nil {
		return fmt.Errorf("anthropic: %w", err)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return fmt.Errorf("anthropic: the model declined to answer (%s)", resp.StopDetails.Category)
	}
	for _, block := range resp.Content {
		use, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok || use.Name != toolSubmit {
			continue
		}
		if err := json.Unmarshal([]byte(use.JSON.Input.Raw()), out); err != nil {
			return fmt.Errorf("anthropic: the answer could not be read: %w", err)
		}
		return nil
	}
	return errors.New("anthropic: the model finished without submitting an answer")
}

// jsonBlock renders v for a prompt.
func jsonBlock(v any) string {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil || string(body) == "null" {
		return "(none)"
	}
	return "```json\n" + strings.TrimSpace(string(body)) + "\n```"
}
