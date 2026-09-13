package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
)

// handleGetDetection returns the current auction result (design 04 §2.2).
//
// runners_up is part of the response on purpose: R-102 is "ask, never guess",
// and showing what else bid is how a user sees the auction rather than being
// handed a verdict.
func (s *Server) handleGetDetection(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}

	d, err := s.Detections.Get(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}

	JSON(w, http.StatusOK, detectionResponse(d))
}

// handleRerunDetection re-detects, explicitly (R-022).
//
// Explicitly is the whole point. Detection never re-runs on its own: a spec
// that changed under someone because a file moved in their repository is a spec
// they did not write, and R-098 pins the reviewed proposal precisely so that
// cannot happen.
func (s *Server) handleRerunDetection(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}
	if s.Detector == nil {
		Error(w, r, errs.New(errs.AdapterUnavailable, "Detection is not configured on this install."))
		return
	}

	p := PrincipalFrom(r.Context())
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        "detection.rerun",
		AppID:         app.ID,
		TargetKind:    "app",
		TargetID:      app.ID,
	})

	d, err := s.Detector.Detect(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, detectionResponse(d))
}

// handleDetectionDiff compares the proposal against the pinned spec (R-022).
//
// The same machinery as every other diff (design 01 §4), so a re-detection that
// would remove a volume is shown as destructive here exactly as it would be
// anywhere else.
func (s *Server) handleDetectionDiff(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}

	d, err := s.Detections.Get(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}

	proposal, err := decodeProposal(d)
	if err != nil {
		Error(w, r, err)
		return
	}

	if app.PinnedSpecID == "" {
		// Nothing pinned yet, so every part of the proposal is new. Saying so
		// is more useful than an empty diff that reads as "no changes".
		JSON(w, http.StatusOK, map[string]any{
			"app_id":     app.ID,
			"pinned":     nil,
			"changes":    nil,
			"is_first":   true,
			"draft_spec": proposal.DraftSpec,
		})
		return
	}

	rev, found, err := s.Apps.RevisionByID(r.Context(), app.PinnedSpecID)
	if err != nil || !found {
		Error(w, r, orNotFound(err))
		return
	}

	changes := spec.Compare(rev.Body, &proposal.DraftSpec)
	JSON(w, http.StatusOK, map[string]any{
		"app_id":   app.ID,
		"pinned":   rev.Revision,
		"changes":  changes,
		"is_first": false,
	})
}

type answersRequest struct {
	Answers map[string]string `json:"answers"`
}

// handleDetectionAnswers records answers to outstanding questions.
//
// R-105's workflow is a person pasting a question into the assistant that wrote
// their app and pasting the answer back, so answers arrive one or a few at a
// time and are merged rather than replacing what is there.
func (s *Server) handleDetectionAnswers(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}

	var req answersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if len(req.Answers) == 0 {
		Error(w, r, errs.New(errs.ValidInvalid, "No answers were supplied.").
			WithRemedy(`Send answers keyed by question, for example {"answers": {"primary_port": "8080"}}.`))
		return
	}

	d, err := s.Detections.Get(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	proposal, err := decodeProposal(d)
	if err != nil {
		Error(w, r, err)
		return
	}

	// An answer to a question nobody asked is a mistake worth reporting rather
	// than storing: it usually means a typo in the key, and silently accepting
	// it would leave the real question unanswered and the deploy blocked with
	// no explanation.
	known := map[string]bool{}
	for _, q := range proposal.Questions {
		known[q.Key] = true
	}
	var unknown []string
	for key := range req.Answers {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		Error(w, r, errs.Newf(errs.ValidInvalid,
			"There are no outstanding questions with these keys: %v.", unknown).
			WithDetail("outstanding", keysOf(known)).
			WithRemedy("Use the `key` from one of the questions in GET /detection."))
		return
	}

	if err := s.Detections.SaveAnswers(r.Context(), app.ID, req.Answers); err != nil {
		Error(w, r, err)
		return
	}

	updated, err := s.Detections.Get(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, detectionResponse(updated))
}

