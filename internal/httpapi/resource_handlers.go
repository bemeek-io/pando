package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
	"github.com/bemeek-io/pando/internal/secret"
)

// Slots, volumes, secret values and adapter configuration (design 04 §2.4,
// §2.8). R-030 makes Slot and Volume first-class objects; these are the
// endpoints that make them reachable.

// handleListSlots returns an app's declared dependencies and how each is filled.
func (s *Server) handleListSlots(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}

	appSpec, err := s.pinnedOrLatest(r, app)
	if err != nil {
		Error(w, r, err)
		return
	}
	if appSpec == nil {
		JSON(w, http.StatusOK, map[string]any{"slots": []any{}})
		return
	}
	JSON(w, http.StatusOK, map[string]any{"slots": appSpec.Slots})
}

// handleSetSlot fills a slot (R-131), producing a new spec revision.
//
// A new revision rather than an edit, because R-152 makes revisions append-only
// and filling a slot changes how the app runs. The revision is not pinned:
// choosing how a dependency is satisfied and deciding to deploy it are separate
// acts, and conflating them would deploy on a dropdown change.
func (s *Server) handleSetSlot(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}

	var req struct {
		Mode   string `json:"mode"`
		Target string `json:"target"`
		Value  string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}

	appSpec, err := s.pinnedOrLatest(r, app)
	if err != nil {
		Error(w, r, err)
		return
	}
	if appSpec == nil {
		Error(w, r, errs.New(errs.StateInvalid, "This app has no configuration yet.").
			WithRemedy("Let detection finish, or write a spec first."))
		return
	}

	key := chi.URLParam(r, "key")
	found := false
	updated := *appSpec
	updated.Slots = append([]spec.Slot(nil), appSpec.Slots...)

	for i := range updated.Slots {
		if updated.Slots[i].Key != key {
			continue
		}
		found = true

		switch req.Mode {
		case string(spec.ResolutionProvisioned):
			updated.Slots[i].Resolution = &spec.Resolution{Mode: spec.ResolutionProvisioned}
		case string(spec.ResolutionBound):
			if req.Target == "" {
				Error(w, r, errs.New(errs.ValidInvalid, "Binding a dependency needs something to bind to."))
				return
			}
			updated.Slots[i].Resolution = &spec.Resolution{Mode: spec.ResolutionBound, Target: req.Target}
		case string(spec.ResolutionLiteral):
			if req.Value == "" {
				Error(w, r, errs.New(errs.ValidInvalid, "A literal needs a value."))
				return
			}
			// The value is stored as a secret, not inline. That is what keeps
			// "an exported spec is safe to hand to someone" true without a
			// special case for this one field.
			secretKey := "slot_" + key
			if err := s.Secrets.Put(r.Context(), app.ID, secretKey, secret.New(req.Value)); err != nil {
				Error(w, r, err)
				return
			}
			updated.Slots[i].Resolution = &spec.Resolution{
				Mode: spec.ResolutionLiteral, SecretRef: secretKey,
			}
		default:
			Error(w, r, errs.New(errs.ValidInvalid,
				"A dependency is either provisioned by Pando, bound to something existing, or given a value.").
				WithRemedy(`Send mode as "provisioned", "bound" or "literal".`))
			return
		}
	}

	if !found {
		Error(w, r, errs.Newf(errs.NotFound, "This app has no dependency called %q.", key))
		return
	}

	p := PrincipalFrom(r.Context())
	rev, err := s.Apps.CreateRevision(r.Context(), app.ID, &updated, spec.OriginEdited, p.ID)
	if err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "app.slot.set", AppID: app.ID, TargetKind: "slot", TargetID: key,
		Detail: map[string]any{"mode": req.Mode, "spec_revision": rev.Revision},
	})
	JSON(w, http.StatusOK, map[string]any{
		"key": key, "mode": req.Mode, "spec_revision": rev.Revision,
		"note": "Saved as a new revision. Deploy to apply it.",
	})
}

// handleListVolumes returns an app's storage.
func (s *Server) handleListVolumes(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	if s.BundleSource == nil {
		Error(w, r, errs.New(errs.Internal, "Storage is not readable on this installation."))
		return
	}

	volumes, err := s.BundleSource.VolumesForApp(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(volumes))
	for _, v := range volumes {
		out = append(out, map[string]any{
			"id": v.VolumeID, "adapter_ref": v.AdapterRef, "handle": v.Handle,
		})
	}
	JSON(w, http.StatusOK, map[string]any{"volumes": out})
}

