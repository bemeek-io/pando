package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/bemeek-io/pando/internal/adapter/ai/aikit"
	"github.com/bemeek-io/pando/internal/adapter/api"
)

// The administrative functions (R-343 … R-346): the tasks are aikit's, shared
// by every AI adapter; what is Anthropic's is how one is sent.

// DraftAccess drafts a role and, optionally, a group (R-343).
func (a *Adapter) DraftAccess(ctx context.Context, req api.AccessRequest) (api.AccessDraft, error) {
	var out api.AccessDraft
	model := a.model(req.Model)
	if err := a.submit(ctx, model, aikit.AccessTask(req), &out); err != nil {
		return api.AccessDraft{}, err
	}
	out.Model = model
	return out, nil
}

// DraftPolicy proposes changes to host policy (R-344).
func (a *Adapter) DraftPolicy(ctx context.Context, req api.PolicyRequest) (api.PolicyDraft, error) {
	var out api.PolicyDraft
	model := a.model(req.Model)
	if err := a.submit(ctx, model, aikit.PolicyTask(req), &out); err != nil {
		return api.PolicyDraft{}, err
	}
	out.Model = model
	return out, nil
}

// SearchAudit turns a question into one audit filter (R-345).
func (a *Adapter) SearchAudit(ctx context.Context, req api.AuditSearchRequest) (api.AuditSearch, error) {
	var out api.AuditSearch
	model := a.model(req.Model)
	if err := a.submit(ctx, model, aikit.AuditSearchTask(req), &out); err != nil {
		return api.AuditSearch{}, err
	}
	out.Model = model
	return out, nil
}

// SummarizeAudit summarizes the records core found (R-345).
func (a *Adapter) SummarizeAudit(ctx context.Context, req api.AuditSummaryRequest) (api.AuditSummary, error) {
	var out api.AuditSummary
	model := a.model(req.Model)
	if err := a.submit(ctx, model, aikit.AuditSummaryTask(req), &out); err != nil {
		return api.AuditSummary{}, err
	}
	out.Model = model
	return out, nil
}

// AnswerReference answers from the generated reference (R-346).
func (a *Adapter) AnswerReference(ctx context.Context, req api.ReferenceRequest) (api.ReferenceAnswer, error) {
	var out api.ReferenceAnswer
	model := a.model(req.Model)
	if err := a.submit(ctx, model, aikit.ReferenceTask(req), &out); err != nil {
		return api.ReferenceAnswer{}, err
	}
	out.Model = model
	return out, nil
}

// submit asks for an answer that is a call to the submit tool, and decodes
// that call's input into out.
//
// The call is asked for, not forced. Forced tool use (tool_choice "tool" or
// "any") is refused with a 400 by current models, Opus 5.5 among them, so
// tool_choice is auto with parallel calls off, and the system prompt says to
// answer through the tool. A model that answers in prose instead is asked once
// more, in the same conversation, before that counts as a failure.
func (a *Adapter) submit(ctx context.Context, model string, task aikit.Task, out any) error {
	if !a.ready {
		return errors.New("anthropic: not configured")
	}

	tool := anthropic.ToolParam{
		Name:        task.Tool.Name,
		Description: anthropic.String(task.Tool.Description),
		InputSchema: anthropic.ToolInputSchemaParam{Properties: task.Tool.Properties, Required: task.Tool.Required},
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{{
			Text:         task.System + aikit.AnswerThroughTool,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Tools: []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfAuto: &anthropic.ToolChoiceAutoParam{DisableParallelToolUse: anthropic.Bool(true)},
		},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(task.User))},
	}

	for attempt := 0; attempt < 2; attempt++ {
		resp, err := a.client.Messages.New(ctx, params)
		if err != nil {
			return fmt.Errorf("anthropic: %w", err)
		}
		if resp.StopReason == anthropic.StopReasonRefusal {
			return fmt.Errorf("anthropic: the model declined to answer (%s)", resp.StopDetails.Category)
		}
		for _, block := range resp.Content {
			use, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok || use.Name != aikit.ToolSubmit {
				continue
			}
			if err := json.Unmarshal([]byte(use.JSON.Input.Raw()), out); err != nil {
				return fmt.Errorf("anthropic: the answer could not be read: %w", err)
			}
			return nil
		}
		// Answered in prose. Asked again, appending rather than editing the
		// conversation, so nothing the model already produced is rewritten.
		params.Messages = append(params.Messages, resp.ToParam(),
			anthropic.NewUserMessage(anthropic.NewTextBlock("Submit that answer now by calling the submit tool.")))
	}
	return errors.New("anthropic: the model finished without submitting an answer")
}
