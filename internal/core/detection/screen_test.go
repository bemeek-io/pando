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
}

func (f *fakeScreener) Capabilities(context.Context) (api.AICapabilities, error) {
	if f.caps.Functions == nil {
		f.caps.Functions = []api.AIFunction{api.AIFunctionScreenPlan}
	}
	return f.caps, nil
}

func (f *fakeScreener) ScreenPlan(context.Context, api.ScreenRequest) (api.ScreenResult, error) {
	f.called++
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
	p := proposal()
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
	p := proposal()
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
	p := proposal()
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
	p := proposal()

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
	require.Empty(t, p.Questions, "nobody is asked this now")
	require.Equal(t, 3000, p.DraftSpec.Workloads[0].Ports[0].Number)
	require.Equal(t, spec.PortScreened, p.DraftSpec.Workloads[0].Ports[0].Source,
		"a screener is not a person, and the review says which it was")
	require.Equal(t, detect.StatusReady, detect.StatusFor(p.Winner, p.Questions))
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
	p := proposal()
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