// handleCreateVolume adds storage to an app, as a new spec revision.
//
// A spec change rather than a direct call to the runtime, because R-020 makes
// the spec the sole record of how an app runs: a volume created behind the
// spec's back would exist until the next deploy and then quietly not be
// mounted. The volume itself is created by the runtime when the revision is
// deployed, which is also when it gets recorded (R-030).
func (s *Server) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}

	var req struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		Workload string `json:"workload"`
		SizeHint int64  `json:"size_hint_bytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if req.Name == "" {
		Error(w, r, errs.New(errs.ValidInvalid, "Storage needs a name.").
			WithRemedy(`Name it after what it holds, for example "data".`))
		return
	}
	if req.Path == "" {
		Error(w, r, errs.New(errs.ValidInvalid, "Storage needs a path inside the app.").
			WithRemedy("For example /app/data — wherever the app writes files it must keep."))
		return
	}

	appSpec, err := s.pinnedOrLatest(r, app)
	if err != nil {
		Error(w, r, err)
		return
	}
	if appSpec == nil {
		Error(w, r, errs.New(errs.StateInvalid, "This app has no configuration yet.").
			WithRemedy("Let detection finish, or write a spec first."))
		return
	}

	updated := *appSpec
	updated.Volumes = append([]spec.Volume(nil), appSpec.Volumes...)
	for _, v := range updated.Volumes {
		if v.Name == req.Name {
			Error(w, r, errs.Newf(errs.ValidInvalid, "This app already has storage called %q.", req.Name))
			return
		}
	}

	volumeID := id.New(id.Volume)
	updated.Volumes = append(updated.Volumes, spec.Volume{
		ID: volumeID, Name: req.Name, Declared: spec.VolumeFromUser, SizeHintBytes: req.SizeHint,
	})

	// Mounted into the named workload, or the primary one. An unmounted volume
	// is storage nothing can write to, which is not what anyone asking for
	// storage means.
	updated.Workloads = append([]spec.Workload(nil), appSpec.Workloads...)
	mounted := false
	for i := range updated.Workloads {
		isTarget := req.Workload == "" && updated.Workloads[i].Primary
		if req.Workload != "" && updated.Workloads[i].Name == req.Workload {
			isTarget = true
		}
		if !isTarget {
			continue
		}
		updated.Workloads[i].Mounts = append(
			append([]spec.Mount(nil), updated.Workloads[i].Mounts...),
			spec.Mount{VolumeID: volumeID, Path: req.Path})
		mounted = true
	}
	if !mounted {
		Error(w, r, errs.Newf(errs.ValidInvalid,
			"This app has no workload called %q to attach storage to.", req.Workload))
		return
	}

	p := PrincipalFrom(r.Context())
	rev, err := s.Apps.CreateRevision(r.Context(), app.ID, &updated, spec.OriginEdited, p.ID)
	if err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "app.volume.add", AppID: app.ID, TargetKind: "volume", TargetID: volumeID,
		Detail: map[string]any{"name": req.Name, "path": req.Path, "spec_revision": rev.Revision},
	})
	JSON(w, http.StatusCreated, map[string]any{
		"id": volumeID, "name": req.Name, "path": req.Path, "spec_revision": rev.Revision,
		"note": "Saved as a new revision. Deploy to create it.",
	})
}

// handleReadSecretValue returns a secret's value (R-083).
//
// Its own verb, deliberately separable from writing one: rotating a credential
// and reading it are different levels of trust, and secrets are write-only for
// anyone below owner. Reading one is audited as its own action, because "who
// read this" is the question asked after a leak.
func (s *Server) handleReadSecretValue(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSecretsRead)
	if !ok {
		return
	}

	key := chi.URLParam(r, "key")
	value, err := s.Secrets.Get(r.Context(), app.ID, key)
	if err != nil {
		Error(w, r, err)
		return
	}

	p := PrincipalFrom(r.Context())
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "app.secret.read", AppID: app.ID, TargetKind: "secret", TargetID: key,
	})

	// Revealed deliberately. secret.Value redacts itself in every marshaler, so
	// this is the one place that has to opt in — and there is no path where the
	// value leaks by somebody forgetting to.
	JSON(w, http.StatusOK, map[string]any{"key": key, "value": value.Reveal()})
}

// handleCreateAdapter configures an adapter instance.
func (s *Server) handleCreateAdapter(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallAdaptersManage)
	if !ok {
		return
	}

	var req struct {
		ID        string          `json:"id"`
		Category  string          `json:"category"`
		Kind      string          `json:"kind"`
		Name      string          `json:"name"`
		Config    json.RawMessage `json:"config"`
		IsDefault bool            `json:"is_default"`
		Enabled   *bool           `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if req.ID == "" || req.Category == "" || req.Kind == "" {
		Error(w, r, errs.New(errs.ValidInvalid, "An adapter needs an id, a category and a kind."))
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if err := s.Adapters.Upsert(r.Context(), state.AdapterConfig{
		ID: req.ID, Category: req.Category, Kind: req.Kind, Name: req.Name,
		Config: req.Config, IsDefault: req.IsDefault, Enabled: enabled,
	}); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "adapter.configure", TargetKind: "adapter", TargetID: req.ID,
		Detail: map[string]any{"category": req.Category, "kind": req.Kind, "default": req.IsDefault},
	})

	// Adapters are registered at startup from compiled-in packages (R-253), so
	// a newly configured one is not live until Pando restarts. Said plainly
	// rather than implied: a configuration that appears to take effect and does
	// not is worse than one that says when it will.
	JSON(w, http.StatusCreated, map[string]any{
		"id":   req.ID,
		"note": "Saved. Pando registers adapters at startup, so restart it for this to take effect.",
	})
}

// pinnedOrLatest returns the app's pinned spec, or its newest revision.
//
// Pinned first: that is what runs (R-098). The newest is the fallback for an
// app still in review, where there is a proposal but nothing pinned yet.
func (s *Server) pinnedOrLatest(r *http.Request, app state.App) (*spec.AppSpec, error) {
	if app.PinnedSpecID != "" {
		rev, found, err := s.Apps.RevisionByID(r.Context(), app.PinnedSpecID)
		if err != nil {
			return nil, err
		}
		if found {
			return rev.Body, nil
		}
	}

	revisions, err := s.Apps.ListRevisions(r.Context(), app.ID)
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, nil
	}
	return revisions[0].Body, nil
}
