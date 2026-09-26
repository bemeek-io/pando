package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/bemeek-io/pando/internal/adapter/ai/aikit"
	"github.com/bemeek-io/pando/internal/adapter/api"
)

// errDone ends a conversation from inside a call handler: the model answered.
var errDone = errors.New("done")

// converse holds one conversation through Chat Completions: a system prompt,
// the first message, and the tools. Every tool call goes to handle; handle
// returns errDone once the model has answered through answerTool.
//
// A local model often answers in prose where a tool call was asked for, and
// sometimes writes the call out as text. Both are read: a reply that is a JSON
// object is taken as the answer tool's input, and one shaped as {"name",
// "arguments"} as a call to that tool. Anything else is asked for again once.
func (a *Adapter) converse(
	ctx context.Context,
	model, system, input string,
	tools []aikit.Tool,
	answerTool string,
	handle func(name, arguments string) (string, bool, error),
) error {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system + " If you cannot call a tool, reply with only the JSON object the " +
			answerTool + " tool takes, and nothing else."),
		openai.UserMessage(input),
	}
	params := openai.ChatCompletionNewParams{
		Model:     model,
		Tools:     toolParams(tools),
		MaxTokens: openai.Int(maxOutputTokens),
	}

	nudged := false
	for range maxIterations {
		if err := ctx.Err(); err != nil {
			return err
		}
		params.Messages = messages
		completion, err := a.client.Chat.Completions.New(ctx, params)
		if err != nil {
			return fmt.Errorf("local: %w", err)
		}
		if len(completion.Choices) == 0 {
			return errors.New("local: the server returned no answer")
		}
		msg := completion.Choices[0].Message
		messages = append(messages, msg.ToParam())

		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				fn := tc.AsFunction()
				text, isError, err := handle(fn.Function.Name, fn.Function.Arguments)
				if errors.Is(err, errDone) {
					return nil
				}
				if err != nil {
					return err
				}
				if isError {
					text = "That did not work: " + text
				}
				messages = append(messages, openai.ToolMessage(text, tc.ID))
			}
			continue
		}

		// No tool call: the answer may be in the text.
		if name, args, ok := answerInText(msg.Content, answerTool); ok {
			text, isError, err := handle(name, args)
			if errors.Is(err, errDone) {
				return nil
			}
			if err != nil {
				return err
			}
			if isError {
				text = "That did not work: " + text
			}
			messages = append(messages, openai.UserMessage(text))
			continue
		}
		if nudged {
			return errors.New("local: the model finished without submitting a result")
		}
		nudged = true
		messages = append(messages, openai.UserMessage("Submit your answer now by calling the "+answerTool+
			" tool, or reply with only the JSON object it takes."))
	}
	return fmt.Errorf("local: the call did not finish within %d rounds", maxIterations)
}

// answerInText reads a call out of a reply that has none: a JSON object shaped
// as {"name", "arguments"} is a call to name; any other JSON object is the
// answer tool's input. Code fences around it are allowed.
func answerInText(content, answerTool string) (string, string, bool) {
	s := strings.TrimSpace(content)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		return "", "", false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return "", "", false
	}
	if rawName, ok := obj["name"]; ok {
		var name string
		if json.Unmarshal(rawName, &name) == nil && name != "" {
			if args, ok := obj["arguments"]; ok {
				// Arguments may arrive as an object or as a JSON string.
				var str string
				if json.Unmarshal(args, &str) == nil {
					return name, str, true
				}
				return name, string(args), true
			}
		}
	}
	return answerTool, s, true
}

// toolParams are aikit's tools as Chat Completions takes them. Not strict:
// few local servers honor it, and core validates every answer regardless.
func toolParams(tools []aikit.Tool) []openai.ChatCompletionToolUnionParam {
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		out = append(out, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        t.Name,
			Description: openai.String(t.Description),
			Parameters:  shared.FunctionParameters(t.Schema()),
		}))
	}
	return out
}

// RepairPlan reads a failed proposal and proposes amendments (R-106, R-336).
func (a *Adapter) RepairPlan(ctx context.Context, req api.ScreenRequest) (api.ScreenResult, error) {
	return a.screen(ctx, api.AIFunctionRepairPlan, req)
}

// AnswerQuestions answers detection's outstanding questions (R-338).
func (a *Adapter) AnswerQuestions(ctx context.Context, req api.ScreenRequest) (api.ScreenResult, error) {
	return a.screen(ctx, api.AIFunctionAnswerQuestions, req)
}

// RevisePlan acts on what a person reviewing the plan asked for (R-336).
func (a *Adapter) RevisePlan(ctx context.Context, req api.ScreenRequest) (api.ScreenResult, error) {
	return a.screen(ctx, api.AIFunctionRevisePlan, req)
}

func (a *Adapter) screen(ctx context.Context, fn api.AIFunction, req api.ScreenRequest) (api.ScreenResult, error) {
	if !a.ready {
		return api.ScreenResult{}, errors.New("local: not configured")
	}
	if a.cfg.ScreenPlans != nil && !*a.cfg.ScreenPlans {
		return api.ScreenResult{}, errors.New("local: this adapter is set not to assist detection")
	}
	if req.Source == nil {
		return api.ScreenResult{}, errors.New("local: no readable copy of the repository was supplied")
	}

	src := aikit.NewReader(req.Source, aikit.Limit(req.Budget.MaxFiles, a.cfg.MaxFiles), aikit.Limit(req.Budget.MaxBytes, a.cfg.MaxBytes))
	model := a.model(req.Model)
	var result api.ScreenResult
	err := a.converse(ctx, model, aikit.SystemPrompt(fn),
		aikit.UserPrompt(fn, req)+aikit.Preloaded(src, req.Known),
		aikit.ScreenTools(fn, req.Questions, req.Values), aikit.ToolSubmitFindings,
		func(name, arguments string) (string, bool, error) {
			if name == aikit.ToolSubmitFindings {
				r, err := aikit.Findings(arguments, src, model)
				if err != nil {
					return err.Error(), true, nil //nolint:nilerr // handed back to the model to fix, not a failure of the call.
				}
				result = r
				return "", false, errDone
			}
			text, isError := aikit.Call(name, arguments, src)
			return text, isError, nil
		})
	if err != nil {
		return api.ScreenResult{}, err
	}
	return result, nil
}

// submit runs one administrative task and decodes its answer into out. An
// answer that does not decode is handed back to the model to fix, once per
// round, rather than failing the request: small models get JSON wrong.
func (a *Adapter) submit(ctx context.Context, model string, task aikit.Task, out any) error {
	if !a.ready {
		return errors.New("local: not configured")
	}
	return a.converse(ctx, model, task.System+aikit.AnswerThroughTool, task.User,
		[]aikit.Tool{task.Tool}, task.Tool.Name,
		func(name, arguments string) (string, bool, error) {
			if name != task.Tool.Name {
				return "There is no tool by that name. Answer with " + task.Tool.Name + ".", true, nil
			}
			if err := json.Unmarshal([]byte(arguments), out); err != nil {
				//nolint:nilerr // handed back to the model to fix, not a failure of the call.
				return "Your answer could not be read as the " + task.Tool.Name + " tool's input: " +
					err.Error() + ". Send it again.", true, nil
			}
			return "", false, errDone
		})
}

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
