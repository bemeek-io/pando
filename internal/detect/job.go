package detect

import (
	"context"
	"fmt"
	"time"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/secret"
)

// Proposal is what a detection job produces (design 04 §2.2).
//
// A proposal, never a deployment (R-093). It is reviewed, and only then pinned
// as spec revision 1 — accepting is not deploying, which is the assertion
// Sequence A makes twice.
type Proposal struct {
	Status    string       `json:"status"`
	Winner    Candidate    `json:"winning_bid"`
	RunnersUp []Candidate  `json:"runners_up"`
	Questions []Question   `json:"questions"`
	DraftSpec spec.AppSpec `json:"draft_spec"`

	// Blocked is the reason, when the best reading of this repository is one
	// that cannot proceed — a compose file using a construct that cannot cross
	// the boundary (R-099).
	Blocked *errs.Error `json:"blocked,omitempty"`

	// TrialLog is the app's own output from the trial run. On a crash this is
	// the entire answer Pando has, and R-107 says showing it and stopping is
	// the correct outcome rather than a gap to close with inference.
	TrialLog string `json:"trial_log,omitempty"`

	// Commit is what was actually read. R-120: Ref is what the user asked for,
	// Commit is what runs.
	Commit string `json:"commit,omitempty"`
}

// RegistryProbe and PublishedImage live in the adapter package, because a probe
// is an adapter: it talks to ghcr.io and Docker Hub, and R-027 says nothing
// under internal/adapter may reach into core beyond internal/adapter/api.
type (
	RegistryProbe  = api.RegistryProbe
	PublishedImage = api.PublishedImage
)

// TrialRunner starts a draft in throwaway isolation (R-097).
type TrialRunner interface {
	Capabilities(ctx context.Context) (api.RuntimeCapabilities, error)
	Trial(ctx context.Context, req api.TrialRequest) (api.TrialResult, error)
}

// Builder produces an image from source, so the trial run has something to run.
type Builder interface {
	Build(ctx context.Context, req api.BuildRequest) (api.BuildResult, error)
}

// Job runs detection: Sequence A steps 6 through 12.
//
// Every collaborator is optional, and each one missing degrades rather than
// fails. That is R-106's shape applied to the whole job: with nothing
// configured, each step turns back into a question, not a dead end.
type Job struct {
	Auction  *Auction
	Registry RegistryProbe
	Runtime  TrialRunner
	Builder  Builder

	// TrialTimeout bounds the observation. Zero uses DefaultTrialTimeout.
	TrialTimeout time.Duration

	// NewTrialID names the throwaway bundle. Injectable so a test can assert
	// what was created and cleaned up.
	NewTrialID func() string
}

// DefaultTrialTimeout is how long an app gets to start and bind.
//
// Generous, because a first run pulls a base image and a JVM or a Rails app can
// take most of a minute to come up — and an app wrongly judged dead costs a
// question the whole trial run exists to avoid.
const DefaultTrialTimeout = 90 * time.Second

// Run detects how to build and run the source at src.
//
// Order matters and follows Sequence A. The registry check comes before the
// auction because a published image makes the auction moot; the trial run comes
// after, because there is nothing to run until a draft exists.
func (j *Job) Run(ctx context.Context, appID string, src spec.Source, view api.SourceView) (Proposal, error) {
	if j.Auction == nil {
		return Proposal{}, errs.New(errs.Internal, "Detection is not configured.")
	}

	// Step 7 — R-094 tier 1. An image the maintainer already publishes.
	if p, found := j.checkRegistry(ctx, appID, src); found {
		return p, nil
	}

	// Steps 8 and 9 — the auction.
	result, err := j.Auction.Run(ctx, view)
	if err != nil {
		return Proposal{}, err
	}

	proposal := Proposal{
		Status:    result.Status,
		Winner:    result.Winner,
		RunnersUp: result.RunnersUp,
		Questions: result.Questions,
	}
	if result.Blocked != nil {
		// Nothing further is useful. There is no point trial-running a compose
		// file that cannot be imported, and the reason is the answer.
		proposal.Blocked = errs.As(result.Blocked)
		proposal.DraftSpec = j.assemble(appID, src, result.Winner.Draft)
		//nolint:nilerr // The blocked reason is the proposal's content, not a
		// failure of the job. Returning it as an error would collapse "Pando
		// ran and found that this cannot be imported, here is which line and
		// why" into "detection failed", losing the only useful thing it knows.
		return proposal, nil
	}

	draft := result.Winner.Draft

	// Step 10 — the trial run.
	trial := j.trial(ctx, draft)
	draft, proposal.Questions = ApplyTrial(draft, proposal.Questions, trial)
	proposal.TrialLog = trial.Log

	// Step 11 — warnings. Path routing is read from the source rather than the
	// trial, so it is attached here regardless of whether a trial happened.
	draft.Warnings = append(draft.Warnings, PathRoutingWarning(view, draft)...)

	// Step 12 — the status, recomputed. The trial may have answered the only
	// outstanding question, which turns needs_answers into ready.
	proposal.Status = statusFor(result.Winner, proposal.Questions)
	proposal.Winner.Draft = draft
	proposal.DraftSpec = j.assemble(appID, src, draft)
	return proposal, nil
}

// statusFor recomputes detection status after the trial run.
func statusFor(winner Candidate, questions []Question) string {
	switch {
	case winner.Strategy == StrategyUnknown:
		return StatusNeedsAnswers
	case len(Asked(questions)) > 0:
		return StatusNeedsAnswers
	case winner.Confidence < uncertain:
		return StatusNeedsAnswers
	default:
		return StatusReady
	}
}

