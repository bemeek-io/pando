package screening

import (
	"time"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// Outcome is what a screening did, recorded on the proposal.
//
// On the proposal and not only in the audit log, because the console renders it
// as a section of the review: which adapter, which model, what it read, what it
// changed and why, and what it asked for that Pando would not do. R-314 calls
// for attribution, and attribution nobody can see is a record, not attribution.
type Outcome struct {
	// Ran is false when nothing screened. Skipped says why.
	Ran bool `json:"ran"`

	// Skipped is the reason no screening happened: no adapter configured, one
	// that does not screen, host policy, an unreachable provider, a timeout.
	// Every one of them is an ordinary outcome (R-315).
	Skipped string `json:"skipped,omitempty"`

	// SkipCode is Skipped as a stable machine value, so a client can branch
	// without matching on a sentence that may be reworded.
	SkipCode SkipCode `json:"skip_code,omitempty"`

	AdapterRef string `json:"adapter_ref,omitempty"`
	Model      string `json:"model,omitempty"`

	// FilesRead is what left the host (R-317).
	FilesRead []string `json:"files_read,omitempty"`

	Applied []Applied `json:"applied,omitempty"`

	// Refused is never dropped. An amendment that vanishes because core did not
	// like it teaches nobody anything, and these are how the next version of
	// the prompt gets written.
	Refused []Refused `json:"refused,omitempty"`

	// Answers are the detection questions the screener answered (R-318),
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
	// nothing for it, because an install without one is not degraded (R-315).
	SkipNotConfigured SkipCode = "not_configured"
	SkipPolicy        SkipCode = "policy"      // host policy forbids it (R-316)
	SkipUnsupported   SkipCode = "unsupported" // the adapter does not screen
	SkipUnavailable   SkipCode = "unavailable" // the provider failed or timed out
	SkipBlocked       SkipCode = "blocked"     // nothing to screen (R-099)
)

// SkippedOutcome returns an outcome for a screening that never ran.
func SkippedOutcome(code SkipCode, reason string) Outcome {
	return Outcome{Ran: false, SkipCode: code, Skipped: reason}
}

// Elapsed records how long a screening took.
func (o Outcome) Elapsed(d time.Duration) Outcome {
	o.DurationMS = d.Milliseconds()
	return o
}
