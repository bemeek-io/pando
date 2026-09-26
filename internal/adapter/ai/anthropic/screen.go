package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

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

	src := newReader(req.Source, a.limit(req.Budget.MaxFiles, a.cfg.MaxFiles), a.limitBytes(req.Budget.MaxBytes, a.cfg.MaxBytes))

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.cfg.Model),
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{{
			Text: systemPrompt(fn),

			// The system prompt and the tool definitions are identical on every
			// call of one function and sit ahead of everything that varies, so
			// one breakpoint here is cached across every app an install onboards.
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Tools: tools(fn, req.Questions),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt(fn, req) + preloaded(src, req.Known))),
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
			if use.Name == toolSubmitFindings {
				return a.findings(use, src)
			}

			results = append(results, a.call(use, src))
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

// call runs one read tool and shapes the result.
//
// A budget refusal comes back as an ordinary tool_result rather than an error,
// so the model can submit what it has instead of the whole screening being
// lost over one file too many. That is the difference between a bounded
// screening and a failed one.
func (a *Adapter) call(use anthropic.ToolUseBlock, src *reader) anthropic.ContentBlockParamUnion {
	switch use.Name {
	case toolListFiles:
		var in struct {
			Pattern string `json:"pattern"`
		}
		if err := json.Unmarshal([]byte(use.JSON.Input.Raw()), &in); err != nil {
			return anthropic.NewToolResultBlock(use.ID, "That request could not be read: "+err.Error(), true)
		}
		matches, err := src.glob(in.Pattern)
		if err != nil {
			return anthropic.NewToolResultBlock(use.ID, err.Error(), true)
		}
		if len(matches) == 0 {
			return anthropic.NewToolResultBlock(use.ID, "Nothing in this repository matches that pattern.", false)
		}
		body, _ := json.Marshal(matches) //nolint:errcheck // a []string always marshals.
		return anthropic.NewToolResultBlock(use.ID, string(body), false)

	case toolReadFile:
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(use.JSON.Input.Raw()), &in); err != nil {
			return anthropic.NewToolResultBlock(use.ID, "That request could not be read: "+err.Error(), true)
		}
		body, err := src.open(in.Path)
		if err != nil {
			if errors.Is(err, errBudget) {
				return anthropic.NewToolResultBlock(use.ID,
					err.Error()+". Submit your findings now, based on what you have read.", false)
			}
			return anthropic.NewToolResultBlock(use.ID, err.Error(), true)
		}
		return anthropic.NewToolResultBlock(use.ID, body, false)

	default:
		return anthropic.NewToolResultBlock(use.ID, "There is no tool by that name.", true)
	}
}

// findings reads the submitted amendments.
func (a *Adapter) findings(use anthropic.ToolUseBlock, src *reader) (api.ScreenResult, error) {
	var in struct {
		Amendments []api.Amendment `json:"amendments"`
		Notes      []string        `json:"notes"`
		Reply      string          `json:"reply"`
	}

	// Parsed rather than matched on the raw string: escaping in a tool input is
	// the model's to choose, and string matching on it is how that bites.
	if err := json.Unmarshal([]byte(use.JSON.Input.Raw()), &in); err != nil {
		return api.ScreenResult{}, fmt.Errorf("anthropic: the submitted result could not be read: %w", err)
	}

	return api.ScreenResult{
		Amendments: in.Amendments,
		Notes:      in.Notes,
		Reply:      in.Reply,
		FilesRead:  src.files(),
		Model:      a.cfg.Model,
	}, nil
}

// preloaded reads the files an earlier call on this proposal read, and hands
// them over at the start: the model begins knowing what it knew last time,
// rather than listing and reading its way back there one round trip at a time.
// Read through the budgeted reader, so they count as reads and are recorded
// as sent (R-337); one the budget refuses, or that is gone, is left out.
func preloaded(src *reader, known []string) string {
	if len(known) == 0 {
		return ""
	}
	var b strings.Builder
	for _, name := range known {
		body, err := src.open(name)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n\n```\n%s\n```\n", name, body)
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n## Files you read about this plan before\n\nYou read these in an earlier look at this " +
		"repository, at this same commit. Their contents are below, so there is no need to read them " +
		"again; read anything else you need.\n" + b.String()
}

// limit takes the lower of what core asked for and what this adapter will do.
// Neither side raises the other's (design 10 §2).
func (a *Adapter) limit(asked, own int) int {
	if asked <= 0 || asked > own {
		return own
	}
	return asked
}

func (a *Adapter) limitBytes(asked, own int64) int64 {
	if asked <= 0 || asked > own {
		return own
	}
	return asked
}
