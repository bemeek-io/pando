package screening

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// Default budget (R-339). Overridden downward by an adapter's own capabilities
// and by the install's configuration; never raised by either.
const (
	DefaultMaxFiles = 40
	DefaultMaxBytes = 256 << 10
	DefaultTimeout  = 2 * time.Minute
)

// Screener is the half of api.AIAdapter this package needs.
//
// Narrowed so a test can supply one in four lines, and so the dependency reads
// as what it is: something that reviews a proposal, not an adapter registry.
type Screener interface {
	Capabilities(ctx context.Context) (api.AICapabilities, error)
	ScreenPlan(ctx context.Context, req api.ScreenRequest) (api.ScreenResult, error)
}

// Run calls the screener under the budget and returns what it said.
//
// It never returns an error. Every failure becomes a skipped Outcome carrying
// its reason, because R-335 is that a screening which cannot run leaves the
// deterministic proposal exactly as it was — and a caller that has to remember
// to ignore an error is a caller that will one day not.
func Run(ctx context.Context, s Screener, ref string, req api.ScreenRequest) (api.ScreenResult, Outcome) {
	if s == nil {
		return api.ScreenResult{}, SkippedOutcome(SkipNotConfigured, "No AI adapter is configured.")
	}

	started := time.Now()
	caps, err := s.Capabilities(ctx)
	if err != nil {
		return api.ScreenResult{}, SkippedOutcome(SkipUnavailable,
			"Pando could not reach the configured AI adapter: "+err.Error()).Elapsed(time.Since(started))
	}
	if !caps.Does(api.AIFunctionScreenPlan) {
		return api.ScreenResult{}, SkippedOutcome(SkipUnsupported, fmt.Sprintf(
			"The configured AI adapter (%s) does not screen deployment plans.", ref)).Elapsed(time.Since(started))
	}

	req.Budget = budget(req.Budget, caps)

	// The timeout is enforced here rather than trusted to the adapter. An
	// adapter that ignores its budget would otherwise hold an app in detection
	// for as long as a provider takes to answer, and detection runs in the
	// background where nobody is watching the clock.
	callCtx, cancel := context.WithTimeout(ctx, req.Budget.Timeout)
	defer cancel()

	result, err := s.ScreenPlan(callCtx, req)
	elapsed := time.Since(started)
	if err != nil {
		return api.ScreenResult{}, SkippedOutcome(SkipUnavailable,
			"The AI adapter could not screen this plan: "+err.Error()).Elapsed(elapsed)
	}

	model := result.Model
	if model == "" {
		model = caps.Model
	}
	return result, Outcome{
		Ran:        true,
		AdapterRef: ref,
		Model:      model,
		FilesRead:  result.FilesRead,
		Notes:      result.Notes,
		DurationMS: elapsed.Milliseconds(),
	}
}

// budget lowers the install's limits to the adapter's, never the other way.
func budget(want api.ScreenBudget, caps api.AICapabilities) api.ScreenBudget {
	if want.MaxFiles <= 0 {
		want.MaxFiles = DefaultMaxFiles
	}
	if want.MaxBytes <= 0 {
		want.MaxBytes = DefaultMaxBytes
	}
	if want.Timeout <= 0 {
		want.Timeout = DefaultTimeout
	}
	if caps.MaxFiles > 0 && caps.MaxFiles < want.MaxFiles {
		want.MaxFiles = caps.MaxFiles
	}
	if caps.MaxBytes > 0 && caps.MaxBytes < want.MaxBytes {
		want.MaxBytes = caps.MaxBytes
	}
	return want
}

// Split separates answers to detection's questions from spec amendments.
//
// R-338 makes an answer an amendment like any other — evidenced, attributed,
// refused when it does not match an outstanding question — but it is applied
// through detection's own answer machinery rather than here, because that
// machinery already knows that answering "which service is primary" changes
// what every other answer means. Two ways to apply an answer is how the two
// would drift.
func Split(env Env, amendments []api.Amendment, outstanding map[string]bool) (
	answers map[string]string, rest []api.Amendment, refused []Refused,
) {
	answers = map[string]string{}
	for _, a := range amendments {
		if a.Kind != api.AmendAnswerQuestion {
			rest = append(rest, a)
			continue
		}
		if reason := check(env, a); reason != "" {
			refused = append(refused, Refused{Amendment: a, Reason: reason})
			continue
		}

		key := strings.TrimSpace(a.Key)
		value := strings.TrimSpace(a.Value)
		if !outstanding[key] {
			refused = append(refused, Refused{Amendment: a, Reason: fmt.Sprintf(
				"Pando is not waiting on an answer called %q.", a.Key)})
			continue
		}
		if value == "" {
			refused = append(refused, Refused{Amendment: a, Reason: fmt.Sprintf(
				"The amendment answered %q with nothing.", key)})
			continue
		}
		if _, already := answers[key]; already {
			refused = append(refused, Refused{Amendment: a, Reason: fmt.Sprintf(
				"%q was already answered by this screening.", key)})
			continue
		}
		answers[key] = value
	}

	if len(answers) == 0 {
		answers = nil
	}
	return answers, rest, refused
}
