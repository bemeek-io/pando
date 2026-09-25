package detection

import (
	"context"
	"fmt"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/screening"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
)

// Screening: Sequence A's step 12a, after the install's defaults and before the
// proposal is stored. Design 10 §4.
//
// It is here rather than in internal/detect for three reasons, and the first is
// the one that matters. The auction is a pure function of the source, testable
// without a network and without a model, and R-330 says a screener is not a
// detector — keeping it out of that package is how that stays true rather than
// being asserted. The other two are practical: R-027 forbids an adapter writing
// an audit event, so R-337's event has to be written by core, and host policy is
// already read here.

// ScreenPolicy is host policy's veto over screening (R-336).
//
// Its own interface rather than a method on SourcePolicy, because a nil one
// means "allowed" and every existing implementation of SourcePolicy would
// otherwise have had to grow a method saying so.
type ScreenPolicy interface {
	// AllowsScreening returns a reason when host policy forbids it, and an
	// empty string when it does not. A reason, not an error: this is not a
	// failure, it is a posture, and it is shown in the review as one.
	AllowsScreening(ctx context.Context) string
}

// Auditor records that a repository's contents left the host (R-337).
type Auditor interface {
	Write(ctx context.Context, e AuditEvent) error
}

// AuditEvent is what a screening records.
type AuditEvent struct {
	Action string
	AppID  string
	Detail map[string]any
}

// ActionScreen is R-337's event.
const ActionScreen = "detection.screen"

// screen calls the AI adapter when detection needs it, and folds in what it
// may. When detection does not need it, nothing is called (R-336).
//
// It mutates proposal and returns the outcome. It never returns an error:
// R-335 makes every failure here leave the deterministic proposal exactly as it
// was, and the only trace is a reason recorded on the outcome.
func (r *Runner) screen(ctx context.Context, appID string, proposal *detect.Proposal, view api.SourceView) screening.Outcome {
	if r.Screener == nil {
		return screening.SkippedOutcome(screening.SkipNotConfigured, "No AI adapter is configured.")
	}

	// A blocked proposal is not screened. There is nothing useful to amend in a
	// compose file that cannot be imported, and the reason it cannot is the
	// answer (R-099) — the same argument the job makes for skipping the trial
	// run on a blocked winner.
	if proposal.Blocked != nil {
		return screening.SkippedOutcome(screening.SkipBlocked, "This repository cannot be imported as written, so there was nothing to screen.")
	}

	fn, why := needed(*proposal)
	if fn == "" {
		// The common case, and the point of R-336's amendment: a plan that
		// worked and asked nothing is not sent anywhere. No latency, no cost,
		// no repository contents leaving the host, and no audit event because
		// nothing happened.
		return screening.SkippedOutcome(screening.SkipNotNeeded,
			"Detection produced a plan without failing or asking anything, so no AI adapter was called.")
	}

	if r.ScreenPolicy != nil {
		if reason := r.ScreenPolicy.AllowsScreening(ctx); reason != "" {
			o := screening.SkippedOutcome(screening.SkipPolicy, reason).For(fn)
			o.Why = why
			return o
		}
	}

	// Only now, so the console's "Checking with AI" step appears on the
	// detections that call one and on no others.
	detect.Report(ctx, detect.StageScreening, proposal)

	trial := proposal.TrialSummary()
	result, outcome := screening.Run(ctx, r.Screener, r.ScreenerRef, fn, api.ScreenRequest{
		Source:    view,
		Spec:      proposal.DraftSpec,
		Evidence:  proposal.Winner.Evidence,
		Questions: questionsFor(proposal.Questions),
		Trial:     trial,
		Budget: api.ScreenBudget{
			MaxFiles: r.ScreenMaxFiles,
			MaxBytes: r.ScreenMaxBytes,
			Timeout:  r.ScreenTimeout,
		},
	})

	outcome.Why = why

	// Audited whether or not anything was applied, and before the amendments
	// are folded in: the event records that a repository was read, which
	// happened regardless of what Pando then did with the answer.
	r.auditScreen(ctx, appID, outcome)

	if !outcome.Ran {
		return outcome
	}

	env := screening.Env{Source: view, Trial: trial}

	// Answers first. One of them can change which reading of the repository the
	// rest applies to — the tie-break — and detection's own answer machinery is
	// where that is already known (design 10 §5, detect.WithScreenedAnswers).
	answers, rest, refused := screening.Split(env, result.Amendments, outstanding(proposal.Questions))
	outcome.Refused = append(outcome.Refused, refused...)

	// Asked for answers, it may give answers. A plan that worked needs no
	// repairing, and the only reason this call was made is the questions
	// (R-336). Refused rather than dropped, so the review shows what was asked.
	if fn == api.AIFunctionAnswerQuestions {
		for _, a := range rest {
			outcome.Refused = append(outcome.Refused, screening.Refused{Amendment: a,
				Reason: "Pando asked the AI adapter only to answer detection's questions; this plan did not fail, so it was not changed."})
		}
		rest = nil
	}

	// An answer that cannot become a spec is not an answer, whoever gave it.
	// The screener answered build_method in prose ("serve the repository root
	// with php -S …") and it was applied to nothing (issue #55).
	for _, key := range []string{detect.KeyBuildStrategy, detect.KeyBuildMethod} {
		value, given := answers[key]
		if !given {
			continue
		}
		if err := proposal.CheckAnswers(map[string]string{key: value}); err != nil {
			delete(answers, key)
			outcome.Refused = append(outcome.Refused, screening.Refused{
				Amendment: api.Amendment{Kind: api.AmendAnswerQuestion, Key: key, Value: value},
				Reason:    errs.As(err).Message,
			})
		}
	}
	if len(answers) > 0 {
		proposal.DraftSpec = proposal.WithScreenedAnswers(answers)
		proposal.Questions = unanswered(proposal.Questions, answers)
		outcome.Answers = answers

		// Listed with the other changes, reason and evidence included, so the
		// review shows why a question stopped being asked — not only that it did.
		pending := make(map[string]string, len(answers))
		for k, v := range answers {
			pending[k] = v
		}
		for _, a := range result.Amendments {
			key, value := strings.TrimSpace(a.Key), strings.TrimSpace(a.Value)
			if a.Kind != api.AmendAnswerQuestion || pending[key] != value || value == "" {
				continue
			}
			outcome.Applied = append(outcome.Applied, screening.Applied{
				Amendment: a, Summary: fmt.Sprintf("answered %s: %s", key, value),
			})
			delete(pending, key) // once each
		}
	}

	applied, refusedRest := screening.Apply(&proposal.DraftSpec, env, rest)
	outcome.Applied = append(outcome.Applied, applied...)
	outcome.Refused = append(outcome.Refused, refusedRest...)

	// The winner's draft is kept in step with the spec, because it is what the
	// review renders beside the evidence and what a re-detection diffs against.
	proposal.Winner.Draft.Build = proposal.DraftSpec.Build
	proposal.Winner.Draft.Workloads = proposal.DraftSpec.Workloads
	proposal.Winner.Draft.Volumes = proposal.DraftSpec.Volumes
	proposal.Winner.Draft.Slots = proposal.DraftSpec.Slots
	proposal.Winner.Draft.Health = proposal.DraftSpec.Health
	proposal.Winner.Draft.Warnings = proposal.DraftSpec.Warnings

	return outcome
}

