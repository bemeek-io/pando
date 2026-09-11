package policy

import (
	"context"
	"net/url"
	"strings"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// Document is the host policy (design 02 §2.5).
//
// A single versioned document rather than scattered columns, so applying policy
// to a running install is one transaction and one audit event (R-274).
//
// Policy is a floor, not an override (R-272): it can only deny. Nothing here
// grants anything, which is why it is evaluated before grants rather than mixed
// with them.
type Document struct {
	// SourceAllowlist restricts where apps may be created from (R-092). Empty
	// means no restriction — Pando ships permissive defaults (R-270).
	SourceAllowlist []string `json:"source_allowlist,omitempty"`

	// DisabledVerbs are denied install-wide, to everyone, including an app's
	// owner. R-085's "host policy may disable exec install-wide" is this list
	// containing app.exec.
	DisabledVerbs []string `json:"disabled_verbs,omitempty"`

	// AllowAnonymousGrants controls whether an app may be shared with everyone
	// (R-076). Pointer so that "unset" is distinguishable from "explicitly
	// false", which matters when a policy document is partially written.
	AllowAnonymousGrants *bool `json:"allow_anonymous_grants,omitempty"`

	// MinBuildIsolation and MinRuntimeIsolation are floors (R-024, R-114).
	// Separate because how isolated a build must be is a different question
	// from how isolated the running app must be.
	MinBuildIsolation   spec.IsolationClass `json:"min_build_isolation,omitempty"`
	MinRuntimeIsolation spec.IsolationClass `json:"min_runtime_isolation,omitempty"`

	// EgressAllowlist is the install-wide default. An app-level list REPLACES
	// it rather than narrowing it (R-182, R-183), which is why defining one is
	// gated by app.egress.override (R-184) — this is a default, not a ceiling.
	EgressAllowlist []string `json:"egress_allowlist,omitempty"`

	// RequireBackupBeforeDestroy: an admin sets "never destroy without backup"
	// once, and app owners cannot override downward (R-284).
	RequireBackupBeforeDestroy bool `json:"require_backup_before_destroy,omitempty"`

	// MaxTokenLifetimeDays caps how long a token may live; 0 means no cap.
	// R-061 allows policy to forbid non-expiring tokens.
	MaxTokenLifetimeDays int `json:"max_token_lifetime_days,omitempty"`
}

// Default is the permissive starting posture (R-270).
func Default() Document {
	allowAnonymous := true
	return Document{
		AllowAnonymousGrants: &allowAnonymous,
		MinBuildIsolation:    spec.IsolationContainer,
		MinRuntimeIsolation:  spec.IsolationContainer,
	}
}

// Evaluator answers policy questions against the current document.
type Evaluator struct {
	load func(ctx context.Context) (Document, error)
}

// New builds an evaluator over a document source.
//
// The document is loaded per evaluation rather than cached, because R-274 says
// policy applies to a running install: a cached policy would keep denying, or
// keep allowing, for however long the cache lived.
func New(load func(ctx context.Context) (Document, error)) *Evaluator {
	return &Evaluator{load: load}
}

// Static builds an evaluator over a fixed document, for tests and for an
// install that has not written one yet.
func Static(d Document) *Evaluator {
	return &Evaluator{load: func(context.Context) (Document, error) { return d, nil }}
}

// Allows implements authz.Policy: it reports whether a verb is permitted at all.
//
// Evaluated before grants (design 06 §2, step 5). A policy that disables exec
// install-wide denies the owner too.
func (e *Evaluator) Allows(ctx context.Context, verb authz.Verb, _ string) error {
	doc, err := e.load(ctx)
	if err != nil {
		return err
	}

	for _, disabled := range doc.DisabledVerbs {
		if authz.Verb(disabled) != verb {
			continue
		}
		if verb == authz.AppExec {
			// R-085 has its own code because the console explains this one
			// specifically — exec being off install-wide is a deliberate
			// posture, not a permissions mistake to be escalated.
			return errs.New(errs.PolicyExecDisabled,
				"Running commands inside apps is turned off for this installation.").
				WithRemedy("An administrator can turn it back on in the installation's policy settings.")
		}
		return errs.Newf(errs.PolicyExecDisabled,
			"This action is turned off for this installation.").
			WithDetail("verb", string(verb)).
			WithRemedy("An administrator can change this in the installation's policy settings.")
	}
	return nil
}

// AllowsSource checks the source allowlist (R-092).
//
// Called before any clone, so a blocked source produces zero disk writes. The
// check being here rather than inside the clone path is the whole point: by the
// time you are cloning, you have already written to disk.
func (e *Evaluator) AllowsSource(ctx context.Context, rawURL string) error {
	doc, err := e.load(ctx)
	if err != nil {
		return err
	}
	if len(doc.SourceAllowlist) == 0 || rawURL == "" {
		return nil
	}

	host := hostOf(rawURL)
	for _, allowed := range doc.SourceAllowlist {
		if matchesHost(host, allowed) {
			return nil
		}
	}

	return errs.Newf(errs.PolicySourceNotAllowed,
		"Apps on this installation can only be created from approved sources, and %s is not one of them.", orUnknown(host)).
		WithDetail("source", rawURL).
		WithDetail("allowed", doc.SourceAllowlist).
		WithRemedy("Use a repository from an approved source, or ask an administrator to add this one.")
}

// AllowsAnonymousGrant checks R-076.
func (e *Evaluator) AllowsAnonymousGrant(ctx context.Context) error {
	doc, err := e.load(ctx)
	if err != nil {
		return err
	}
	if doc.AllowAnonymousGrants != nil && !*doc.AllowAnonymousGrants {
		return errs.New(errs.PolicyAnonymousGrantForbidden,
			"Apps on this installation cannot be shared with anyone on the internet.").
			WithRemedy("Share the app with specific people or groups instead, or ask an administrator whether this can be allowed.")
	}
	return nil
}

// IsolationFloors returns the build and runtime floors (R-024, R-114).
func (e *Evaluator) IsolationFloors(ctx context.Context) (build, runtime spec.IsolationClass, err error) {
	doc, err := e.load(ctx)
	if err != nil {
		return 0, 0, err
	}
	return doc.MinBuildIsolation, doc.MinRuntimeIsolation, nil
}

// Document returns the current policy.
func (e *Evaluator) Document(ctx context.Context) (Document, error) { return e.load(ctx) }

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.ToLower(u.Hostname())
	}
	// Also accept scp-style git remotes: git@github.com:acme/notes.git
	if _, rest, found := strings.Cut(raw, "@"); found {
		if host, _, ok := strings.Cut(rest, ":"); ok {
			return strings.ToLower(host)
		}
	}
	return ""
}

// matchesHost compares a host against an allowlist entry, which may be a bare
// host or a leading-dot suffix such as .corp.com.
func matchesHost(host, pattern string) bool {
	host = strings.ToLower(host)
	pattern = strings.ToLower(pattern)
	if host == "" {
		return false
	}
	if strings.HasPrefix(pattern, ".") {
		return strings.HasSuffix(host, pattern) || host == strings.TrimPrefix(pattern, ".")
	}
	return host == pattern
}

func orUnknown(host string) string {
	if host == "" {
		return "that address"
	}
	return host
}

var _ authz.Policy = (*Evaluator)(nil)
