//go:build integration

package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
)

// proposalWithRouting is a finished detection whose plan is routed as given.
func proposalWithRouting(appID string, routing spec.Routing) detect.Proposal {
	return detect.Proposal{
		Status: "ready",
		Winner: detect.Candidate{Detector: "dockerfile", Strategy: "dockerfile"},
		DraftSpec: spec.AppSpec{
			SchemaVersion: 1,
			AppID:         appID,
			Source:        spec.Source{Type: "git", URL: "https://github.com/acme/notes"},
			Build:         spec.Build{Strategy: "dockerfile", Dockerfile: "Dockerfile"},
			Routing:       routing,
			Workloads: []spec.Workload{{
				Name: "web", Primary: true, Exposed: true,
				Ports: []spec.Port{{Number: 8080, Protocol: "http"}},
			}},
		},
	}
}

// TestR336_AskingAIToChangeThePlanGoesThroughTheAPI asserts R-336's third
// trigger at the API (R-261): the message reaches core as written, the updated
// detection comes back with the plan's address, and a body that is not a
// message is refused before anything is asked.
func TestR336_AskingAIToChangeThePlanGoesThroughTheAPI(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")

	proposal := proposalWithRouting(appID, spec.Routing{Mode: spec.RoutingPort, Port: 9003})
	body, err := json.Marshal(proposal)
	require.NoError(t, err)
	detector := &fakeDetector{revised: state.Detection{AppID: appID, Status: "ready", Body: body}}
	i.Server.Detector = detector

	got := i.do(admin, http.MethodPost, "/apps/"+appID+"/detection/revise", map[string]any{"message": "It serves on 8080."})
	require.Equal(t, http.StatusOK, got.Code, got.String())
	require.Equal(t, "It serves on 8080.", detector.message)
	require.Contains(t, got.String(), `"address"`)

	bad := i.do(admin, http.MethodPost, "/apps/"+appID+"/detection/revise", "not an object")
	require.Equal(t, errs.ValidInvalid, errs.Code(bad.ErrorCode()), bad.String())

	i.Server.Detector = nil
	none := i.do(admin, http.MethodPost, "/apps/"+appID+"/detection/revise", map[string]any{"message": "x"})
	require.Equal(t, errs.AdapterUnavailable, errs.Code(none.ErrorCode()), none.String())
}

// TestR166_TheReviewShowsWhereTheAppWillBeAndAcceptSetsItsHostname asserts
// that GET /detection carries the address the plan's routing gives the app,
// and that accept applies a hostname the person chose in subdomain mode and
// refuses one where the address is a port Pando allocates.
func TestR166_TheReviewShowsWhereTheAppWillBeAndAcceptSetsItsHostname(t *testing.T) {
	ctx := context.Background()
	i := newInstall(t)
	admin := i.admin()

	// Port mode: the address is the host Pando is reached at, and that port.
	portApp := i.createApp(admin, "ported")
	require.NoError(t, state.NewDetections(i.db).Save(ctx, portApp, "ready",
		proposalWithRouting(portApp, spec.Routing{Mode: spec.RoutingPort, Port: 9003}), "abc123"))
	got := i.do(admin, http.MethodGet, "/apps/"+portApp+"/detection", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var body struct {
		Address string `json:"address"`
	}
	require.NoError(t, json.Unmarshal(got.Body, &body))
	require.Contains(t, body.Address, ":9003/")

	refused := i.do(admin, http.MethodPost, "/apps/"+portApp+"/detection/accept", map[string]any{"hostname": "notes.example.com"})
	require.Equal(t, errs.ValidInvalid, errs.Code(refused.ErrorCode()), refused.String())

	// Subdomain mode: the hostname is the person's to choose.
	hostApp := i.createApp(admin, "hosted")
	require.NoError(t, state.NewDetections(i.db).Save(ctx, hostApp, "ready",
		proposalWithRouting(hostApp, spec.Routing{AdapterRef: "rte_traefik", Mode: spec.RoutingSubdomain, Hostname: "hosted.apps.test"}), "abc123"))
	accepted := i.do(admin, http.MethodPost, "/apps/"+hostApp+"/detection/accept", map[string]any{"hostname": "Notes.Example.com"})
	require.Contains(t, []int{http.StatusOK, http.StatusCreated}, accepted.Code, accepted.String())

	var rev struct {
		Body spec.AppSpec `json:"body"`
	}
	require.NoError(t, json.Unmarshal(i.do(admin, http.MethodGet, "/apps/"+hostApp+"/specs/1", nil).Body, &rev))
	require.Equal(t, "notes.example.com", rev.Body.Routing.Hostname)
}
