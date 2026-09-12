package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// requireControl resolves the app and checks a control-plane verb.
//
// Resolution comes first so a caller without access cannot tell a missing app
// from one they may not see — both return the same not-found.
func (s *Server) requireControl(w http.ResponseWriter, r *http.Request, verb authz.Verb) (state.App, bool) {
	appID := chi.URLParam(r, "appID")
	if !id.Is(id.App, appID) {
		Error(w, r, errs.New(errs.NotFound, "There is no app with that ID."))
		return state.App{}, false
	}

	app, found, err := s.Apps.ByID(r.Context(), appID)
	if err != nil {
		Error(w, r, err)
		return state.App{}, false
	}

	p := PrincipalFrom(r.Context())
	if !found {
		Error(w, r, errs.New(errs.NotFound, "There is no app with that ID."))
		return state.App{}, false
	}
	if err := s.Authz.CheckControl(r.Context(), p, appID, verb); err != nil {
		if errs.CodeOf(err) == errs.PermDenied || errs.CodeOf(err) == errs.PermVerbRequired {
			// Not found rather than forbidden when the caller cannot even view
			// the app: confirming existence is itself a disclosure.
			if viewErr := s.Authz.CheckControl(r.Context(), p, appID, authz.AppView); viewErr != nil {
				Error(w, r, errs.New(errs.NotFound, "There is no app with that ID."))
				return state.App{}, false
			}
		}
		Error(w, r, err)
		return state.App{}, false
	}
	return app, true
}

type createAppRequest struct {
	Name   string `json:"name"`
	Source struct {
		Type   string `json:"type"`
		URL    string `json:"url"`
		Ref    string `json:"ref"`
		Image  string `json:"image"`
		Subdir string `json:"subdir"`
	} `json:"source"`
}

var slugPattern = regexp.MustCompile(`[^a-z0-9-]+`)

// handleCreateApp starts Sequence A.
//
// app.create is install-scoped: there is no app yet to hold a verb on, which
// is exactly why it cannot go through requireControl. Sequence A step 1 has
// always called this an install-level check.
func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.AppCreate)
	if !ok {
		return
	}

	var req createAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		Error(w, r, errs.New(errs.ValidInvalid, "An app needs a name.").
			WithRemedy("Give the app a short name, for example \"team-notes\"."))
		return
	}

	// The source allowlist (R-092) is evaluated before anything touches disk.
	// There is nothing to clone yet in this phase — detection is phase 6 — but
	// the check belongs at creation, and putting it here now means the ordering
	// is already right when cloning exists.
	if s.Policy != nil {
		if err := s.Policy.AllowsSource(r.Context(), req.Source.URL); err != nil {
			s.audit(r, audit.Event{
				PrincipalKind: audit.PrincipalKind(p.Kind),
				PrincipalID:   p.ID,
				OnBehalfOf:    p.UserID,
				Action:        "app.create.denied",
				Detail:        map[string]any{"source_url": req.Source.URL},
			})
			Error(w, r, err)
			return
		}
	}

	app, err := s.Apps.Create(r.Context(), req.Name, slugify(req.Name), p.UserID, p.ID, spec.Source{
		Type:   spec.SourceType(req.Source.Type),
		URL:    req.Source.URL,
		Ref:    req.Source.Ref,
		Image:  req.Source.Image,
		Subdir: req.Source.Subdir,
	})
	if err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        "app.create",
		AppID:         app.ID,
		TargetKind:    "app",
		TargetID:      app.ID,
	})

	// Sequence A step 5: enqueue detection.
	//
	// In the background, and not with the request's context — cloning a
	// repository and trial-running it takes longer than any reasonable HTTP
	// timeout, and tying it to the connection would cancel detection the moment
	// the console navigated away. The result is written to the detections table
	// either way, which is what GET /detection reads.
	if s.Detector != nil && app.Source.Type != "" {
		go s.detectInBackground(context.WithoutCancel(r.Context()), app.ID)
	}

	// 202, not 201: the app exists but is in draft. Detection has been queued,
	// and nothing is deployed.
	JSON(w, http.StatusAccepted, app)
}

