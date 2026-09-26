package detection

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/screening"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
)

type repo map[string]string

func (m repo) Open(name string) (io.ReadCloser, error) {
	content, ok := m[path.Clean(name)]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (m repo) Stat(name string) (api.FileInfo, error) {
	if content, ok := m[path.Clean(name)]; ok {
		return api.FileInfo{Name: name, Size: int64(len(content))}, nil
	}
	return api.FileInfo{}, io.EOF
}

func (m repo) Glob(string) ([]string, error) { return nil, nil }

var checkout = repo{"package.json": `{"scripts":{"start":"node server.js"}}`, "server.js": "listen(3000)"}

// fakeScreener answers however a test needs it to.
type fakeScreener struct {
	caps   api.AICapabilities
	result api.ScreenResult
	err    error
	called int
	fn     api.AIFunction
	got    api.ScreenRequest
}

func (f *fakeScreener) Capabilities(context.Context) (api.AICapabilities, error) {
	if f.caps.Functions == nil {
		f.caps.Functions = []api.AIFunction{api.AIFunctionRepairPlan, api.AIFunctionAnswerQuestions, api.AIFunctionRevisePlan}
	}
	return f.caps, nil
}

func (f *fakeScreener) RepairPlan(context.Context, api.ScreenRequest) (api.ScreenResult, error) {
	f.called++
	f.fn = api.AIFunctionRepairPlan
	return f.result, f.err
}

func (f *fakeScreener) AnswerQuestions(context.Context, api.ScreenRequest) (api.ScreenResult, error) {
	f.called++
	f.fn = api.AIFunctionAnswerQuestions
	return f.result, f.err
}

func (f *fakeScreener) RevisePlan(_ context.Context, req api.ScreenRequest) (api.ScreenResult, error) {
	f.called++
	f.fn = api.AIFunctionRevisePlan
	f.got = req
	return f.result, f.err
}

type recorder struct{ events []AuditEvent }

func (r *recorder) Write(_ context.Context, e AuditEvent) error {
	r.events = append(r.events, e)
	return nil
}

type deny struct{ reason string }

func (d deny) AllowsScreening(context.Context) string { return d.reason }

func proposal() detect.Proposal {
	draft := spec.AppSpec{
		SchemaVersion: spec.SchemaVersion,
		AppID:         "app_x",
		Build:         spec.Build{Strategy: spec.BuildBuildpack},
		Workloads: []spec.Workload{{
			Name: "web", Primary: true, Exposed: true,
			Ports: []spec.Port{{Number: 3000, Protocol: "http", Source: spec.PortObserved}},
		}},
	}
	return detect.Proposal{
		Status:    detect.StatusReady,
		Winner:    detect.Candidate{Strategy: spec.BuildBuildpack, Confidence: 0.7},
		DraftSpec: draft,
		Trial:     detect.TrialObservation{Ran: true, Started: true, ObservedPorts: []int{3000}},
	}
}

// failed is a proposal whose trial run bound its port and then crashed: the
// case an AI adapter is called to repair (R-336).
func failed() detect.Proposal {
	p := proposal()
	p.Trial.Crashed = true
	return p
}

// TestR336_NoAICallWhenDetectionSucceedsWithNoQuestions asserts R-336.
//
// The common path: a plan that worked and asked nothing is not sent anywhere.
// No call, no audit event (nothing left the host), and the proposal as the
// auction produced it.
func TestR336_NoAICallWhenDetectionSucceedsWithNoQuestions(t *testing.T) {
	p := proposal()
	before := p.DraftSpec
	screener, audit := &fakeScreener{}, &recorder{}

	outcome := (&Runner{Screener: screener, Auditor: audit}).screen(context.Background(), "app_x", &p, checkout)

	require.Zero(t, screener.called, "no AI call")
	require.False(t, outcome.Ran)
	require.Equal(t, screening.SkipNotNeeded, outcome.SkipCode)
	require.Empty(t, audit.events, "nothing was sent, so nothing is recorded as sent")
	require.Equal(t, before, p.DraftSpec)
}

// TestR336_AQuestionDeferredToTheTrialDoesNotCallTheAdapter asserts R-336: a
// deferred question is not asked of anyone, so it is not a reason to call.
func TestR336_AQuestionDeferredToTheTrialDoesNotCallTheAdapter(t *testing.T) {
	p := proposal()
	p.Questions = []detect.Question{{Key: detect.KeyPrimaryPort, Kind: api.QuestionPort, Deferred: true}}
	screener := &fakeScreener{}

	outcome := (&Runner{Screener: screener}).screen(context.Background(), "app_x", &p, checkout)

	require.Zero(t, screener.called)
	require.Equal(t, screening.SkipNotNeeded, outcome.SkipCode)
}

// TestR336_ACrashedTrialIsRepaired asserts R-336 and R-106: a plan that failed
// is handed to the repair function, and the original log stays on the
// proposal whatever the repair does (R-107).
func TestR336_ACrashedTrialIsRepaired(t *testing.T) {
	p := failed()
	p.TrialLog = "Error: connect ECONNREFUSED 127.0.0.1:5432"
	screener, audit := &fakeScreener{}, &recorder{}

	outcome := (&Runner{Screener: screener, Auditor: audit}).screen(context.Background(), "app_x", &p, checkout)

	require.Equal(t, 1, screener.called)
	require.Equal(t, api.AIFunctionRepairPlan, screener.fn)
	require.True(t, outcome.Ran)
	require.Equal(t, api.AIFunctionRepairPlan, outcome.Function)
	require.Contains(t, outcome.Why, "exited with an error")
	require.Equal(t, "Error: connect ECONNREFUSED 127.0.0.1:5432", p.TrialLog, "R-107: the log is still shown")
	require.Len(t, audit.events, 1)
	require.Equal(t, string(api.AIFunctionRepairPlan), audit.events[0].Detail["function"])
}

// TestR336_ARepositoryNoDetectorCouldReadIsRepaired asserts R-336.
func TestR336_ARepositoryNoDetectorCouldReadIsRepaired(t *testing.T) {
	p := proposal()
	p.Winner.Strategy = detect.StrategyUnknown
	screener := &fakeScreener{}

	outcome := (&Runner{Screener: screener}).screen(context.Background(), "app_x", &p, checkout)

	require.Equal(t, api.AIFunctionRepairPlan, screener.fn)
	require.Contains(t, outcome.Why, "None of Pando's detectors")
}

// TestR338_AnsweringQuestionsChangesNothingElse asserts R-338 and R-336: the
// call was made for the questions, so an answer lands and any other change the
// adapter proposed is refused with a reason rather than applied.
func TestR338_AnsweringQuestionsChangesNothingElse(t *testing.T) {
	p := proposal()
	p.DraftSpec.Workloads[0].Ports = nil
	p.Trial = detect.TrialObservation{Ran: true, Started: true}
	p.Questions = []detect.Question{{Key: detect.KeyPrimaryPort, Kind: api.QuestionPort, Prompt: "Which port?"}}
	screener := &fakeScreener{result: api.ScreenResult{Amendments: []api.Amendment{
		{Kind: api.AmendAnswerQuestion, Key: detect.KeyPrimaryPort, Value: "3000",
			Reason: "server.js calls listen(3000).", Evidence: []string{"server.js"}},
		{Kind: api.AmendSetCommand, Command: []string{"npm", "run", "serve"},
			Reason: "The start script expects production.", Evidence: []string{"package.json"}},
	}}}

	outcome := (&Runner{Screener: screener}).screen(context.Background(), "app_x", &p, checkout)

	require.Equal(t, api.AIFunctionAnswerQuestions, screener.fn)
	require.Equal(t, map[string]string{detect.KeyPrimaryPort: "3000"}, outcome.Answers)
	require.Len(t, outcome.Refused, 1)
	require.Equal(t, api.AmendSetCommand, outcome.Refused[0].Amendment.Kind)
	require.Contains(t, outcome.Refused[0].Reason, "only to answer")
	require.Empty(t, p.DraftSpec.Workloads[0].Command, "the plan is otherwise untouched")
	require.Empty(t, detect.Open(p.Questions, nil), "answered, by suggestion")
}

// TestR336_AValueNobodyHasIsFilledOnTheReadingTheAnswerAdopts asserts R-336's
// answer trigger for values: a required value the deploy waits on calls the
// adapter like a question does, and the value it fills lands on the reading
// its build-method answer adopts — the one accepting pins.
func TestR336_AValueNobodyHasIsFilledOnTheReadingTheAnswerAdopts(t *testing.T) {
	domain := "APP_DOMAIN"
	composeSpec := spec.AppSpec{
		Workloads: []spec.Workload{{Name: "app", Primary: true, Env: []spec.EnvEntry{{Key: domain, SlotRef: &domain}}}},
		Slots:     []spec.Slot{{Key: domain, Type: spec.SlotUnknown, Required: true}},
	}
	p := proposal()
	p.RunnersUp = []detect.Candidate{{Strategy: spec.BuildCompose, Spec: &composeSpec, Draft: detect.Draft{Workloads: composeSpec.Workloads}}}
	p.Questions = []detect.Question{{Key: detect.KeyBuildStrategy, Kind: api.QuestionChoice, Options: []string{"buildpack", "compose"}}}

	fn, why := needed(p)
	require.Equal(t, api.AIFunctionAnswerQuestions, fn)
	require.NotEmpty(t, why)

	noQuestions := p
	noQuestions.Questions = nil
	fn, why = needed(noQuestions)
	require.Equal(t, api.AIFunctionAnswerQuestions, fn, "an empty required value is enough on its own")
	require.Contains(t, why, "value")

	screener := &fakeScreener{result: api.ScreenResult{Amendments: []api.Amendment{
		{Kind: api.AmendAnswerQuestion, Key: detect.KeyBuildStrategy, Value: "compose",
			Reason: "The README deploys with compose.", Evidence: []string{"package.json"}},
		{Kind: api.AmendSetEnv, Key: domain, Value: "localhost",
			Reason: "server.js serves the app on localhost.", Evidence: []string{"server.js"}},
	}}}
	p.Winner.Draft = detect.Draft{Workloads: p.DraftSpec.Workloads}
	outcome := (&Runner{Screener: screener}).screen(context.Background(), "app_x", &p, checkout)
	require.Len(t, outcome.Applied, 2, outcome.Refused)

	filled, _ := p.RunnersUp[0].Spec.Slot(domain)
	require.Equal(t, &spec.Resolution{Mode: spec.ResolutionBound, Target: "localhost"}, filled.Resolution)
}

// TestR336_AFailedPlanThatAlsoAskedIsOneCall asserts R-336: a repair is handed
// the questions and may answer them, so there is never a second call.
func TestR336_AFailedPlanThatAlsoAskedIsOneCall(t *testing.T) {
	p := failed()
	p.Questions = []detect.Question{{Key: "start_command", Kind: api.QuestionText, Prompt: "Which command?"}}
	screener := &fakeScreener{}

	(&Runner{Screener: screener}).screen(context.Background(), "app_x", &p, checkout)

	require.Equal(t, 1, screener.called)
	require.Equal(t, api.AIFunctionRepairPlan, screener.fn)
}

// TestR335_NoAdapterConfiguredLeavesTheProposalAlone asserts R-335.
//
// An install with no AI adapter is not a degraded install: everything the
// auction produced is in the proposal either way (R-106).
func TestR335_NoAdapterConfiguredLeavesTheProposalAlone(t *testing.T) {
	p := proposal()
	before := p.DraftSpec

	outcome := (&Runner{}).screen(context.Background(), "app_x", &p, checkout)

	require.False(t, outcome.Ran)
	require.NotEmpty(t, outcome.Skipped)
	require.Equal(t, screening.SkipNotConfigured, outcome.SkipCode)
	require.Equal(t, before, p.DraftSpec)
}

// TestR335_AProviderThatFailsDoesNotFailTheDetection asserts R-335.
//
// A detection that failed because a provider was down is a detection that did
// not need to fail.
func TestR335_AProviderThatFailsDoesNotFailTheDetection(t *testing.T) {
	p := failed()
	before := p.DraftSpec
	audit := &recorder{}

	outcome := (&Runner{
		Screener: &fakeScreener{err: errors.New("502 bad gateway")},
		Auditor:  audit,
	}).screen(context.Background(), "app_x", &p, checkout)

	require.False(t, outcome.Ran)
	require.Contains(t, outcome.Skipped, "502 bad gateway")
	require.Equal(t, before, p.DraftSpec, "the deterministic proposal stands")
	require.Len(t, audit.events, 1, "the attempt is still recorded")
}

// TestR336_HostPolicyCanForbidScreeningInstallWide asserts R-336.
//
// And nothing is sent: the veto is checked before the adapter is reached, so a
// forbidden screening is one where no repository contents left the host.
func TestR336_HostPolicyCanForbidScreeningInstallWide(t *testing.T) {
	p := failed()
	screener := &fakeScreener{}

	outcome := (&Runner{
		Screener:     screener,
		ScreenPolicy: deny{"An administrator has turned off AI screening on this installation."},
	}).screen(context.Background(), "app_x", &p, checkout)

	require.False(t, outcome.Ran)
	require.Contains(t, outcome.Skipped, "administrator")
	require.Equal(t, screening.SkipPolicy, outcome.SkipCode)
	require.Zero(t, screener.called, "the adapter was never reached")
}

// TestR331_AnAmendmentLandsInTheDraftSpecAndIsAttributed asserts R-331 and
// R-334 together: the change is real, and the review can say who made it.
func TestR331_AnAmendmentLandsInTheDraftSpecAndIsAttributed(t *testing.T) {
	p := failed()
	audit := &recorder{}

	outcome := (&Runner{
		ScreenerRef: "ai_anthropic",
		Auditor:     audit,
		Screener: &fakeScreener{
			caps: api.AICapabilities{Model: "claude-opus-5"},
			result: api.ScreenResult{
				Model:     "claude-opus-5",
				FilesRead: []string{"package.json", "server.js"},
				Amendments: []api.Amendment{{
					Kind: api.AmendSetEnv, Key: "HOST", Value: "0.0.0.0",
					Reason:   "The app binds 127.0.0.1, which nothing outside the container can reach.",
					Evidence: []string{"server.js"},
				}},
			},
		},
	}).screen(context.Background(), "app_x", &p, checkout)

	require.True(t, outcome.Ran)
	require.Equal(t, "ai_anthropic", outcome.AdapterRef)
	require.Equal(t, "claude-opus-5", outcome.Model)
	require.Len(t, outcome.Applied, 1)
	require.Empty(t, outcome.Refused)

	require.Len(t, p.DraftSpec.Workloads[0].Env, 1)
	require.Equal(t, spec.EnvFromScreening, p.DraftSpec.Workloads[0].Env[0].Source)
	require.Equal(t, p.DraftSpec.Workloads, p.Winner.Draft.Workloads,
		"the winner's draft is what the review renders, and must not drift from the spec")

	// R-337: what left the host is recorded.
	require.Len(t, audit.events, 1)
	require.Equal(t, ActionScreen, audit.events[0].Action)
	require.Equal(t, []string{"package.json", "server.js"}, audit.events[0].Detail["files_read"])
}

// TestR333_AScreeningDoesNotOverruleTheTrialRun asserts R-333 through the
// whole path, not only through Apply.
func TestR333_AScreeningDoesNotOverruleTheTrialRun(t *testing.T) {
	p := failed()

	outcome := (&Runner{Screener: &fakeScreener{result: api.ScreenResult{
		Amendments: []api.Amendment{{
			Kind: api.AmendSetPort, Port: 8080,
			Reason:   "The framework usually serves on 8080.",
			Evidence: []string{"package.json"},
		}},
	}}}).screen(context.Background(), "app_x", &p, checkout)

	require.Empty(t, outcome.Applied)
	require.Len(t, outcome.Refused, 1)
	require.Equal(t, 3000, p.DraftSpec.Workloads[0].Ports[0].Number)
	require.Equal(t, spec.PortObserved, p.DraftSpec.Workloads[0].Ports[0].Source)
}

// TestR338_AnAnsweredQuestionStopsBeingAsked asserts R-338.
//
// R-103 is the metric this moves: a question answered from the repository is a
// question a person did not have to carry to an assistant and back (R-105).
func TestR338_AnAnsweredQuestionStopsBeingAsked(t *testing.T) {
	p := proposal()
	p.DraftSpec.Workloads[0].Ports = nil
	p.Trial = detect.TrialObservation{Ran: true, Started: true}
	p.Questions = []detect.Question{{
		Key: detect.KeyPrimaryPort, Kind: api.QuestionPort,
		Prompt: "Pando could not tell which port this app serves HTTP on.",
	}}
	p.Status = detect.StatusNeedsAnswers

	outcome := (&Runner{Screener: &fakeScreener{result: api.ScreenResult{
		Amendments: []api.Amendment{{
			Kind: api.AmendAnswerQuestion, Key: detect.KeyPrimaryPort, Value: "3000",
			Reason:   "server.js calls listen(3000).",
			Evidence: []string{"server.js"},
		}},
	}}}).screen(context.Background(), "app_x", &p, checkout)

	require.Equal(t, map[string]string{detect.KeyPrimaryPort: "3000"}, outcome.Answers)
	require.Len(t, outcome.Applied, 1, "the review lists it with the other changes")
	require.Equal(t, "server.js calls listen(3000).", outcome.Applied[0].Amendment.Reason)

	// The question stays, carrying the answer, so a person can see and change
	// it; nobody has to answer it.
	require.Len(t, p.Questions, 1)
	require.Equal(t, &detect.Suggestion{Value: "3000", Reason: "server.js calls listen(3000).",
		Evidence: []string{"server.js"}}, p.Questions[0].Suggested)
	require.Empty(t, detect.Open(p.Questions, nil), "nobody is asked this now")
	require.Equal(t, detect.StatusReady, detect.StatusFor(p.Winner, p.Questions))

	// Accepting with no answer of a person's applies the suggestion, recorded
	// as screened: a screener is not a person, and the review says which.
	accepted := p.WithAnswers(nil)
	require.Equal(t, 3000, accepted.Workloads[0].Ports[0].Number)
	require.Equal(t, spec.PortScreened, accepted.Workloads[0].Ports[0].Source)
}

// TestR338_APersonCanOverrideAnAIAnswer asserts R-338's review gate: an AI
// adapter's answer is a suggestion a person can replace, and theirs wins.
func TestR338_APersonCanOverrideAnAIAnswer(t *testing.T) {
	p := proposal()
	p.DraftSpec.Workloads[0].Ports = nil
	p.Questions = []detect.Question{{Key: detect.KeyPrimaryPort, Kind: api.QuestionPort,
		Suggested: &detect.Suggestion{Value: "3000"}}}

	overridden := p.WithAnswers(map[string]string{detect.KeyPrimaryPort: "8080"})
	require.Equal(t, 8080, overridden.Workloads[0].Ports[0].Number)
	require.Equal(t, spec.PortUser, overridden.Workloads[0].Ports[0].Source)
	require.Equal(t, map[string]string{detect.KeyPrimaryPort: "8080"},
		p.Answers(map[string]string{detect.KeyPrimaryPort: "8080"}))
}

// TestR099_ABlockedProposalIsNotScreened asserts R-099.
//
// There is nothing useful to amend in a compose file that cannot be imported,
// and the reason it cannot is the answer.
func TestR099_ABlockedProposalIsNotScreened(t *testing.T) {
	blocked := proposal()
	blocked.Status = detect.StatusBlocked
	blocked.Blocked = errs.As(errs.New(errs.PlanComposeConstructRejected,
		"This compose file sets privileged: true, which cannot run inside Pando's isolation boundary."))

	screener := &fakeScreener{}
	outcome := (&Runner{Screener: screener}).screen(context.Background(), "app_x", &blocked, checkout)

	require.False(t, outcome.Ran)
	require.Zero(t, screener.called)
}

// TestR330_ScreeningDoesNotReRankTheAuction asserts R-330.
//
// A screener is not a detector. Whatever it amends, the auction's reading of
// the repository — which detector won, with what confidence and evidence, and
// who came second — is exactly what it was. That is what keeps a proposal
// explainable (R-102): "a model ranked it highest" is not a reason.
func TestR330_ScreeningDoesNotReRankTheAuction(t *testing.T) {
	p := failed()
	p.Winner.Detector = "buildpack"
	p.Winner.Evidence = []string{"package.json declares a start script"}
	p.RunnersUp = []detect.Candidate{{Detector: "static", Strategy: spec.BuildStatic, Confidence: 0.4}}

	winner, runnersUp := p.Winner, p.RunnersUp

	outcome := (&Runner{Screener: &fakeScreener{result: api.ScreenResult{
		Amendments: []api.Amendment{{
			Kind: api.AmendSetEnv, Key: "NODE_ENV", Value: "production",
			Reason:   "The start script expects a production build.",
			Evidence: []string{"package.json"},
		}},
	}}}).screen(context.Background(), "app_x", &p, checkout)

	require.Len(t, outcome.Applied, 1, "the amendment landed")
	require.Equal(t, winner.Detector, p.Winner.Detector)
	require.Equal(t, winner.Strategy, p.Winner.Strategy)
	require.Equal(t, winner.Confidence, p.Winner.Confidence)
	require.Equal(t, winner.Evidence, p.Winner.Evidence)
	require.Equal(t, runnersUp, p.RunnersUp)
}

// TestR338_AScreenedAnswerThatCannotBecomeASpecIsRefused asserts R-338. The
// screener answered build_method in prose ("serve the repository root with
// php -S …") and it was applied to nothing (issue #55). An answer naming no
// reading a detector made is refused, with the reason a person would be given,
// and the question stays asked.
func TestR338_AScreenedAnswerThatCannotBecomeASpecIsRefused(t *testing.T) {
	p := proposal()
	p.Winner.Draft = detect.Draft{Workloads: p.DraftSpec.Workloads}
	p.Questions = []detect.Question{{
		Key: detect.KeyBuildMethod, Kind: api.QuestionChoice,
		Options: []string{string(spec.BuildBuildpack)},
		Prompt:  "Pando could not tell how this app is built.",
	}}
	p.Status = detect.StatusNeedsAnswers
	before := p.DraftSpec

	prose := "serve the repository root with php -S 0.0.0.0:8000"
	outcome := (&Runner{Screener: &fakeScreener{result: api.ScreenResult{
		Amendments: []api.Amendment{{
			Kind: api.AmendAnswerQuestion, Key: detect.KeyBuildMethod, Value: prose,
			Reason:   "The start script serves the repository root.",
			Evidence: []string{"server.js"},
		}},
	}}}).screen(context.Background(), "app_x", &p, checkout)

	require.True(t, outcome.Ran)
	require.Empty(t, outcome.Answers)
	require.Len(t, outcome.Refused, 1)
	require.Equal(t, detect.KeyBuildMethod, outcome.Refused[0].Amendment.Key)
	require.Equal(t, prose, outcome.Refused[0].Amendment.Value)
	require.Contains(t, outcome.Refused[0].Reason, "is not one of the ways Pando found to build this app")
	require.Len(t, p.Questions, 1, "the question is still asked")
	require.Equal(t, before, p.DraftSpec)
}
