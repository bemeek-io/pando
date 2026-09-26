package screening

import (
	"time"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// Outcome is what a screening did, recorded on the proposal.
//
// On the proposal and not only in the audit log, because the console renders it
// as a section of the review: which adapter, which model, what it read, what it
// changed and why, and what it asked for that Pando would not do. R-334 calls
// for attribution, and attribution nobody can see is a record, not attribution.
type Outcome struct {
	// Ran is false when nothing screened. Skipped says why.
	Ran bool `json:"ran"`

	// Skipped is the reason no screening happened: no adapter configured, one
	// that does not screen, host policy, an unreachable provider, a timeout.
	// Every one of them is an ordinary outcome (R-335).
	Skipped string `json:"skipped,omitempty"`

	// SkipCode is Skipped as a stable machine value, so a client can branch
	// without matching on a sentence that may be reworded.
	SkipCode SkipCode `json:"skip_code,omitempty"`

	// Function is what the adapter was asked to do: repair_plan when detection
	// failed, answer_questions when it asked something (R-336). Empty when
	// nothing was needed.
	Function api.AIFunction `json:"function,omitempty"`

	// Why is what made the call necessary — the trial run crashed, no detector
	// could read the repository, detection asked a question — in one sentence.
	// On the repair path the original failure is still on the proposal, as its
	// trial log, whatever the repair did (R-107).
	Why string `json:"why,omitempty"`

	AdapterRef string `json:"adapter_ref,omitempty"`
	Model      string `json:"model,omitempty"`

	// FilesRead is what left the host (R-337).
	FilesRead []string `json:"files_read,omitempty"`

	Applied []Applied `json:"applied,omitempty"`

	// Refused is never dropped. An amendment that vanishes because core did not
	// like it teaches nobody anything, and these are how the next version of
	// the prompt gets written.
	Refused []Refused `json:"refused,omitempty"`

	// Answers are the detection questions the screener answered (R-338),
	// applied through the same machinery a person's answers go through.
	Answers map[string]string `json:"answers,omitempty"`

	Notes []string `json:"notes,omitempty"`

	DurationMS int64 `json:"duration_ms,omitempty"`
}

// Changed reports whether the screening altered the proposal at all.
func (o Outcome) Changed() bool {
	return len(o.Applied) > 0 || len(o.Answers) > 0
}

// Applied is an amendment that landed.
type Applied struct {
	Amendment api.Amendment `json:"amendment"`

	// Summary is what changed, in one line, for the review.
	Summary string `json:"summary"`
}

// Refused is an amendment that did not, and why.
type Refused struct {
	Amendment api.Amendment `json:"amendment"`
	Reason    string        `json:"reason"`
}

// SkipCode says why a screening did not run.
type SkipCode string

const (
	// SkipNotConfigured is the ordinary case: no AI adapter. The console shows
	// nothing for it, because an install without one is not degraded (R-335).
	SkipNotConfigured SkipCode = "not_configured"

	// SkipNotNeeded is the other ordinary case, and the common one on an install
	// that has an adapter: detection produced a plan, the plan did not fail, and
	// nothing was asked. No call is made, nothing leaves the host, and the
	// console shows nothing for it (R-336).
	SkipNotNeeded SkipCode = "not_needed"

	SkipPolicy      SkipCode = "policy"      // host policy forbids it (R-336)
	SkipUnsupported SkipCode = "unsupported" // the adapter does not do what was needed
	SkipUnavailable SkipCode = "unavailable" // the provider failed or timed out
	SkipBlocked     SkipCode = "blocked"     // nothing to screen (R-099)
)

// SkippedOutcome returns an outcome for a screening that never ran.
func SkippedOutcome(code SkipCode, reason string) Outcome {
	return Outcome{Ran: false, SkipCode: code, Skipped: reason}
}

// For records which function the outcome was for.
func (o Outcome) For(fn api.AIFunction) Outcome {
	o.Function = fn
	return o
}

// Elapsed records how long a screening took.
func (o Outcome) Elapsed(d time.Duration) Outcome {
	o.DurationMS = d.Milliseconds()
	return o
}