func (r *Runner) auditScreen(ctx context.Context, appID string, o screening.Outcome) {
	if r.Auditor == nil {
		return
	}

	detail := map[string]any{"ran": o.Ran, "function": string(o.Function), "why": o.Why}
	if o.Skipped != "" {
		detail["skipped"] = o.Skipped
	}
	if o.Ran {
		detail["adapter_ref"] = o.AdapterRef
		detail["model"] = o.Model
		detail["files_read"] = o.FilesRead
		detail["duration_ms"] = o.DurationMS
	}

	// A failure to audit does not fail the detection, and it does not fail the
	// screening either — which already happened. R-335 holds here too.
	_ = r.Auditor.Write(ctx, AuditEvent{Action: ActionScreen, AppID: appID, Detail: detail})
}

// needed decides whether detection needs an AI adapter, and for what (R-336).
//
// Two triggers and no others. A plan that failed is repaired: the trial run
// crashed, or no detector could read the repository at all. Otherwise a plan
// that asks something has its questions answered. A repair is handed the
// questions too and may answer them, so one call covers a detection that both
// failed and asked — there is never a second.
//
// An empty function means nothing is needed.
func needed(p detect.Proposal) (api.AIFunction, string) {
	switch {
	case p.Trial.Crashed:
		return api.AIFunctionRepairPlan, "Pando started this app to watch it, and it exited with an error."
	case p.Winner.Strategy == detect.StrategyUnknown:
		return api.AIFunctionRepairPlan, "None of Pando's detectors could work out how to build this repository."
	}
	if n := len(detect.Asked(p.Questions)); n > 0 {
		if n == 1 {
			return api.AIFunctionAnswerQuestions, "Detection asked one question."
		}
		return api.AIFunctionAnswerQuestions, fmt.Sprintf("Detection asked %d questions.", n)
	}
	return "", ""
}

// questionsFor converts detection's questions to the adapter vocabulary.
//
// Deferred ones are included: a question the trial run was meant to answer and
// did not is exactly the kind a screener can settle from the repository, and
// ApplyTrial has already promoted the ones it could not resolve.
func questionsFor(qs []detect.Question) []api.Question {
	out := make([]api.Question, 0, len(qs))
	for _, q := range qs {
		out = append(out, api.Question{
			Key: q.Key, Prompt: q.Prompt, Why: q.Why, Kind: q.Kind, Options: q.Options,
		})
	}
	return out
}

func outstanding(qs []detect.Question) map[string]bool {
	keys := map[string]bool{}
	for _, q := range qs {
		keys[q.Key] = true
	}
	return keys
}

func unanswered(qs []detect.Question, answers map[string]string) []detect.Question {
	var out []detect.Question
	for _, q := range qs {
		if _, done := answers[q.Key]; done {
			continue
		}
		out = append(out, q)
	}
	return out
}