// detectInBackground runs detection for a newly created app.
//
// The context is the request's with cancellation removed, not a fresh one.
// Detaching cancellation is the point — the request returns 202 long before a
// clone and a trial run finish — but the request's values are worth keeping, so
// the log lines for this detection still carry the trace that started it.
func (s *Server) detectInBackground(parent context.Context, appID string) {
	ctx, cancel := context.WithTimeout(parent, detectionTimeout)
	defer cancel()

	if _, err := s.Detector.Detect(ctx, appID); err != nil {
		// Already recorded against the app as a failed detection, which is
		// where a user will look for it. Logged as well, because a detection
		// that fails for every app is an install problem rather than an app
		// problem, and nobody finds that by reading one app's page.
		s.Logger.Warn("detection failed", zap.String("app_id", appID), zap.Error(err))
	}
}

// detectionTimeout bounds a background detection run.
//
// Generous: it covers a clone, an auction and a trial run that may pull a base
// image over a slow connection. The cost of being too short is a detection that
// fails for a large repository on a home connection, which is exactly the user
// R-005 describes.
const detectionTimeout = 10 * time.Minute

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.Apps.ListForPrincipal(r.Context(), PrincipalFrom(r.Context()))
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"apps": apps})
}

// handleMyApps returns the launcher tiles (R-264).
//
// A different list from handleListApps, deliberately: that one is control-plane
// scoped, this one is data-plane. Two planes, two endpoints.
func (s *Server) handleMyApps(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p.Kind == authz.KindAnonymous {
		Error(w, r, errs.New(errs.AuthRequired, "You need to sign in."))
		return
	}
	apps, err := s.Apps.ListForUse(r.Context(), p)
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"apps": apps})
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	JSON(w, http.StatusOK, app)
}

func (s *Server) handlePatchApp(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		Error(w, r, errs.New(errs.ValidInvalid, "An app needs a name."))
		return
	}

	if err := s.Apps.Rename(r.Context(), app.ID, req.Name); err != nil {
		Error(w, r, err)
		return
	}
	s.auditApp(r, app.ID, "app.update")

	updated, _, err := s.Apps.ByID(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, updated)
}

// handleDeleteApp implements R-204/R-205.
//
// The 409-on-ambiguity shape is what lets the interactive prompt and the
// non-interactive default coexist without two code paths: a client that has not
// decided about backups is told to decide, and one that has says so in the
// query string.
func (s *Server) handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppDelete)
	if !ok {
		return
	}

	backup := r.URL.Query().Get("backup")
	force := r.URL.Query().Get("force") == "true"

	volumes, err := s.Apps.VolumeCount(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}

	if backup == "" && !force && volumes > 0 {
		Error(w, r, errs.New(errs.StateBackupDecisionRequired,
			"This app has storage attached. Deleting it will remove that storage unless you back it up first.").
			WithRemedy("Delete with backup=true to keep a copy, or force=true to delete without one. A backup made this way is kept until you discard it.").
			WithDetail("volume_count", volumes))
		return
	}

	// Volumes are ON DELETE RESTRICT (R-204), so they are resolved explicitly
	// rather than cascading. Snapshotting needs a runtime adapter, which is
	// phase 3 — until then, a delete that would discard data is refused rather
	// than silently doing the wrong half of the job.
	if volumes > 0 {
		if backup == "true" {
			Error(w, r, errs.New(errs.StateInvalid,
				"Backing up storage before deletion is not available yet.").
				WithRemedy("This arrives with the runtime adapters. Until then, an app with storage can only be deleted with force=true, which discards it."))
			return
		}
		if err := s.Volumes.DeleteForApp(r.Context(), app.ID); err != nil {
			Error(w, r, err)
			return
		}
	}

	if err := s.Apps.Archive(r.Context(), app.ID); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(PrincipalFrom(r.Context()).Kind),
		PrincipalID:   PrincipalFrom(r.Context()).ID,
		OnBehalfOf:    PrincipalFrom(r.Context()).UserID,
		Action:        "app.delete",
		AppID:         app.ID,
		TargetKind:    "app",
		TargetID:      app.ID,
		Detail:        map[string]any{"forced": force, "volumes_discarded": volumes},
	})
	JSON(w, http.StatusNoContent, nil)
}

// --- specs -----------------------------------------------------------------

func (s *Server) handleListSpecs(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	revs, err := s.Apps.ListRevisions(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"revisions": revs, "pinned_spec_id": app.PinnedSpecID})
}

func (s *Server) handleGetSpec(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	rev, found, err := s.revisionFromPath(r, app.ID, "rev")
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		Error(w, r, errs.New(errs.NotFound, "There is no such revision of this app's spec."))
		return
	}
	JSON(w, http.StatusOK, rev)
}

