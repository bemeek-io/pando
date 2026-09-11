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

	"github.com/bemeek-io/pando/internal/core/source"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
)

// SourcePolicy is the source allowlist check (R-092).
type SourcePolicy interface {
	AllowsSource(ctx context.Context, url string) error
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

	proposal, err := r.run(ctx, appID, app.Source)
	if err != nil {
		// The failure is recorded rather than only returned: detection runs in
		// the background after app creation, and a user who comes back to the
		// console later needs to find out what happened.
		e := errs.As(err)
		_ = r.Detections.Save(ctx, appID, state.DetectionFailed,
			map[string]any{"error": e}, "")
		return state.Detection{}, err
	}

	if err := r.Detections.Save(ctx, appID, proposal.Status, proposal, proposal.Commit); err != nil {
		return state.Detection{}, err
	}
	return r.Detections.Get(ctx, appID)
}

func (r *Runner) run(ctx context.Context, appID string, src spec.Source) (detect.Proposal, error) {
	checkout, err := source.Fetch(ctx, src)
	if err != nil {
		return detect.Proposal{}, err
	}
	defer checkout.Close()

	proposal, err := r.Job.Run(ctx, appID, src, checkout.View(src.Subdir))
	if err != nil {
		return detect.Proposal{}, err
	}

	// R-120: Ref is what the user asked for, Commit is what runs. Recorded on
	// the proposal so that accepting it pins a revision against a specific
	// commit rather than against a branch that has since moved.
	proposal.Commit = checkout.Commit
	proposal.DraftSpec.Source.Commit = checkout.Commit
	return proposal, nil
}
