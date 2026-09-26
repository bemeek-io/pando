package anthropic

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/bemeek-io/pando/internal/adapter/ai/aikit"
	"github.com/bemeek-io/pando/internal/adapter/api"
)

// RepairPlan reads a failed proposal and proposes amendments that might make
// it work (R-106, R-336). Core calls it only when the trial run crashed or no
// detector could read the repository.
func (a *Adapter) RepairPlan(ctx context.Context, req api.ScreenRequest) (api.ScreenResult, error) {
	return a.run(ctx, api.AIFunctionRepairPlan, req)
}

// AnswerQuestions answers detection's outstanding questions from the
// repository (R-338). Core calls it only when there are some.
func (a *Adapter) AnswerQuestions(ctx context.Context, req api.ScreenRequest) (api.ScreenResult, error) {
	return a.run(ctx, api.AIFunctionAnswerQuestions, req)
}

// RevisePlan acts on what a person reviewing the plan asked for, checking it
// against the repository first (R-336). Core calls it only when somebody asks.
func (a *Adapter) RevisePlan(ctx context.Context, req api.ScreenRequest) (api.ScreenResult, error) {
	return a.run(ctx, api.AIFunctionRevisePlan, req)
}

// run is the conversation every function has; the prompt and the amendments
// the model may submit are what differ.
//
// A manual loop rather than the SDK's tool runner, for one reason: the budget.
// Each read has to be counted, refused when the ceiling is reached, and turned
// into a tool_result the model can act on rather than an error that ends the
// conversation — and the loop has to stop the moment findings are submitted
// rather than when the model runs out of things to say.
func (a *Adapter) run(ctx context.Context, fn api.AIFunction, req api.ScreenRequest) (api.ScreenResult, error) {
	if !a.ready {
		return api.ScreenResult{}, errors.New("anthropic: not configured")
	}
	if !a.screensPlans() {
		return api.ScreenResult{}, errors.New("anthropic: this adapter is set not to assist detection")
	}
	if req.Source == nil {
		return api.ScreenResult{}, errors.New("anthropic: no readable copy of the repository was supplied")
	}

	src := aikit.NewReader(req.Source, aikit.Limit(req.Budget.MaxFiles, a.cfg.MaxFiles), aikit.Limit(req.Budget.MaxBytes, a.cfg.MaxBytes))

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model(req.Model)),
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{{
			Text: aikit.SystemPrompt(fn),

			// The system prompt and the tool definitions are identical on every
			// call of one function and sit ahead of everything that varies, so
			// one breakpoint here is cached across every app an install onboards.
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Tools: tools(fn, req.Questions, req.Values),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(aikit.UserPrompt(fn, req) + aikit.Preloaded(src, req.Known))),
		},
	}

	for range maxIterations {
		// Checked before each round rather than relied on through the client,
		// so an expired budget ends the loop rather than the next HTTP call.
		if err := ctx.Err(); err != nil {
			return api.ScreenResult{}, err
		}

		resp, err := a.client.Messages.New(ctx, params)
		if err != nil {
			return api.ScreenResult{}, fmt.Errorf("anthropic: %w", err)
		}
		params.Messages = append(params.Messages, resp.ToParam())

		// A refusal is an answer, not a transport failure. Reported as an
		// error so R-335 keeps the deterministic proposal, with the category
		// carried so an operator can tell it from an outage.
		if resp.StopReason == anthropic.StopReasonRefusal {
			return api.ScreenResult{}, fmt.Errorf(
				"anthropic: the model declined to read this repository (%s)", resp.StopDetails.Category)
		}

		var results []anthropic.ContentBlockParamUnion
		for _, block := range resp.Content {
			use, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}

			// Findings end the conversation. Nothing after them is read: the
			// model has answered, and a second call would be a second answer.
			if use.Name == aikit.ToolSubmitFindings {
				result, err := aikit.Findings(use.JSON.Input.Raw(), src, a.model(req.Model))
				if err != nil {
					return api.ScreenResult{}, fmt.Errorf("anthropic: %w", err)
				}
				return result, nil
			}

			text, isError := aikit.Call(use.Name, use.JSON.Input.Raw(), src)
			results = append(results, anthropic.NewToolResultBlock(use.ID, text, isError))
		}

		if resp.StopReason != anthropic.StopReasonToolUse || len(results) == 0 {
			// It stopped without submitting. Not an error in the usual sense,
			// but there is nothing to apply, and saying so plainly is better
			// than returning an empty result that reads like "no problems
			// found" — those two mean opposite things to a person reviewing.
			return api.ScreenResult{}, errors.New(
				"anthropic: the model finished without submitting a result")
		}

		// All results in one user turn. Splitting them across messages trains
		// the model out of asking for several files at once.
		params.Messages = append(params.Messages, anthropic.NewUserMessage(results...))
	}

	return api.ScreenResult{}, fmt.Errorf(
		"anthropic: the call did not finish within %d rounds", maxIterations)
}
