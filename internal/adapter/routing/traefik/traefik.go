// Package traefik routes apps through a Traefik edge (R-160, R-164, R-165).
//
// Traefik sits in front of Pando and forwards to Pando's proxy. It never points
// at a workload, and that is the single most important thing about this file.
// R-023 makes Pando's proxy the only enforcement point for every request to
// every app; a Traefik router pointing straight at a container would be a path
// to an app that skips authorization entirely, and it would work, which is why
// the instinct to do it is dangerous. `RouteRequest.ProxyUpstream` is in the
// request precisely so an adapter is *told* where to point rather than
// discovering it.
//
// Configuration is written as Traefik's file provider — one YAML file per app
// in a watched directory. Chosen over the Docker label provider because labels
// live on the workload container, which would mean this adapter reaching into
// the runtime adapter's objects: two adapters owning one thing, and a route
// that disappears when a deploy recreates the container.
package traefik

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// Kind is the adapter's kind string.
const Kind = "traefik"

// Adapter writes Traefik file-provider configuration.
type Adapter struct {
	config Config
}

// Config is the adapter's configuration.
type Config struct {
	// Dir is the directory Traefik's file provider watches. Pando writes one
	// file per app here and Traefik picks them up; there is no API call and no
	// reload signal, which is why this adapter has no credentials.
	Dir string `json:"dir"`

	// EntryPoint is the Traefik entrypoint routers attach to.
	EntryPoint string `json:"entrypoint,omitempty"`

	// CertResolver is Traefik's ACME resolver name, if the edge has one
	// configured. Empty means Pando asks for no certificate and Traefik serves
	// whatever it already has — which is the honest behaviour when issuance is
	// the edge's business (O-5).
	CertResolver string `json:"cert_resolver,omitempty"`

	// BaseDomain is what a subdomain app's hostname is carved out of, when the
	// route request does not carry one.
	BaseDomain string `json:"base_domain,omitempty"`
}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Kind() string           { return Kind }
func (a *Adapter) Category() api.Category { return api.CategoryRouting }

func (a *Adapter) Configure(_ context.Context, raw json.RawMessage) error {
	cfg := Config{Dir: "/etc/traefik/dynamic", EntryPoint: "websecure"}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return errs.Wrap(errs.ValidInvalid, "The Traefik routing configuration could not be read.", err)
		}
	}
	if cfg.Dir == "" {
		return errs.New(errs.ValidInvalid, "Traefik routing needs a directory to write its configuration to.").
			WithRemedy("Set dir to the directory Traefik's file provider watches.")
	}
	a.config = cfg
	return nil
}

// HealthCheck verifies the configuration directory is writable.
//
// Not whether Traefik is running: Pando does not manage Traefik and cannot see
// it. What it can check is the one thing it is responsible for — that a route
// it writes will land somewhere Traefik reads. A directory that has become
// read-only produces routes that silently never appear, which is the failure
// this catches.
func (a *Adapter) HealthCheck(context.Context) error {
	if a.config.Dir == "" {
		return errs.New(errs.Internal, "Traefik routing is not configured.")
	}
	if err := os.MkdirAll(a.config.Dir, 0o755); err != nil {
		return errs.Wrap(errs.AdapterFailed,
			fmt.Sprintf("Pando cannot create Traefik's configuration directory at %s.", a.config.Dir), err)
	}

	probe := filepath.Join(a.config.Dir, ".pando-probe.yml")
	if err := os.WriteFile(probe, []byte("# pando write probe\n"), 0o644); err != nil {
		return errs.Wrap(errs.AdapterFailed,
			fmt.Sprintf("Pando cannot write to Traefik's configuration directory at %s.", a.config.Dir), err)
	}
	return os.Remove(probe)
}

// Capabilities: subdomain and path, with TLS when a resolver is configured.
//
// SupportsWildcardTLS follows CertResolver because a wildcard needs a DNS-01
// challenge, which needs provider credentials this adapter does not hold. Saying
// yes without them would produce a plan that succeeds and an app nobody can
// reach over HTTPS.
func (a *Adapter) Capabilities(context.Context) (api.RoutingCapabilities, error) {
	return api.RoutingCapabilities{
		Modes: []api.RoutingMode{spec.RoutingSubdomain, spec.RoutingPath},

		// Subdomain, because an install that has gone to the trouble of putting
		// Traefik in front of Pando has a hostname and wants apps on it. Path
		// mode remains available per app (R-162), and deviating from this
		// default is gated by app.routing.override.
		DefaultMode: spec.RoutingSubdomain,

		SupportsTLS:         a.config.CertResolver != "",
		SupportsWildcardTLS: false,

		// Traefik is an inbound edge: something has to reach it.
		RequiresPublicReachability: true,
	}, nil
}

// Ensure writes the router for an app.
//
// Idempotent by construction: the file is named for the app and rewritten
// whole, so applying the same route twice leaves the same file. The reconciler
// calls this on every pass and must not accumulate anything.
func (a *Adapter) Ensure(_ context.Context, r api.RouteRequest) (api.RouteHandle, error) {
	if r.ProxyUpstream == "" {
		// Refused rather than defaulted. A default here would be Pando's
		// address as this adapter guesses it, and a wrong guess produces a
		// route to nothing — or worse, to something else.
		return api.RouteHandle{}, errs.New(errs.AdapterFailed,
			"Pando did not say where to send this app's traffic.")
	}

	rule, err := a.rule(r)
	if err != nil {
		return api.RouteHandle{}, err
	}

	if err := os.MkdirAll(a.config.Dir, 0o755); err != nil {
		return api.RouteHandle{}, errs.Wrap(errs.AdapterFailed,
			"Pando could not write Traefik's configuration.", err)
	}

	body := a.render(r, rule)
	path := a.pathFor(r.AppID)

	// Written to a temporary file and renamed, because Traefik watches the
	// directory and will read a half-written file the moment it appears.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return api.RouteHandle{}, errs.Wrap(errs.AdapterFailed,
			"Pando could not write Traefik's configuration.", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return api.RouteHandle{}, errs.Wrap(errs.AdapterFailed,
			"Pando could not write Traefik's configuration.", err)
	}
	return api.RouteHandle{AppID: r.AppID, Handle: path}, nil
}

