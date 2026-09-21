// Package detection runs the detection job for an app and records the result.
//
// It is the seam between detection (which knows about repositories and
// detectors) and the rest of Pando (which knows about apps, policy and audit).
// Sequence A's numbered steps 6 through 12 live in internal/detect; the parts
// that need an app row, the source allowlist and a place to write the answer
// live here.
package detection

import (
	"context"
	"time"

	"github.com/bemeek-io/pando/internal/core/screening"
	"github.com/bemeek-io/pando/internal/core/source"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
)

// SourcePolicy is the source allowlist check (R-092), plus the isolation floors
// a detected spec inherits (R-024, R-114).
type SourcePolicy interface {
	AllowsSource(ctx context.Context, url string) error
	IsolationFloors(ctx context.Context) (build, runtime spec.IsolationClass, err error)
}

// Installation supplies the answers a repository cannot give about itself.
//
// A repository says it builds from a Dockerfile and listens on 3000. It cannot
// say which runtime this install uses or how apps here are addressed, and R-104
// is explicit that none of that is a question worth asking — it is
// configuration, and configuration gets a default.
type Installation interface {
	// Defaults returns the install's adapters, routing mode and limits. The
	// routing mode comes from the routing adapter's own declared default
	// (R-162): adding an app uses it without asking.
	Defaults(ctx context.Context) spec.Defaults
}

// Runner detects for an app and stores the proposal.
type Runner struct {
	Apps       *state.Apps
	Detections *state.Detections
	Job        *detect.Job

	// Policy is checked again here, not only at app creation. The allowlist can
	// change between the two, and a re-detection (R-022) of an app whose source
	// is no longer allowed must not clone it.
	Policy SourcePolicy

	// Install fills in everything the repository cannot answer. Without it a
	// detected spec describes the app and says nothing about where it runs,
	// which is a spec that cannot be planned — the gap that made detection
	// produce proposals nobody could deploy.
	Install Installation

	// Ports assigns a host port in port-mode routing. Only consulted when the
	// routing adapter's default mode is `port`, which the loopback adapter that
	// ships as the laptop default is.
	Ports PortAllocator

	// PortRange bounds that allocation.
	PortRangeStart int
	PortRangeEnd   int

	// Screener reviews the finished proposal against the repository (R-330).
	// Nil is the ordinary case: an install with no AI adapter configured is not
	// a degraded install, because everything the auction produced is there
	// either way (R-106, R-335).
	Screener     screening.Screener
	ScreenerRef  string
	ScreenPolicy ScreenPolicy

	// Auditor records that a repository's contents left the host (R-337).
	Auditor Auditor

	// Screening budget (R-339). Zero means the package default.
	ScreenMaxFiles int
	ScreenMaxBytes int64
	ScreenTimeout  time.Duration

	// Scanner scores the checkout while it is still on disk (R-312).
	//
	// Detection is the first and, for a while, the only moment Pando holds an
	// app's source: an app that has been added and not yet deployed has no
	// image to look at, and waiting for one means the first thing anybody sees
	// about a new app is "not scanned yet". A committed key or a vulnerable
	// lockfile is exactly what somebody wants to know *before* deciding to
	// deploy it.
	//
	// Optional, and never fatal. An installation with no scanner detects
	// exactly as before, and a scanner that fails does not fail a detection —
	// the proposal is the thing being produced here.
	Scanner SourceScanner
}

// SourceScanner scans a checkout. One method, so detection cannot reach into
// the rest of the security service.
type SourceScanner interface {
	ScanSource(ctx context.Context, appID, dir string)
}

// PortAllocator hands out host ports for port-mode routing.
type PortAllocator interface {
	Allocate(ctx context.Context, adapterRef, appID string, from, to int) (int, error)
}

// Detect runs detection for an app and records the result.
//
// The allowlist is checked before anything touches disk, and the assertion in
// Sequence A is exact: a blocked source produces zero disk writes, because
// git clone is never invoked. That is why this check is here and not inside
// source.Fetch — a check that runs after the call has already started is not
// the same promise.
func (r *Runner) Detect(ctx context.Context, appID string) (state.Detection, error) {
	app, found, err := r.Apps.ByID(ctx, appID)
	if err != nil {
		return state.Detection{}, err
	}
	if !found {
		return state.Detection{}, errs.New(errs.NotFound, "That app does not exist.")
	}

	if app.Source.Type == "" {
		return state.Detection{}, errs.New(errs.StateInvalid,
			"This app has no source to detect from.").
			WithRemedy("Create the app with a repository URL, or write a spec for it directly.")
	}

	if r.Policy != nil {
		if err := r.Policy.AllowsSource(ctx, app.Source.URL); err != nil {
			return state.Detection{}, err
		}
	}

	if err := r.Detections.Start(ctx, appID); err != nil {
		return state.Detection{}, err
	}

	proposal, err := r.run(ctx, appID, app.Slug, app.Source)
	if err != nil {
		// The failure is recorded rather than only returned: detection runs in
		// the background after app creation, and a user who comes back to the
		// console later needs to find out what happened.
		// errs.As is nil for an error carrying no envelope, and storing that
		// would leave the user looking at "error: null" where the reason should
		// be. Everything on this path should be enveloped; the fallback is for
		// the one that is not.
		recorded := any(map[string]any{"message": err.Error()})
		if e := errs.As(err); e != nil {
			recorded = e
		}
		_ = r.Detections.Save(ctx, appID, state.DetectionFailed,
			map[string]any{"error": recorded}, "")
		return state.Detection{}, err
	}

	if err := r.Detections.Save(ctx, appID, proposal.Status, proposal, proposal.Commit); err != nil {
		return state.Detection{}, err
	}
	return r.Detections.Get(ctx, appID)
}

