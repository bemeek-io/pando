package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/log"
	"github.com/bemeek-io/pando/internal/secret"
)

// Small helpers so handlers name adapter types in one place.
func apiBundleRef(appID string) api.BundleRef { return api.BundleRef{BundleID: appID} }
func apiWorkloadRef(appID, workload string) api.WorkloadRef {
	return api.WorkloadRef{BundleID: appID, Workload: workload}
}
func apiLogOptions(follow bool, tail int) api.LogOptions {
	return api.LogOptions{Follow: follow, Tail: tail}
}

type deployRequest struct {
	SpecRevision int    `json:"spec_revision"`
	Trigger      string `json:"trigger"`
}

// handleDeploy starts a deployment.
//
// Returns 202 and runs the pipeline in the background: a build can take minutes,
// and holding the request open for it would make every client's timeout a
// deployment timeout. The deployment ID comes back immediately and its logs
// stream from their own endpoint.
func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppDeploy)
	if !ok {
		return
	}

	var req deployRequest
	idempotencyKey, err := decodeWithKey(r, &req)
	if err != nil {
		Error(w, r, err)
		return
	}

	// A retry of a deploy that already happened replays the first answer rather
	// than deploying again. This is what makes the endpoint safe to give an
	// agent (R-262): an agent retries on a timeout, and a deploy resolves a ref
	// — which means cloning — so it regularly outlasts a client's patience.
	if s.replayed(w, r, idempotencyKey, "POST /apps/{id}/deployments") {
		return
	}

	// Two deploys racing on one bundle is how an app ends up in a state neither
	// intended. Refused rather than queued — a queue on a fast-moving branch
	// produces a backlog nobody wants (R-141's reasoning applies here too).
	inFlight, err := s.Deployments.InFlight(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if inFlight {
		Error(w, r, errs.New(errs.StateInvalid, "This app is already deploying.").
			WithRemedy("Wait for the current deploy to finish, then try again."))
		return
	}

	rev, err := s.revisionToDeploy(r, app, req.SpecRevision)
	if err != nil {
		Error(w, r, err)
		return
	}

	p := PrincipalFrom(r.Context())

	// Resolving a ref to a commit is an explicit act that produces a revision,
	// never something the deploy does on its own (design 01 §2.1).
	prepared, err := s.Deployer.PrepareRevision(r.Context(), rev, p.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if prepared.ID != rev.ID {
		s.auditApp(r, app.ID, "spec.create")
	}

	// The plan runs before anything is created, so a deploy that cannot succeed
	// is refused here rather than part-way through.
	if _, err := s.Planner.Check(r.Context(), prepared.Body); err != nil {
		Error(w, r, err)
		return
	}

	trigger := state.TriggerManual
	if req.Trigger == state.TriggerRollback {
		trigger = state.TriggerRollback
	}

	dep, err := s.Deployments.Create(r.Context(), app.ID, prepared.ID, trigger, p.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if err := s.Apps.SetState(r.Context(), app.ID, state.StateDeploying); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        "app.deploy",
		AppID:         app.ID,
		TargetKind:    "deployment",
		TargetID:      dep.ID,
		Detail:        map[string]any{"spec_revision": prepared.Revision, "trigger": trigger},
	})

	s.remember(r, idempotencyKey, "POST /apps/{id}/deployments", http.StatusAccepted, dep)

	// Detached from the request context deliberately: a client that disconnects
	// must not cancel a deploy that is already changing things.
	runCtx := log.Into(context.WithoutCancel(r.Context()), log.From(r.Context()))
	go func() {
		if err := s.Deployer.Run(runCtx, dep, prepared); err != nil {
			log.From(runCtx).Warn("deployment ended in failure", zap.Error(err))
		}
	}()

	JSON(w, http.StatusAccepted, dep)
}

func (s *Server) revisionToDeploy(r *http.Request, app state.App, requested int) (state.Revision, error) {
	if requested > 0 {
		rev, found, err := s.Apps.RevisionByNumber(r.Context(), app.ID, requested)
		if err != nil {
			return state.Revision{}, err
		}
		if !found {
			return state.Revision{}, errs.Newf(errs.NotFound, "This app has no revision %d.", requested)
		}
		return rev, nil
	}

	if app.PinnedSpecID == "" {
		return state.Revision{}, errs.New(errs.StateInvalid, "This app has no spec to deploy yet.").
			WithRemedy("Write a spec for the app and pin it first.")
	}
	rev, found, err := s.Apps.RevisionByID(r.Context(), app.PinnedSpecID)
	if err != nil {
		return state.Revision{}, err
	}
	if !found {
		return state.Revision{}, errs.New(errs.NotFound, "This app's pinned spec is missing.")
	}
	return rev, nil
}

