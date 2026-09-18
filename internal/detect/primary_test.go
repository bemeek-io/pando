package detect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/detect"
)

// A draft in the shape accept produces one: the install's defaults have been
// applied, so what is left to check is the workloads.
func composeDraft() spec.AppSpec {
	return spec.AppSpec{
		SchemaVersion: spec.SchemaVersion,
		Source:        spec.Source{Type: spec.SourceGit, URL: "https://example.test/crewmate", Ref: "main"},
		Build:         spec.Build{Strategy: spec.BuildCompose, AdapterRef: "bld_buildkit"},
		Routing:       spec.Routing{AdapterRef: "rte_loopback", Mode: spec.RoutingPort, Port: 9000},
		Runtime:       spec.RuntimeRef{AdapterRef: "rt_docker", IsolationFloor: spec.IsolationContainer},
		Deploy:        spec.Deploy{Strategy: spec.DeployRecreate},
		Workloads: []spec.Workload{
			{
				Name:      "app",
				Build:     &spec.WorkloadBuild{Context: "."},
				Ports:     []spec.Port{{Number: 8080, Protocol: "http"}},
				DependsOn: []string{"db"},
			},
			{Name: "db", Image: "postgres:17"},
		},
	}
}

// TestR026_AcceptingWithoutAnsweringStillProducesAnAppThatCanDeploy asserts the
// end of a dead end.
//
// A compose file does not say which service a person opens, so detection asks —
// and that question rides on the compose candidate, which is not necessarily
// the one somebody adopts when they answer the tie-break. Accepting without it
// produced a spec with no primary workload at all: refused at deploy with "mark
// exactly one workload as primary", from a button, about a decision made on
// another screen, with nothing anywhere to mark it with.
func TestR026_AcceptingWithoutAnsweringStillProducesAnAppThatCanDeploy(t *testing.T) {
	proposal := detect.Proposal{DraftSpec: composeDraft()}

	out := proposal.WithAnswers(nil)
	require.NoError(t, spec.Validate(&out), "the accepted spec deploys")

	primary, ok := out.PrimaryWorkload()
	require.True(t, ok)
	require.Equal(t, "app", primary.Name,
		"the one serving HTTP that nothing depends on, which is the shape of a web service")
	require.True(t, primary.Exposed)

	// And it says so, rather than choosing quietly (R-102).
	var warned bool
	for _, w := range out.Warnings {
		if w.Code == spec.WarnPrimaryWorkloadAssumed {
			warned = true
			require.Contains(t, w.Message, "app")
		}
	}
	require.True(t, warned, "a choice Pando made for somebody is a choice it tells them about")
}

// An answer still wins, and leaves no warning behind: nothing was assumed.
func TestR026_AnAnsweredPrimaryBeatsTheElection(t *testing.T) {
	proposal := detect.Proposal{DraftSpec: composeDraft()}

	out := proposal.WithAnswers(map[string]string{detect.KeyPrimaryService: "db"})
	require.NoError(t, spec.Validate(&out))

	primary, ok := out.PrimaryWorkload()
	require.True(t, ok)
	require.Equal(t, "db", primary.Name)

	for _, w := range out.Warnings {
		require.NotEqual(t, spec.WarnPrimaryWorkloadAssumed, w.Code,
			"nothing was assumed, so nothing is warned about")
	}
}

// One workload is not a question at all.
func TestR026_ASingleWorkloadIsThePrimaryOne(t *testing.T) {
	draft := composeDraft()
	draft.Workloads = []spec.Workload{
		{Name: "web", Build: &spec.WorkloadBuild{Context: "."}, Ports: []spec.Port{{Number: 3000, Protocol: "http"}}},
	}

	out := detect.Proposal{DraftSpec: draft}.WithAnswers(nil)
	primary, ok := out.PrimaryWorkload()
	require.True(t, ok)
	require.Equal(t, "web", primary.Name)
}

// TestR131_AComposeDatabaseArrivesAlreadyFilled asserts that importing a compose
// file answers its own question.
//
// `services: db: image: postgres:16` says this app runs a PostgreSQL beside
// itself. Pando's sentence for that is "run one inside this app" (R-131), and
// leaving the slot empty instead turned an app that worked under
// `docker compose up` into one that was accepted and then refused at deploy —
// asking a question whose answer was in the file being imported.
func TestR131_AComposeDatabaseArrivesAlreadyFilled(t *testing.T) {
	const file = `
services:
  app:
    build: .
    ports: ["8080:8080"]
    depends_on: [db]
  db:
    image: postgres:16-alpine
`

	draft, err := detect.ImportCompose(memSource{"docker-compose.yml": file}, "docker-compose.yml")
	require.NoError(t, err)

	require.Len(t, draft.Slots, 1)
	slot := draft.Slots[0]
	require.Equal(t, "DB_URL", slot.Key)
	require.Equal(t, spec.SlotPostgres, slot.Type)
	require.True(t, slot.Required)

	require.NotNil(t, slot.Resolution, "the compose file already said what fills this")
	require.Equal(t, spec.ResolutionProvisioned, slot.Resolution.Mode)

	// And the evidence still says where it came from, because somebody
	// changing it is entitled to know what Pando read.
	require.NotEmpty(t, slot.Evidence)
	require.Contains(t, slot.Evidence[0], "db")
}