// handleAcceptDetection pins the proposal as spec revision 1 (Sequence A 13–17).
//
// Accepting does not deploy. It pins a revision and writes the grants, and the
// app moves to `proposed` — deploying is a separate, deliberate act, which is
// the assertion Sequence A makes explicitly.
func (s *Server) handleAcceptDetection(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}

	// Accepting onto an app that is already configured replaces that
	// configuration, and nothing warns about it afterwards: the new spec comes
	// from detection, so every environment variable, slot resolution and volume
	// added since is simply absent from it. The old revision survives —
	// spec_revisions is append-only (R-152) — but the app is pinned to one that
	// does not have them, and the next deploy ships that.
	//
	// Confirmed rather than refused, because re-accepting is a real thing to
	// want: the repository changed and detection should win. And confirmed on
	// the server rather than in the console, because the console is one client
	// of three (R-261) — a guard only the browser enforces is not a guard.
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if app.PinnedSpecID != "" && !req.Confirm {
		Error(w, r, errs.New(errs.ValidInvalid,
			"This app is already configured, and accepting again replaces that configuration with what detection found.").
			WithDetail("pinned_spec_id", app.PinnedSpecID).
			WithRemedy("Anything set since — environment variables, dependencies, storage — is not carried over. Send confirm: true to replace it anyway."))
		return
	}

	d, err := s.Detections.Get(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	proposal, err := decodeProposal(d)
	if err != nil {
		Error(w, r, err)
		return
	}

	if proposal.Blocked != nil {
		Error(w, r, proposal.Blocked)
		return
	}

	// Every question a person still has to answer is a blocker. R-104: Pando
	// asks only when it genuinely cannot proceed, so an outstanding question is
	// by definition something without which the app cannot run.
	if outstanding := unanswered(proposal, d.Answers); len(outstanding) > 0 {
		Error(w, r, errs.Newf(errs.StateInvalid,
			"This proposal still has %d unanswered question(s).", len(outstanding)).
			WithDetail("questions", outstanding).
			WithRemedy("Answer them with POST /detection/answers, then accept again."))
		return
	}

	draft := proposal.WithAnswers(d.Answers)
	draft.AppID = app.ID

	p := PrincipalFrom(r.Context())
	rev, err := s.Apps.CreateRevision(r.Context(), app.ID, &draft, spec.OriginDetected, p.UserID)
	if err != nil {
		Error(w, r, err)
		return
	}

	// Pinning moves the app to `proposed`, not to anything running. Accepting a
	// proposal is not deploying it — that is a separate, deliberate act, and
	// Sequence A asserts it.
	if err := s.Apps.Pin(r.Context(), app.ID, rev.ID, state.StateProposed, p.UserID); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        "spec.pin",
		AppID:         app.ID,
		TargetKind:    "spec_revision",
		TargetID:      rev.ID,
		Detail:        map[string]any{"revision": rev.Revision, "origin": string(spec.OriginDetected)},
	})

	JSON(w, http.StatusOK, map[string]any{
		"app_id":   app.ID,
		"spec_id":  rev.ID,
		"revision": rev.Revision,
		"state":    state.StateProposed,
		"deployed": false,
	})
}

// --- helpers ---------------------------------------------------------------

func detectionResponse(d state.Detection) map[string]any {
	return map[string]any{
		"status":     d.Status,
		"detection":  json.RawMessage(d.Body),
		"answers":    d.Answers,
		"commit":     d.Commit,
		"started_at": d.StartedAt,
		"updated_at": d.UpdatedAt,
	}
}

func decodeProposal(d state.Detection) (detect.Proposal, error) {
	var p detect.Proposal
	if len(d.Body) == 0 {
		return p, errs.New(errs.StateInvalid, "Detection has not finished for this app yet.").
			WithRemedy("Wait for detection to finish, then try again.")
	}
	if err := json.Unmarshal(d.Body, &p); err != nil {
		return p, errs.Wrap(errs.Internal, "The stored detection result could not be read.", err)
	}
	return p, nil
}

// unanswered returns the questions a person still has to answer.
//
// Deferred questions are excluded: those belong to the trial run (R-097), and
// one still marked deferred by the time a proposal is accepted was resolved by
// observation rather than left hanging.
func unanswered(p detect.Proposal, answers map[string]string) []string {
	var out []string
	for _, q := range detect.Asked(p.Questions) {
		if answers[q.Key] == "" {
			out = append(out, q.Key)
		}
	}
	return out
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Detector runs detection for an app.
//
// An interface rather than the concrete job, so the HTTP layer does not have to
// know how source is fetched or which adapters are involved — and so a test can
// assert what the endpoints do without a network, a builder or a daemon.
type Detector interface {
	Detect(ctx context.Context, appID string) (state.Detection, error)
}