// handleRollback deploys a previous revision (R-152).
//
// Rollback is repointing at a revision that provably existed — the same
// machinery as any other deploy, with a different trigger recorded.
func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppDeploy)
	if !ok {
		return
	}

	var req struct {
		To int `json:"to"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.To <= 0 {
		revs, err := s.Apps.ListRevisions(r.Context(), app.ID)
		if err != nil {
			Error(w, r, err)
			return
		}
		// The previous revision that was actually live. A revision that was
		// never deployed is not something to roll back to.
		for _, rev := range revs {
			if rev.ID != app.PinnedSpecID && rev.EverPinned {
				req.To = rev.Revision
				break
			}
		}
		if req.To == 0 {
			Error(w, r, errs.New(errs.StateInvalid, "There is no earlier version of this app to go back to."))
			return
		}
	}

	body, _ := json.Marshal(deployRequest{SpecRevision: req.To, Trigger: state.TriggerRollback})
	r.Body = io.NopCloser(bytes.NewReader(body))
	s.handleDeploy(w, r)
}

func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	deps, err := s.Deployments.ListForApp(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"deployments": deps})
}

func (s *Server) handleGetDeployment(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	dep, found, err := s.Deployments.ByID(r.Context(), chi.URLParam(r, "depID"))
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found || dep.AppID != app.ID {
		Error(w, r, errs.New(errs.NotFound, "There is no such deploy for this app."))
		return
	}
	JSON(w, http.StatusOK, dep)
}

// handleDeploymentLogs streams build output as server-sent events.
//
// Unbuffered and flushed per line (R-170): a build log that arrives in one lump
// at the end is not a live log, and the whole point is watching a build happen.
func (s *Server) handleDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppLogsRead)
	if !ok {
		return
	}

	depID := chi.URLParam(r, "depID")
	dep, found, err := s.Deployments.ByID(r.Context(), depID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found || dep.AppID != app.ID {
		Error(w, r, errs.New(errs.NotFound, "There is no such deploy for this app."))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	backlog, updates, cancel := s.Logs.Follow(depID)
	defer cancel()

	for _, line := range backlog {
		// G705: not an HTML context. The response is text/event-stream, set
		// above before anything is written, and a browser never parses an SSE
		// frame as markup. The console renders these lines as text.
		fmt.Fprintf(w, "data: %s\n\n", line) //nolint:gosec
	}
	_ = rc.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case line, open := <-updates:
			if !open {
				fmt.Fprint(w, "event: end\ndata: \n\n")
				_ = rc.Flush()
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			_ = rc.Flush()
		}
	}
}

// --- secrets ---------------------------------------------------------------

// handleListSecrets returns keys and metadata only, never values.
func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	keys, err := s.Secrets.Keys(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"secrets": keys})
}

// handlePutSecret stores or rotates a value. Requires app.secrets.write, which
// is deliberately separable from app.secrets.read (R-083).
func (s *Server) handlePutSecret(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSecretsWrite)
	if !ok {
		return
	}

	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}

	key := chi.URLParam(r, "key")
	if err := s.Secrets.Put(r.Context(), app.ID, key, secret.New(req.Value)); err != nil {
		Error(w, r, err)
		return
	}

	// The key is audited; the value is not, and cannot be — it is a
	// secret.Value everywhere it travels.
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(PrincipalFrom(r.Context()).Kind),
		PrincipalID:   PrincipalFrom(r.Context()).ID,
		OnBehalfOf:    PrincipalFrom(r.Context()).UserID,
		Action:        "secret.write",
		AppID:         app.ID,
		TargetKind:    "secret",
		TargetID:      key,
	})
	JSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSecretsWrite)
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	if err := s.Secrets.Delete(r.Context(), app.ID, key); err != nil {
		Error(w, r, err)
		return
	}
	s.auditApp(r, app.ID, "secret.delete")
	JSON(w, http.StatusNoContent, nil)
}

// --- app logs and status ---------------------------------------------------

func (s *Server) handleAppStatus(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}

	body := map[string]any{
		"app_id":        app.ID,
		"state":         app.State,
		"desired_state": app.DesiredState,
	}

	if app.PinnedSpecID != "" {
		rev, found, err := s.Apps.RevisionByID(r.Context(), app.PinnedSpecID)
		if err == nil && found {
			if runtime, ok := s.Registry.Runtime(rev.Body.Runtime.AdapterRef); ok {
				observed, err := runtime.Observe(r.Context(), apiBundleRef(app.ID))
				if err != nil {
					// An adapter being unreachable is a platform problem, not
					// app failure (design 05 §2). Reported as such rather than
					// presented as the app being broken.
					body["observability"] = "unreachable"
				} else {
					body["workloads"] = workloadStatuses(rev.Body, observed)
				}
			}
			body["revision"] = rev.Revision
		}
	}
	JSON(w, http.StatusOK, body)
}

// WorkloadStatus is one part of an app, as it is right now.
//
// The adapter's own struct used to be serialized straight out, which put Go
// field names on the wire — `Running`, `RestartCount` — in an API that is
// snake_case everywhere else, and left the console reading a shape nothing
// documented. It is the product (R-261), so it gets a type.
type WorkloadStatus struct {
	Name    string `json:"name"`
	Primary bool   `json:"primary"`

	Present bool `json:"present"`
	Running bool `json:"running"`

	// Restarting is the crash loop, which is the thing somebody looking at a
	// degraded app most needs to see. A container that exits and is restarted
	// by the runtime is "running" at almost every instant Pando looks at it.
	Restarting   bool `json:"restarting"`
	RestartCount int  `json:"restart_count"`

	// Healthy is null when the workload declares no health check: no signal is
	// not the same as unhealthy (R-221).
	Healthy  *bool  `json:"healthy"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Image    string `json:"image,omitempty"`

	StartedAt *time.Time `json:"started_at,omitempty"`
}