// checkRegistry is R-094 tier 1.
//
// A published image short-circuits everything below it, including the trial
// run: there is nothing to detect about how to build something that is already
// built, and spec.BuildPrebuilt is exactly that case.
func (j *Job) checkRegistry(ctx context.Context, appID string, src spec.Source) (Proposal, bool) {
	if j.Registry == nil {
		return Proposal{}, false
	}

	images, err := j.Registry.Published(ctx, src)
	if err != nil || len(images) == 0 {
		// A registry that is unreachable is not a detection failure. Everything
		// below tier 1 still works, and refusing to detect because a network
		// call failed would be the opposite of degrading gracefully.
		return Proposal{}, false
	}

	best := images[0]
	draft := Draft{
		Build: spec.Build{Strategy: spec.BuildPrebuilt},
		Workloads: []spec.Workload{{
			Name: "web", Image: best.Ref, Primary: true, Exposed: true,
		}},
	}

	candidate := Candidate{
		Detector:   "registry",
		Strategy:   spec.BuildPrebuilt,
		Confidence: 0.97,
		Evidence: []string{
			fmt.Sprintf("%s is already published at %s", best.Ref, best.Registry),
			"Pando will run this image rather than building the source. Nothing here checks that the " +
				"image was built from the commit you are deploying — only that it is published under " +
				"the same owner and name.",
		},
		Draft: draft,
	}

	// The port is still unknown, and still the trial run's to answer.
	candidate.Questions = []Question{{
		Key:      "primary_port",
		Kind:     api.QuestionPort,
		Deferred: true,
		Prompt: fmt.Sprintf(
			"Pando found a published image for this project (%s) and will run that rather than "+
				"building the source. It could not tell which port the image serves HTTP on. "+
				"Pando will start the image and watch it to work that out, but if that does not "+
				"succeed it needs to be told. Valid answer: a port number, such as 8080.", best.Ref),
		Why: "Pando needs to know where to send traffic once the app is running.",
	}}

	proposal := Proposal{Winner: candidate, Questions: candidate.Questions}

	trial := j.trial(ctx, draft)
	draft, proposal.Questions = ApplyTrial(draft, proposal.Questions, trial)
	proposal.TrialLog = trial.Log
	proposal.Status = statusFor(candidate, proposal.Questions)
	proposal.Winner.Draft = draft
	proposal.DraftSpec = j.assemble(appID, src, draft)
	return proposal, true
}

// trial runs the draft once in throwaway isolation, if that is possible.
//
// Everything here is a reason it might not be, and none of them is an error.
// A missing runtime, a runtime that cannot do it, an image that has not been
// built: each turns deferred questions back into real ones, which is worse for
// the user and not a failure of detection.
func (j *Job) trial(ctx context.Context, draft Draft) Trial {
	if j.Runtime == nil {
		return Trial{}
	}
	caps, err := j.Runtime.Capabilities(ctx)
	if err != nil || !caps.SupportsTrialRun {
		return Trial{}
	}

	primary, ok := primaryWorkload(draft)
	if !ok || primary.Image == "" {
		// Nothing to run. A source build would have to happen first, and
		// building before a proposal is reviewed is work nobody asked for.
		return Trial{}
	}

	timeout := j.TrialTimeout
	if timeout <= 0 {
		timeout = DefaultTrialTimeout
	}

	env := map[string]secret.Value{}
	for _, e := range primary.Env {
		if e.Value != nil {
			env[e.Key] = secret.New(*e.Value)
		}
	}

	result, err := j.Runtime.Trial(ctx, api.TrialRequest{
		TrialID:       j.trialID(),
		Image:         primary.Image,
		Command:       primary.Command,
		Entrypoint:    primary.Entrypoint,
		WorkingDir:    primary.WorkingDir,
		Env:           env,
		DeclaredPaths: declaredPaths(primary),
		Timeout:       timeout,
	})
	if err != nil {
		return Trial{}
	}
	return FromTrialResult(caps, result)
}

func (j *Job) trialID() string {
	if j.NewTrialID != nil {
		return j.NewTrialID()
	}
	return fmt.Sprintf("d%d", time.Now().UnixNano())
}

func primaryWorkload(draft Draft) (spec.Workload, bool) {
	for _, w := range draft.Workloads {
		if w.Primary {
			return w, true
		}
	}
	if len(draft.Workloads) == 1 {
		return draft.Workloads[0], true
	}
	return spec.Workload{}, false
}

// declaredPaths is where the draft says the app keeps data, so the trial run
// can tell a write it expected from one it did not (R-202).
func declaredPaths(w spec.Workload) []string {
	paths := make([]string, 0, len(w.Mounts))
	for _, m := range w.Mounts {
		paths = append(paths, m.Path)
	}
	return paths
}

// assemble turns a draft into a spec that can be shown, diffed and pinned.
//
// Origin is detected, and the revision stays 0: this is a proposal. It becomes
// revision 1 when someone accepts it (Sequence A step 14), and not before.
func (j *Job) assemble(appID string, src spec.Source, draft Draft) spec.AppSpec {
	return Assemble(appID, src, draft)
}

// Assemble turns a draft into a spec that can be shown, diffed and pinned.
//
// A plain function because answering the tie-break needs it too: adopting the
// other candidate's reading means assembling that candidate's draft, and doing
// it a second way is how the two would drift.
func Assemble(appID string, src spec.Source, draft Draft) spec.AppSpec {
	return spec.AppSpec{
		SchemaVersion: spec.SchemaVersion,
		AppID:         appID,
		Origin:        spec.OriginDetected,
		Source:        src,
		Build:         draft.Build,
		Workloads:     draft.Workloads,
		Volumes:       draft.Volumes,
		Slots:         draft.Slots,
		Health:        draft.Health,
		Warnings:      draft.Warnings,
	}
}