func (r *Runner) run(ctx context.Context, appID, slug string, src spec.Source) (detect.Proposal, error) {
	checkout, err := source.Fetch(ctx, src)
	if err != nil {
		return detect.Proposal{}, err
	}
	defer checkout.Close()

	proposal, err := r.Job.Run(ctx, appID, src, checkout.View(src.Subdir))
	if err != nil {
		return detect.Proposal{}, err
	}

	// While the checkout exists, and after the proposal is in hand: a scan is
	// worth having and is not worth failing a detection for.
	if r.Scanner != nil {
		r.Scanner.ScanSource(ctx, appID, checkout.Dir)
	}

	// Fill in the install's own answers before the proposal is shown, not when
	// it is accepted. The review is where someone sees how their app will run,
	// and a draft that says nothing about routing or limits is not something
	// they can review — they would be approving blanks and finding out at
	// deploy time (R-102: the user sees the reasoning, not a verdict).
	r.applyDefaults(ctx, &proposal.DraftSpec, slug)

	// And to every other reading of the repository, because answering the
	// tie-break adopts one of them. A candidate completed only at accept time
	// is one whose review showed blanks.
	for i := range proposal.RunnersUp {
		if len(proposal.RunnersUp[i].Draft.Workloads) == 0 {
			continue
		}
		candidate := detect.Assemble(appID, src, proposal.RunnersUp[i].Draft)
		candidate.Source.Commit = checkout.Commit
		r.applyDefaults(ctx, &candidate, slug)
		proposal.RunnersUp[i].Spec = &candidate
	}

	// R-120: Ref is what the user asked for, Commit is what runs. Recorded on
	// the proposal so that accepting it pins a revision against a specific
	// commit rather than against a branch that has since moved.
	proposal.Commit = checkout.Commit
	proposal.DraftSpec.Source.Commit = checkout.Commit

	// Step 12a — screening (R-330, design 10 §4). After the defaults, because a
	// screener handed a spec with no routing mode and no limits is reviewing
	// blanks for the same reason a person would be. Before the proposal is
	// stored, because what is stored is what gets reviewed.
	//
	// The outcome is recorded whatever it is, including "nothing ran and here
	// is why". Screening never fails a detection (R-335), so there is nothing
	// to check here and that is the point.
	outcome := r.screen(ctx, appID, &proposal, checkout.View(src.Subdir))
	proposal.Screening = &outcome
	if outcome.Changed() {
		// An amendment can answer the last outstanding question, which turns
		// needs_answers into ready — the same recomputation the trial run gets.
		proposal.Status = detect.StatusFor(proposal.Winner, proposal.Questions)
	}
	return proposal, nil
}

// applyDefaults folds the install's configuration into a draft spec.
//
// Isolation floors come from host policy rather than from the defaults struct,
// because policy is a floor and not a preference (R-272): an install that
// requires VM-class isolation must have every new app inherit that, not a
// value someone configured elsewhere.
func (r *Runner) applyDefaults(ctx context.Context, s *spec.AppSpec, slug string) {
	if r.Install == nil {
		return
	}
	defaults := r.Install.Defaults(ctx)

	if r.Policy != nil {
		if build, runtime, err := r.Policy.IsolationFloors(ctx); err == nil {
			defaults.BuildIsolation = build
			defaults.RuntimeIsolation = runtime
		}
	}

	defaults.Apply(s, slug)

	// A port is the one routing field that cannot be derived from the app's
	// name: two apps called different things still collide if they are both
	// handed 9000. So it is allocated rather than defaulted, and only when the
	// mode that needs it is the one in effect.
	if s.Routing.Mode == spec.RoutingPort && s.Routing.Port == 0 && r.Ports != nil {
		port, err := r.Ports.Allocate(ctx, s.Routing.AdapterRef, s.AppID, r.PortRangeStart, r.PortRangeEnd)
		if err != nil {
			// Left at zero. Validation refuses the spec with a message about
			// the port, and the proposal still reaches the user carrying
			// everything else detection worked out — which is more use than
			// failing the whole run over one field.
			return
		}
		s.Routing.Port = port
	}
}
