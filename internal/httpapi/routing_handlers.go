package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/trypando/pando/internal/core/address"
	"github.com/trypando/pando/internal/core/audit"
	"github.com/trypando/pando/internal/core/authz"
	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/errs"
)

// handleSetRouting changes where a configured app is reached (R-162, R-163,
// R-165).
//
// A spec revision like any other edit (R-152): it does not deploy, and the
// next deploy ships it. Two things make it more than a field edit, and both
// are here rather than in the console so every client meets them (R-261).
// Choosing a mode the routing adapter does not default to needs
// app.routing.override (R-163). And a change moves the app — the old address
// stops working and bookmarks to it break — so it needs `confirm`, the
// confirmation design 01 §6 asks of a destructive change.
func (s *Server) handleSetRouting(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}
	if s.Address == nil {
		Error(w, r, errs.New(errs.Internal, "Changing an app's address is not set up on this installation."))
		return
	}

	var req struct {
		address.Request
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request could not be read.").
			WithRemedy(`Send JSON such as {"adapter_ref": "rte_cloudflare", "hostname": "notes.example.com", "confirm": true}.`))
		return
	}

	if app.PinnedSpecID == "" {
		Error(w, r, errs.New(errs.ValidInvalid, "This app isn't configured yet, so it has no address to change.").
			WithRemedy("Set its hostname while reviewing its plan, before it is first configured."))
		return
	}
	pinned, found, err := s.Apps.RevisionByID(r.Context(), app.PinnedSpecID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found || pinned.Body == nil {
		Error(w, r, errs.New(errs.Internal, "This app's configuration could not be read."))
		return
	}

	// The newest revision, not only the pinned one: an address saved and not
	// yet deployed is the one being changed, and building on the pinned spec
	// would drop whatever else was edited since.
	base := pinned
	if list, err := s.Apps.ListRevisions(r.Context(), app.ID); err == nil && len(list) > 0 && list[0].Revision > pinned.Revision {
		if latest, ok, err := s.Apps.RevisionByID(r.Context(), list[0].ID); err == nil && ok && latest.Body != nil {
			base = latest
		}
	}

	d, err := s.Address.Resolve(r.Context(), app.ID, app.Slug, base.Body.Routing, req.Request)
	if err != nil {
		Error(w, r, err)
		return
	}
	from := spec.Address(r.Host, app.Slug, base.Body.Routing)
	to := spec.Address(r.Host, app.Slug, d.Routing)
	if !d.Changed {
		JSON(w, http.StatusOK, map[string]any{"changed": false, "address": to})
		return
	}

	p := PrincipalFrom(r.Context())
	if d.Override {
		// R-163. Asked of the authorizer like any verb, which audits a
		// refusal (design 06 §6).
		if err := s.Authz.CheckControl(r.Context(), p, app.ID, authz.AppRoutingOverride); err != nil {
			Error(w, r, err)
			return
		}
	}

	if !req.Confirm {
		Error(w, r, errs.Newf(errs.ValidInvalid,
			"Changing this app's address moves it from %s to %s. Once the change is deployed the old address stops working, and links and bookmarks to it break.",
			describeAddress(from), describeAddress(to)).
			WithDetail("from", from).
			WithDetail("to", to).
			WithRemedy("Send confirm: true to go ahead."))
		return
	}

	next := *base.Body
	next.Routing = d.Routing
	if err := spec.Validate(&next); err != nil {
		Error(w, r, err)
		return
	}
	rev, err := s.Apps.CreateRevision(r.Context(), app.ID, &next, spec.OriginEdited, p.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        "routing.change",
		AppID:         app.ID,
		TargetKind:    "spec_revision",
		TargetID:      rev.ID,
		Detail: map[string]any{
			"from": from, "to": to, "revision": rev.Revision,
			"adapter_ref": d.Routing.AdapterRef, "mode": string(d.Routing.Mode),
			"override": d.Override,
		},
	})

	JSON(w, http.StatusCreated, map[string]any{
		"changed":  true,
		"address":  to,
		"revision": rev.Revision,
		"spec_id":  rev.ID,
	})
}

// handleGetRouting says where an app is reached, where it will be after the
// next deploy if that differs, and what it could be moved to.
//
// Readable with app.view, not install.view: the person changing an app's
// address is usually its owner, who cannot list the installation's adapters,
// and the choices have to come from somewhere they can read (R-261).
func (s *Server) handleGetRouting(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	body := map[string]any{"routing": app.Routing, "address": spec.Address(r.Host, app.Slug, app.Routing)}

	if app.PinnedSpecID != "" {
		if list, err := s.Apps.ListRevisions(r.Context(), app.ID); err == nil && len(list) > 0 && list[0].ID != app.PinnedSpecID {
			if latest, ok, err := s.Apps.RevisionByID(r.Context(), list[0].ID); err == nil && ok && latest.Body != nil &&
				latest.Body.Routing != app.Routing {
				// Saved and not yet deployed.
				body["next_routing"] = latest.Body.Routing
				body["next_address"] = spec.Address(r.Host, app.Slug, latest.Body.Routing)
			}
		}
	}

	var options []address.Option
	if s.Address != nil {
		options = s.Address.Options(r.Context())
	}
	// Each adapter by the name an administrator gave it, where there is one.
	names := map[string]string{}
	if s.Adapters != nil {
		if configured, err := s.Adapters.List(r.Context()); err == nil {
			for _, c := range configured {
				names[c.ID] = c.Name
			}
		}
	}
	type option struct {
		address.Option
		Name string `json:"name"`
	}
	out := make([]option, 0, len(options))
	for _, o := range options {
		name := names[o.AdapterRef]
		if name == "" {
			name = o.AdapterRef
		}
		out = append(out, option{Option: o, Name: name})
	}
	body["options"] = out
	JSON(w, http.StatusOK, body)
}

// describeAddress is an address as a sentence reads it.
func describeAddress(a string) string {
	if a == "" {
		return "no address"
	}
	return a
}
