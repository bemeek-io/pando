package api

import (
	"context"
	"time"

	"github.com/bemeek-io/pando/internal/core/spec"
)

// The AI category (R-258), and the screening function (R-330 … R-339).
//
// Design 10 is the whole argument; two things from it are worth repeating where
// an implementer will see them.
//
// A screener is not a detector. It does not bid in the auction (R-093) and does
// not rank against one. It is handed the repository and the proposal the
// detectors already produced, and reports what that proposal got wrong or left
// out. The auction stays a pure function of the source.
//
// And a screener does not return a spec. It returns amendments drawn from the
// closed set below, which is what makes R-332 a mechanism rather than a rule:
// there is no amendment that says "lower the isolation floor", so there is no
// sentence in which a model can ask for one.

// AIFunction names something an AI adapter can do.
//
// Data, not a type assertion (R-254, R-259). An adapter that does not screen is
// skipped with a reason in the proposal, rather than by an assertion the caller
// cannot plan around.
type AIFunction string

const (
	// AIFunctionScreenPlan reviews a detection proposal against its source.
	AIFunctionScreenPlan AIFunction = "screen_plan"

	// The other two R-106 names. Declared so an adapter can advertise them and
	// so the reason this is a list is visible; neither is built.
	AIFunctionReadReadme  AIFunction = "read_readme"
	AIFunctionRepairBuild AIFunction = "repair_build"
)

// AICapabilities is what an AI adapter can do, as data.
type AICapabilities struct {
	Functions []AIFunction

	// Model is shown in the review. "Anthropic (claude-opus-5-5) read 7 files and
	// changed 3 things" is the honest account of what happened, and it is not
	// available if the model is a config value core never sees.
	Model string

	// MaxFiles and MaxBytes are what this adapter will read from a source. Core
	// lowers them to the install's own limits; it never raises them.
	MaxFiles int
	MaxBytes int64
}

// Does reports whether the adapter advertises a function.
func (c AICapabilities) Does(f AIFunction) bool {
	for _, have := range c.Functions {
		if have == f {
			return true
		}
	}
	return false
}

// AIAdapter is optional supporting functionality, never required (R-106).
//
// Every method may fail, and no failure of one is a failure of the thing that
// called it: R-335 says a screening that cannot run leaves the deterministic
// proposal exactly as it was.
type AIAdapter interface {
	Adapter
	Capabilities(ctx context.Context) (AICapabilities, error)

	// ScreenPlan reviews a proposal against its source and returns amendments.
	//
	// It must honor ctx cancellation and the budget in req. Returning an error
	// is legitimate and costs nothing but the screening.
	ScreenPlan(ctx context.Context, req ScreenRequest) (ScreenResult, error)
}

// ScreenRequest is a finished proposal, and the repository it came from.
type ScreenRequest struct {
	// Source is read-only, structurally (R-020). Handing it over grants reading
	// and nothing else — there is no write method to withhold.
	//
	// Not a digest core assembled: the value of screening is in the files
	// Pando's detectors do not read, and core cannot pre-select those without
	// already knowing what it is looking for, which is the problem.
	Source SourceView

	// Spec is the proposal, complete, with the install's defaults applied. A
	// screener that can see Routing.Mode can say something useful about an app
	// served under a path prefix; one handed a bare draft cannot.
	Spec spec.AppSpec

	// Evidence is why the winning detector bid what it did (R-102).
	Evidence []string

	// Questions are what detection could not work out. A screener may answer
	// one, and that is an amendment like any other (R-338).
	Questions []Question

	// Trial is what the trial run saw, including the app's own output.
	//
	// R-107's repository is not rescued by this — nothing in it says it needs
	// Postgres — but on an app that failed to start the log is the single most
	// informative thing Pando has, and withholding it from the one component
	// positioned to read it would be withholding it for no reason. What it may
	// produce is bounded by the amendment set, not by keeping it secret.
	Trial TrialSummary

	Budget ScreenBudget
}

// TrialSummary is the trial run's output (R-097), as a screener sees it.
type TrialSummary struct {
	Ran            bool
	Crashed        bool
	ObservedPorts  []int
	ObservedWrites []string
	Log            string
}

// ScreenBudget bounds a screening in files, bytes and wall clock (R-339).
type ScreenBudget struct {
	MaxFiles int
	MaxBytes int64
	Timeout  time.Duration
}

