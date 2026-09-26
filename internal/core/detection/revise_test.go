package detection

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/clock"
	"github.com/bemeek-io/pando/internal/core/screening"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
)

var when = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

// TestR336_APersonsRequestIsCheckedAndApplied asserts R-336's third trigger:
// a person reviewing the plan says what is wrong, the adapter is asked with
// their words and the conversation so far, and what it proposes lands in the
// plan with the exchange recorded.
func TestR336_APersonsRequestIsCheckedAndApplied(t *testing.T) {
	p := failed()
	p.Screening = &screening.Outcome{Ran: true, FilesRead: []string{"package.json"}}
	p.Conversation = []detect.Turn{
		{From: detect.TurnPerson, Text: "earlier"},
		{From: detect.TurnAI, Text: "reply", FilesRead: []string{"server.js", "package.json"}},
	}
	screener, audit := &fakeScreener{result: api.ScreenResult{
		Reply: "server.js binds 127.0.0.1, so I set HOST to 0.0.0.0.",
		Amendments: []api.Amendment{{
			Kind: api.AmendSetEnv, Key: "HOST", Value: "0.0.0.0",
			Reason: "server.js binds 127.0.0.1.", Evidence: []string{"server.js"},
		}},
	}}, &recorder{}
	r := &Runner{Screener: screener, Auditor: audit, Clock: clock.NewFake(when)}

	err := r.revise(context.Background(), "app_x", &p, nil, checkout, "It's unreachable once deployed.")
	require.NoError(t, err)

	require.Equal(t, api.AIFunctionRevisePlan, screener.fn)
	require.Equal(t, "It's unreachable once deployed.", screener.got.Instruction)
	require.Len(t, screener.got.Conversation, 2, "the adapter hears what was said before")
	require.Equal(t, []string{"package.json", "server.js"}, screener.got.Known,
		"and starts from what it already read, once each, rather than reading it all again")

	require.Len(t, p.DraftSpec.Workloads[0].Env, 1)
	require.Equal(t, "HOST", p.DraftSpec.Workloads[0].Env[0].Key)

	require.Len(t, p.Conversation, 4)
	person, ai := p.Conversation[2], p.Conversation[3]
	require.Equal(t, detect.Turn{From: detect.TurnPerson, Text: "It's unreachable once deployed.", At: when}, person)
	require.Equal(t, detect.TurnAI, ai.From)
	require.Contains(t, ai.Text, "HOST")
	require.Len(t, ai.Changes, 1)

	// R-337: what left the host is recorded, as its own action.
	require.Len(t, audit.events, 1)
	require.Equal(t, ActionRevise, audit.events[0].Action)
}

// TestR336_ARevisionChangesTheReadingAcceptingWouldPin asserts R-336: when the
// build-method answer adopts a runner-up, the change lands on that reading's
// spec — the one accept pins — not on the winner's draft nobody chose.
func TestR336_ARevisionChangesTheReadingAcceptingWouldPin(t *testing.T) {
	p := proposal()
	composeSpec := spec.AppSpec{Workloads: []spec.Workload{{Name: "app", Primary: true}}}
	p.RunnersUp = []detect.Candidate{{
		Strategy: spec.BuildCompose, Spec: &composeSpec,
		Draft: detect.Draft{Workloads: composeSpec.Workloads},
	}}
	p.Questions = []detect.Question{{Key: detect.KeyBuildStrategy, Kind: api.QuestionChoice,
		Options: []string{"buildpack", "compose"}, Suggested: &detect.Suggestion{Value: "compose"}}}
	r := &Runner{Screener: &fakeScreener{result: api.ScreenResult{Amendments: []api.Amendment{{
		Kind: api.AmendSetEnv, Key: "NODE_ENV", Value: "production",
		Reason: "The start script expects production.", Evidence: []string{"package.json"},
	}}}}}

	require.NoError(t, r.revise(context.Background(), "app_x", &p, nil, checkout, "Run it in production mode."))

	require.Len(t, p.RunnersUp[0].Spec.Workloads[0].Env, 1, "the compose reading changed")
	require.Empty(t, p.DraftSpec.Workloads[0].Env, "the winner's draft did not")
}

// TestR332_ARevisionIsHeldToTheClosedSet asserts R-332 – R-334 for a person's
// request: asking is not a way around the rules. A change the repository does
// not support is refused and recorded on the turn, not applied.
func TestR332_ARevisionIsHeldToTheClosedSet(t *testing.T) {
	p := proposal()
	before := p.DraftSpec
	r := &Runner{Screener: &fakeScreener{result: api.ScreenResult{
		Reply: "Set the port.",
		Amendments: []api.Amendment{{
			Kind: api.AmendSetPort, Port: 8080,
			Reason: "The person said so.", Evidence: []string{"package.json"},
		}},
	}}}

	require.NoError(t, r.revise(context.Background(), "app_x", &p, nil, checkout, "It serves on 8080."))

	require.Equal(t, before, p.DraftSpec, "an observed port outranks a request (R-333)")
	ai := p.Conversation[len(p.Conversation)-1]
	require.Empty(t, ai.Changes)
	require.Len(t, ai.Refused, 1)
}

// TestR335_ARevisionThatCannotRunChangesNothing asserts R-335 for a revision:
// a provider that fails leaves the plan and the conversation as they were, and
// says so.
func TestR335_ARevisionThatCannotRunChangesNothing(t *testing.T) {
	p := proposal()
	before := p
	r := &Runner{Screener: &fakeScreener{err: errors.New("502 bad gateway")}}

	err := r.revise(context.Background(), "app_x", &p, nil, checkout, "Add Redis.")
	require.Error(t, err)
	require.Equal(t, errs.AdapterFailed, errs.As(err).Code)
	require.Contains(t, errs.As(err).Message, "502")
	require.Equal(t, before, p)
}

// TestR336_ARevisionNeedsSomethingToSayAndSomebodyToAsk asserts the requests
// Revise refuses before reaching storage or the network.
func TestR336_ARevisionNeedsSomethingToSayAndSomebodyToAsk(t *testing.T) {
	_, err := (&Runner{Screener: &fakeScreener{}}).Revise(context.Background(), "app_x", "   ")
	require.Equal(t, errs.ValidInvalid, errs.As(err).Code)

	_, err = (&Runner{}).Revise(context.Background(), "app_x", "Add Redis.")
	require.Equal(t, errs.AdapterUnavailable, errs.As(err).Code)

	_, err = (&Runner{Screener: &fakeScreener{}, ScreenPolicy: deny{"AI is off here."}}).
		Revise(context.Background(), "app_x", "Add Redis.")
	require.Equal(t, errs.PermDenied, errs.As(err).Code)
}
