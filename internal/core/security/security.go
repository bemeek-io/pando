// Package security turns a scanner's findings into a number, and that number
// into a decision.
//
// Requirements §23, design 09. The split matters: the scanner knows what is
// wrong, this package knows what it costs and what follows, and neither learns
// the other's vocabulary (R-251). Everything here is arithmetic and policy —
// there is no I/O in the scoring, which is why it can be tested without Docker,
// a scanner, or a database.
package security

import (
	"context"
	"sort"
	"time"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/policy"
)

// The weights (R-313).
//
// One critical finding costs more than fifty low ones, deliberately. A score
// that averaged, or that scaled with the size of the app, would let a real
// problem hide behind a long tail of noise — and a threshold set against it
// would mean something different for every app.
//
// `unknown` scores as low: a finding whose severity nobody has decided is not
// free, and is not critical either.
const (
	WeightCritical = 25
	WeightHigh     = 10
	WeightMedium   = 3
	WeightLow      = 1
)

// Score is the number, 0 to 100.
func Score(findings []api.Finding) int {
	score := 100
	for _, f := range findings {
		switch f.Severity {
		case api.SeverityCritical:
			score -= WeightCritical
		case api.SeverityHigh:
			score -= WeightHigh
		case api.SeverityMedium:
			score -= WeightMedium
		case api.SeverityLow, api.SeverityUnknown:
			score -= WeightLow
		default:
			// A severity this build does not know is worth at least something:
			// scoring it zero would let an adapter with a typo produce a clean
			// app out of a list of critical findings.
			score -= WeightLow
		}
	}
	if score < 0 {
		return 0
	}
	return score
}

// Fixable returns the findings a fix exists for.
//
// The distinction host policy may act on (R-313): "what is wrong with this app"
// and "what could its owner do about it today" are different questions, and an
// installation that only acts on the second should not be scored on the first.
// A finding with no fix is still real — it is hidden only where policy says the
// number should not count it, and the two agree because the same filter drives
// both.
func Fixable(findings []api.Finding) []api.Finding {
	out := make([]api.Finding, 0, len(findings))
	for _, f := range findings {
		if f.Fix != "" {
			out = append(out, f)
		}
	}
	return out
}

// Ranked returns the findings worst-first.
//
// Severity, then ID, so the same scan orders the same way twice — a list whose
// order depends on the scanner's output order changes under the reader for no
// reason.
func Ranked(findings []api.Finding) []api.Finding {
	return Worst(findings, len(findings))
}

// Worst returns the findings that cost the most, for an error or a summary.
//
// Sorted by severity and then by ID, so the same scan produces the same list
// twice — an error message whose contents depend on map iteration is an error
// message people learn to ignore.
func Worst(findings []api.Finding, n int) []api.Finding {
	ranked := append([]api.Finding(nil), findings...)
	sort.SliceStable(ranked, func(i, j int) bool {
		if cost(ranked[i].Severity) != cost(ranked[j].Severity) {
			return cost(ranked[i].Severity) > cost(ranked[j].Severity)
		}
		return ranked[i].ID < ranked[j].ID
	})
	if len(ranked) > n {
		ranked = ranked[:n]
	}
	return ranked
}

func cost(s api.Severity) int {
	switch s {
	case api.SeverityCritical:
		return WeightCritical
	case api.SeverityHigh:
		return WeightHigh
	case api.SeverityMedium:
		return WeightMedium
	default:
		return WeightLow
	}
}

// Counts is how many findings of each severity, for a sentence a person reads.
type Counts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
}

// Count tallies findings by severity.
func Count(findings []api.Finding) Counts {
	var c Counts
	for _, f := range findings {
		switch f.Severity {
		case api.SeverityCritical:
			c.Critical++
		case api.SeverityHigh:
			c.High++
		case api.SeverityMedium:
			c.Medium++
		case api.SeverityLow:
			c.Low++
		default:
			c.Unknown++
		}
	}
	return c
}

// Verdict is what policy says about an app's standing.
type Verdict string

const (
	// VerdictOK: at or above the threshold, or no threshold in force.
	VerdictOK Verdict = "ok"
	// VerdictUnscanned: a threshold is in force and this revision has no score.
	VerdictUnscanned Verdict = "unscanned"
	// VerdictInsecure: below the threshold.
	VerdictInsecure Verdict = "insecure"
	// VerdictInert: a threshold is set and no scanner is configured, so nothing
	// is enforced and the console says so (R-317).
	VerdictInert Verdict = "inert"
)

// Standing is an app's position against the installation's threshold.
type Standing struct {
	Verdict Verdict `json:"verdict"`

	// Score is nil for a revision with no score, which is not a score of zero
	// (R-318). The wire keeps them apart because the console has to.
	Score     *int `json:"score"`
	Threshold int  `json:"threshold"`

	// Scanned is when the score was taken. Zero when there is none.
	Scanned time.Time `json:"scanned,omitzero"`

	// StopAt is when this app will be stopped, when it is insecure and policy
	// says to stop. Zero otherwise.
	StopAt time.Time `json:"stop_at,omitzero"`
}

// Evaluate places one app against policy.
//
// `score` is nil for a revision that has never been scanned, or whose only scan
// failed — which is not the same as a score of zero and is not treated as one
// (R-318).
func Evaluate(doc policy.Document, score *int, scanned time.Time, scannerConfigured bool, insecureSince *time.Time) Standing {
	s := Standing{Score: score, Threshold: doc.MinSecurityScore, Scanned: scanned}

	if doc.MinSecurityScore <= 0 {
		s.Verdict = VerdictOK
		return s
	}
	if !scannerConfigured {
		s.Verdict = VerdictInert
		return s
	}
	if score == nil {
		s.Verdict = VerdictUnscanned
		return s
	}
	if *score >= doc.MinSecurityScore {
		s.Verdict = VerdictOK
		return s
	}

	s.Verdict = VerdictInsecure
	if doc.StopsInsecureApps() && insecureSince != nil {
		s.StopAt = insecureSince.Add(time.Duration(doc.GraceHours()) * time.Hour)
	}
	return s
}

// Deployable reports whether a standing permits a deploy (R-314).
//
// Unscanned blocks: with a threshold in force, "we have never looked" is not a
// pass. Inert does not: an installation with no scanner has nothing to look
// with, and a policy that blocks every deploy because a component is missing is
// worse than one that is visibly off.
func (s Standing) Deployable() bool {
	return s.Verdict == VerdictOK || s.Verdict == VerdictInert
}

// Clock is the time source, so the grace period is testable without sleeping.
type Clock interface{ Now() time.Time }

// Due reports whether an insecure app's grace has run out.
func Due(clock Clock, doc policy.Document, insecureSince *time.Time) bool {
	if !doc.StopsInsecureApps() || insecureSince == nil {
		return false
	}
	deadline := insecureSince.Add(time.Duration(doc.GraceHours()) * time.Hour)
	return !clock.Now().Before(deadline)
}

// Scanner is what this package needs from an adapter, narrowed to the one call.
type Scanner interface {
	Scan(ctx context.Context, req api.ScanRequest) (api.ScanResult, error)
}