// workloadStatuses pairs what the runtime sees with what the spec declares.
func workloadStatuses(s *spec.AppSpec, observed api.ObservedBundle) []WorkloadStatus {
	primary, _ := s.PrimaryWorkload()

	found := make(map[string]api.ObservedWorkload, len(observed.Workloads))
	for _, w := range observed.Workloads {
		found[w.Name] = w
	}

	out := make([]WorkloadStatus, 0, len(observed.Workloads))
	add := func(name string, w api.ObservedWorkload, declared bool) {
		status := WorkloadStatus{
			Name:         name,
			Primary:      declared && name == primary.Name,
			Present:      w.Present,
			Running:      w.Running,
			Restarting:   w.Restarting,
			RestartCount: w.RestartCount,
			Healthy:      w.Healthy,
			ExitCode:     w.ExitCode,
			Image:        w.ImageDigest,
		}
		if !w.StartedAt.IsZero() {
			at := w.StartedAt
			status.StartedAt = &at
		}
		out = append(out, status)
	}

	// The spec's order first, so the app's own parts read in the order its
	// author wrote them, then anything else the runtime is running for this
	// app — a provisioned database, which is part of what is running and would
	// otherwise be invisible.
	for _, w := range s.Workloads {
		add(w.Name, found[w.Name], true)
		delete(found, w.Name)
	}
	for _, w := range observed.Workloads {
		if _, still := found[w.Name]; still {
			add(w.Name, w, false)
			delete(found, w.Name)
		}
	}
	return out
}

func (s *Server) handleAppLogs(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppLogsRead)
	if !ok {
		return
	}
	if app.PinnedSpecID == "" {
		Error(w, r, errs.New(errs.StateInvalid, "This app is not running yet."))
		return
	}

	rev, found, err := s.Apps.RevisionByID(r.Context(), app.PinnedSpecID)
	if err != nil || !found {
		Error(w, r, orNotFound(err))
		return
	}
	runtime, ok := s.Registry.Runtime(rev.Body.Runtime.AdapterRef)
	if !ok {
		Error(w, r, errs.New(errs.AdapterUnavailable, "The app's runtime is not configured."))
		return
	}

	workload := r.URL.Query().Get("workload")
	if workload == "" {
		if primary, ok := rev.Body.PrimaryWorkload(); ok {
			workload = primary.Name
		}
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail == 0 {
		tail = 200
	}

	rc, err := runtime.Logs(r.Context(), apiWorkloadRef(app.ID, workload), apiLogOptions(false, tail))
	if err != nil {
		Error(w, r, err)
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}