func (a *Adapter) Remove(_ context.Context, h api.RouteHandle) error {
	path := h.Handle
	if path == "" {
		path = a.pathFor(h.AppID)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errs.Wrap(errs.AdapterFailed, "Pando could not remove Traefik's configuration.", err)
	}
	return nil
}

// Observe reports whether the route file is there.
//
// It reports what exists and never remediates — the reconciler decides what to
// do about drift (design 05). An adapter that quietly rewrote a missing file
// here would make drift undetectable.
func (a *Adapter) Observe(_ context.Context, h api.RouteHandle) (api.RouteState, error) {
	path := h.Handle
	if path == "" {
		path = a.pathFor(h.AppID)
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return api.RouteState{Present: false}, nil
	}
	if err != nil {
		return api.RouteState{}, errs.Wrap(errs.AdapterFailed, "Pando could not read Traefik's configuration.", err)
	}
	return api.RouteState{Present: true, Address: addressIn(string(body))}, nil
}

// rule builds the Traefik matcher for a route.
func (a *Adapter) rule(r api.RouteRequest) (string, error) {
	switch r.Mode {
	case spec.RoutingSubdomain:
		host := r.Hostname
		if host == "" {
			return "", errs.New(errs.AdapterFailed, "This app has no hostname to route.")
		}
		return fmt.Sprintf("Host(`%s`)", host), nil

	case spec.RoutingPath:
		prefix := r.PathPrefix
		if prefix == "" {
			return "", errs.New(errs.AdapterFailed, "This app has no path to route.")
		}
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		if r.Hostname != "" {
			return fmt.Sprintf("Host(`%s`) && PathPrefix(`%s`)", r.Hostname, prefix), nil
		}
		return fmt.Sprintf("PathPrefix(`%s`)", prefix), nil

	default:
		// Port mode reaches Pando's proxy directly and needs no edge router.
		// Refused rather than ignored: silently writing nothing would leave an
		// app the planner believed was routed.
		return "", errs.Newf(errs.AdapterFailed,
			"Traefik routing does not handle %q addressing.", r.Mode)
	}
}

// render writes the dynamic configuration for one app.
//
// The service points at ProxyUpstream — Pando — and the path prefix is NOT
// stripped. Pando's proxy strips it and sets X-Forwarded-Prefix itself (R-167),
// because it is the thing that knows which app the prefix belonged to. Stripping
// here would hand Pando a path it cannot resolve.
func (a *Adapter) render(r api.RouteRequest, rule string) string {
	name := routerName(r.AppID)

	var b strings.Builder
	b.WriteString("# Written by Pando. Do not edit: this file is rewritten on every deploy.\n")
	b.WriteString("#\n")
	b.WriteString("# The service below points at Pando's proxy, never at the app's container.\n")
	b.WriteString("# Every request to every app goes through Pando so that authorization is\n")
	b.WriteString("# applied exactly once, in one place (R-023).\n")
	b.WriteString("http:\n")
	b.WriteString("  routers:\n")
	fmt.Fprintf(&b, "    %s:\n", name)
	fmt.Fprintf(&b, "      rule: %q\n", rule)
	fmt.Fprintf(&b, "      service: %s\n", name)
	if a.config.EntryPoint != "" {
		fmt.Fprintf(&b, "      entryPoints:\n        - %s\n", a.config.EntryPoint)
	}
	if r.TLS.Enabled && a.config.CertResolver != "" {
		b.WriteString("      tls:\n")
		fmt.Fprintf(&b, "        certResolver: %s\n", a.config.CertResolver)
	}
	b.WriteString("  services:\n")
	fmt.Fprintf(&b, "    %s:\n", name)
	b.WriteString("      loadBalancer:\n")
	b.WriteString("        servers:\n")
	fmt.Fprintf(&b, "          - url: %q\n", r.ProxyUpstream)
	// Pando's proxy needs the original Host to resolve a subdomain app, so the
	// header is passed through rather than rewritten to the upstream's.
	b.WriteString("        passHostHeader: true\n")
	return b.String()
}

func (a *Adapter) pathFor(appID string) string {
	return filepath.Join(a.config.Dir, "pando-"+sanitize(appID)+".yml")
}

func routerName(appID string) string { return "pando-" + sanitize(appID) }

// sanitize keeps a Traefik router name and a filename to safe characters.
//
// App IDs are Pando's own prefixed ULIDs, so this cannot bite today. It is here
// because "the caller only passes safe values" is how a path escapes a
// directory later, in a change that looks unrelated.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// addressIn pulls the upstream back out of a rendered file, for Observe.
func addressIn(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "- url: "); ok {
			return strings.Trim(after, `"`)
		}
	}
	return ""
}

var _ api.RoutingAdapter = (*Adapter)(nil)