// handleCreateSpec writes a new revision. Editing produces a revision; it never
// modifies one (R-152).
func (s *Server) handleCreateSpec(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}

	var body spec.AppSpec
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The spec could not be read.").
			WithRemedy("Check that the body is valid JSON matching the spec schema."))
		return
	}

	// An imported spec is untrusted input like any other and lands as a
	// proposal requiring review, never as a live deployment (design 01 §5).
	origin := spec.OriginEdited
	if body.Origin == spec.OriginImported || body.Origin == spec.OriginManual {
		origin = body.Origin
	}

	body.SchemaVersion = spec.SchemaVersion
	body.AppID = app.ID
	if err := spec.Validate(&body); err != nil {
		Error(w, r, err)
		return
	}

	p := PrincipalFrom(r.Context())
	rev, err := s.Apps.CreateRevision(r.Context(), app.ID, &body, origin, p.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	s.auditApp(r, app.ID, "spec.create")

	JSON(w, http.StatusCreated, rev)
}

// handlePinSpec points the app at a revision. It does not deploy.
func (s *Server) handlePinSpec(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}
	rev, found, err := s.revisionFromPath(r, app.ID, "rev")
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		Error(w, r, errs.New(errs.NotFound, "There is no such revision of this app's spec."))
		return
	}

	// Validate again at pin time. A revision written by an older Pando, or
	// imported from elsewhere, can be well-formed on arrival and invalid now.
	if err := spec.Validate(rev.Body); err != nil {
		Error(w, r, err)
		return
	}

	state := app.State
	if state == "draft" {
		state = "proposed"
	}
	if err := s.Apps.Pin(r.Context(), app.ID, rev.ID, state, PrincipalFrom(r.Context()).ID); err != nil {
		Error(w, r, err)
		return
	}
	s.auditApp(r, app.ID, "spec.pin")

	updated, _, err := s.Apps.ByID(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, updated)
}

// handleDiffSpecs returns the classified diff between two revisions.
func (s *Server) handleDiffSpecs(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}

	from, found, err := s.revisionFromPath(r, app.ID, "a")
	if err != nil || !found {
		Error(w, r, orNotFound(err))
		return
	}
	to, found, err := s.revisionFromPath(r, app.ID, "b")
	if err != nil || !found {
		Error(w, r, orNotFound(err))
		return
	}

	d := spec.Compare(from.Body, to.Body)
	JSON(w, http.StatusOK, map[string]any{
		"from":                  from.Revision,
		"to":                    to.Revision,
		"class":                 d.Class(),
		"requires_confirmation": d.RequiresConfirmation(),
		"changes":               d.Changes,
	})
}

// handleExportSpec emits the spec (R-020).
//
// Safe to hand to someone: the spec holds no secret values by construction, only
// references. Nothing needs stripping here, which is the point of that design.
func (s *Server) handleExportSpec(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	if app.PinnedSpecID == "" {
		Error(w, r, errs.New(errs.StateInvalid, "This app has no pinned spec to export yet."))
		return
	}
	rev, found, err := s.Apps.RevisionByID(r.Context(), app.PinnedSpecID)
	if err != nil || !found {
		Error(w, r, orNotFound(err))
		return
	}

	s.auditApp(r, app.ID, "spec.export")
	JSON(w, http.StatusOK, rev.Body)
}

func (s *Server) revisionFromPath(r *http.Request, appID, param string) (state.Revision, bool, error) {
	raw := chi.URLParam(r, param)
	if n, err := strconv.Atoi(raw); err == nil {
		return s.Apps.RevisionByNumber(r.Context(), appID, n)
	}
	rev, found, err := s.Apps.RevisionByID(r.Context(), raw)
	if err != nil || !found {
		return rev, found, err
	}
	if rev.AppID != appID {
		// A revision ID from another app must not resolve here.
		return state.Revision{}, false, nil
	}
	return rev, true, nil
}

func orNotFound(err error) error {
	if err != nil {
		return err
	}
	return errs.New(errs.NotFound, "There is no such revision of this app's spec.")
}

func (s *Server) auditApp(r *http.Request, appID, action string) {
	p := PrincipalFrom(r.Context())
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        action,
		AppID:         appID,
		TargetKind:    "app",
		TargetID:      appID,
	})
}

func slugify(name string) string {
	s := slugPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "app"
	}
	// Slugs are used in routing and must be unique, so a suffix keeps two apps
	// with the same name from colliding on the URL as well as on the name.
	return s + "-" + strings.ToLower(id.New(id.App)[len(id.App)+1:])[:6]
}