// ScreenResult is what a screening produced.
type ScreenResult struct {
	Amendments []Amendment

	// Notes are worth saying and change nothing. They are shown in the review
	// and never become warnings — a warning that is a remark teaches people to
	// ignore warnings.
	Notes []string

	// FilesRead is what left the host, recorded in the audit event (R-337).
	//
	// Not a security boundary: an adapter that lied about it has already read
	// the file. It is the operator's record of what was sent, which is the
	// thing somebody wants after the fact and cannot reconstruct.
	FilesRead []string

	Model string
}

// AmendmentKind is the closed set (R-332, design 10 §3).
//
// The obvious interface returns a whole spec and is wrong — not because a model
// might be careless, but because a returned spec can express every field there
// is, including the isolation floor host policy sets (R-272). That would leave
// a denylist in core as the only thing between a model's output and a lowered
// policy floor, and a denylist is a list somebody has to remember to extend
// when the spec grows a field.
//
// Nothing below says policy, isolation, adapter selection, routing, resources,
// egress, grants, or a secret's value.
type AmendmentKind string

const (
	// AmendSetCommand sets a workload's command. A wrong or missing start
	// command is the most common reason an app builds and does not come up.
	AmendSetCommand AmendmentKind = "set_command"

	// AmendSetEnv sets one literal environment entry. Never a secret: an
	// EnvEntry has exactly one of Value, SlotRef and SecretRef, and this writes
	// Value. HOST=0.0.0.0 on an app that binds loopback is the case this earns
	// its place on.
	AmendSetEnv AmendmentKind = "set_env"

	// AmendSetPort sets the port a workload serves HTTP on, and only where the
	// trial run observed nothing (R-333).
	AmendSetPort AmendmentKind = "set_port"

	// AmendSetHealth sets the health path and port.
	AmendSetHealth AmendmentKind = "set_health"

	// AmendAddSlot declares a dependency Pando's detectors missed. Finding a
	// DATABASE_URL in a file nothing reads is finding a declaration (R-021);
	// deciding an app needs Postgres because it imports an ORM is not, and this
	// cannot express that — a slot needs a key, and a key comes from the source.
	AmendAddSlot AmendmentKind = "add_slot"

	// AmendSetBuildContext, AmendSetDockerfile and AmendSetStaticDir are
	// R-106's "disambiguating monorepo entrypoints".
	AmendSetBuildContext AmendmentKind = "set_build_context"
	AmendSetDockerfile   AmendmentKind = "set_dockerfile"
	AmendSetStaticDir    AmendmentKind = "set_static_dir"

	// AmendAddVolume declares persistent storage at a path (R-201).
	AmendAddVolume AmendmentKind = "add_volume"

	// AmendAnswerQuestion answers one of detection's outstanding questions.
	// R-103 is the metric: a question answered from the repository is one a
	// person did not have to carry to an assistant and back.
	AmendAnswerQuestion AmendmentKind = "answer_question"

	// AmendAddWarning says something and changes nothing.
	AmendAddWarning AmendmentKind = "add_warning"
)

// Amendment is one proposed change to a draft spec.
//
// One struct with kind-dependent fields rather than a union, because this
// crosses a JSON boundary twice — into the provider's tool schema and out to
// the console — and a union does neither well.
type Amendment struct {
	Kind AmendmentKind `json:"kind"`

	// Workload names which workload, for the kinds that touch one. Empty means
	// the primary.
	Workload string `json:"workload,omitempty"`

	// Key is an environment key, a slot key, or a question key.
	Key string `json:"key,omitempty"`

	// Value is the new value, meaning what Kind says it means.
	Value string `json:"value,omitempty"`

	Command []string `json:"command,omitempty"`

	// Path is a path in the source, or a mount path for AmendAddVolume.
	Path string `json:"path,omitempty"`

	Port int `json:"port,omitempty"`

	SlotType spec.SlotType `json:"slot_type,omitempty"`

	// Required is honored only when the trial run crashed. O-4's [P] fallback,
	// unchanged: an unfilled required slot blocks a deploy (R-132), so a
	// screener marking slots required freely turns "might not start" into
	// "cannot be deployed" — a blocker where configuration would do (R-104).
	Required bool `json:"required,omitempty"`

	// Reason is held to R-105: self-contained, and readable by someone who
	// cannot see the repository.
	Reason string `json:"reason"`

	// Evidence names the files in the source this rests on, and is required
	// (R-334). The paths are checked to exist. A model that names a file that
	// is not there is refused with that as the reason — the cheapest available
	// check on whether it read the repository or recalled a framework.
	Evidence []string `json:"evidence"`
}
