package security

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/policy"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
)

// Service runs scans and answers "where does this app stand".
//
// The adapter produces findings, this produces the score, records it, and tells
// the audit log. Nothing here knows what a CVE is and nothing in the adapter
// knows what a threshold is (R-251).
type Service struct {
	Scans       *state.Scans
	Deployments *state.Deployments
	Registry    *api.Registry
	Policy      PolicyLoader
	Auditor     Auditor
	Logger      *zap.Logger
}

// PolicyLoader is host policy, narrowed to the one call this needs.
type PolicyLoader interface {
	Document(ctx context.Context) (policy.Document, error)
}

// Auditor records what happened (R-319).
type Auditor interface {
	Write(ctx context.Context, e audit.Event) error
}

// Report is an app's security standing, with the scan behind it.
type Report struct {
	Standing Standing      `json:"standing"`
	Scan     *state.Scan   `json:"scan,omitempty"`
	Counts   Counts        `json:"counts"`
	Worst    []api.Finding `json:"worst,omitempty"`

	// Scanner is what would run, or what did. Empty when the installation has
	// none configured, which is what makes a threshold inert (R-317).
	Scanner string `json:"scanner,omitempty"`
}

// Configured reports whether this installation can scan at all.
func (s *Service) Configured() (string, bool) {
	if s == nil || s.Registry == nil {
		return "", false
	}
	_, ref, ok := s.Registry.DefaultScanner()
	return ref, ok
}

// Scan runs a scan and records it, whatever the outcome.
//
// A failure is recorded as a scan with no score rather than swallowed: a
// scanner that cannot run is a thing somebody has to fix, and silence would
// leave an unscannable app looking clean (R-318).
func (s *Service) Scan(ctx context.Context, req api.ScanRequest, principal audit.Event) (state.Scan, error) {
	scanner, ref, ok := s.Registry.DefaultScanner()
	if !ok {
		return state.Scan{}, errs.New(errs.AdapterUnavailable,
			"This installation has no security scanner configured, so there is nothing to scan with.").
			WithRemedy("Configure a scanner adapter, or ask whoever administers this installation to.")
	}

	result, err := scanner.Scan(ctx, req)
	if err != nil {
		recorded, recordErr := s.Scans.Record(ctx, state.Scan{
			AppID: req.AppID, SpecID: req.SpecID, ScannerRef: ref,
			Error: messageOf(err), RanAt: time.Now().UTC(),
		})
		if recordErr != nil {
			return state.Scan{}, recordErr
		}
		s.audit(ctx, principal, "app.scan.failed", req.AppID, map[string]any{
			"scanner": ref, "reason": messageOf(err),
		})
		return recorded, err
	}

	score := Score(result.Findings)
	recorded, err := s.Scans.Record(ctx, state.Scan{
		AppID:      req.AppID,
		SpecID:     req.SpecID,
		ScannerRef: ref,
		Scanner:    result.Scanner,
		Score:      &score,
		Findings:   result.Findings,
		RanAt:      result.Ran,
	})
	if err != nil {
		return state.Scan{}, err
	}

	counts := Count(result.Findings)
	s.audit(ctx, principal, "app.scan", req.AppID, map[string]any{
		"scanner":  ref,
		"score":    score,
		"critical": counts.Critical,
		"high":     counts.High,
		"medium":   counts.Medium,
		"low":      counts.Low + counts.Unknown,
		"spec_id":  req.SpecID,
	})
	return recorded, nil
}

// Report is where an app stands right now.
//
// The scan of the revision it is running, not the newest scan of the app: an
// app whose newest scan is of a revision it is not running has not been scanned
// in the sense that matters.
func (s *Service) Report(ctx context.Context, appID, specID string) (Report, error) {
	doc, err := s.Policy.Document(ctx)
	if err != nil {
		return Report{}, err
	}

	ref, configured := s.Configured()

	scan, found, err := s.Scans.Latest(ctx, appID, specID)
	if err != nil {
		return Report{}, err
	}

	security, _, err := s.Scans.SecurityStateFor(ctx, appID)
	if err != nil {
		return Report{}, err
	}

	report := Report{Scanner: ref}
	if found {
		report.Scan = &scan
		report.Counts = Count(scan.Findings)
		report.Worst = Worst(scan.Findings, 5)
		report.Standing = Evaluate(doc, scan.Score, scan.RanAt, configured, security.InsecureSince)
		return report, nil
	}

	report.Standing = Evaluate(doc, nil, time.Time{}, configured, security.InsecureSince)
	return report, nil
}

// Allows reports whether this revision may be deployed (R-314).
//
// Called from the deploy path with the score of the image that was just built,
// and from the planner with whatever is already known about the revision.
func (s *Service) Allows(ctx context.Context, appID, specID string) (Standing, error) {
	doc, err := s.Policy.Document(ctx)
	if err != nil {
		return Standing{}, err
	}
	_, configured := s.Configured()

	scan, found, err := s.Scans.Latest(ctx, appID, specID)
	if err != nil {
		return Standing{}, err
	}
	if !found {
		return Evaluate(doc, nil, time.Time{}, configured, nil), nil
	}
	return Evaluate(doc, scan.Score, scan.RanAt, configured, nil), nil
}

// Refusal is the plan-time error for an app that may not be deployed.
//
// Written here rather than at the call site so the message is the same wherever
// the refusal comes from, and so the three findings that cost the most travel
// with it — R-105: an error somebody can act on, or paste into the assistant
// that wrote the app.
func Refusal(standing Standing, worst []api.Finding) error {
	if standing.Verdict == VerdictUnscanned {
		return errs.New(errs.PlanSecurityBelowThreshold,
			"This installation requires a security score and this app has never been scanned.").
			WithRemedy("Scan it from the app's settings, or ask whoever administers this installation about the requirement.").
			WithDetail("threshold", standing.Threshold)
	}

	score := 0
	if standing.Score != nil {
		score = *standing.Score
	}
	err := errs.Newf(errs.PlanSecurityBelowThreshold,
		"This app scores %d and this installation requires at least %d.", score, standing.Threshold).
		WithRemedy("Fix the findings below and deploy again, or ask whoever administers this installation to change the requirement.").
		WithDetail("score", score).
		WithDetail("threshold", standing.Threshold)

	if len(worst) > 0 {
		summary := make([]map[string]string, 0, len(worst))
		for _, f := range worst {
			summary = append(summary, map[string]string{
				"id": f.ID, "severity": string(f.Severity), "title": f.Title, "target": f.Target, "fix": f.Fix,
			})
		}
		err = err.WithDetail("findings", summary)
	}
	return err
}

func (s *Service) audit(ctx context.Context, base audit.Event, action, appID string, detail map[string]any) {
	if s.Auditor == nil {
		return
	}
	event := base
	event.Action = action
	event.AppID = appID
	event.TargetKind = "app"
	event.TargetID = appID
	event.Detail = detail

	if err := s.Auditor.Write(ctx, event); err != nil && s.Logger != nil {
		s.Logger.Warn("could not record a security event", zap.Error(err), zap.String("action", action))
	}
}

func messageOf(err error) string {
	if e := errs.As(err); e != nil {
		return e.Message
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
