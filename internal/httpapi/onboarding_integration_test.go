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
)

// TestR022_AcceptingSetsTheVariablesGivenWithIt asserts onboarding's accept:
// the values a person filled in go into the accepted spec in the same step,
// a plain one as a value and a secret as a reference to the secrets adapter,
// and a variable detection never found goes to the primary workload.
func TestR022_AcceptingSetsTheVariablesGivenWithIt(t *testing.T) {
	ctx := context.Background()
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")

	// A finished detection, as the runner stores one.
	proposal := detect.Proposal{
		Status: "ready",
		Winner: detect.Candidate{Detector: "dockerfile", Strategy: "dockerfile"},
		DraftSpec: spec.AppSpec{
			SchemaVersion: 1,
			AppID:         appID,
			Source:        spec.Source{Type: "git", URL: "https://github.com/acme/notes"},
			Build:         spec.Build{Strategy: "dockerfile", Dockerfile: "Dockerfile"},
			Workloads: []spec.Workload{{
				Name:    "web",
				Primary: true,
				Env:     []spec.EnvEntry{{Key: "API_URL"}, {Key: "API_KEY"}},
			}},
		},
	}
	require.NoError(t, state.NewDetections(i.db).Save(ctx, appID, "ready", proposal, "abc123"))

	accepted := i.do(admin, http.MethodPost, "/apps/"+appID+"/detection/accept", map[string]any{
		"values": []map[string]any{
			{"key": "API_URL", "value": "https://api.example.com"},
			{"key": "API_KEY", "value": "s3cret-value", "secret": true},
			{"key": "EXTRA", "value": "added-during-review"},
		},
	})
	require.Contains(t, []int{http.StatusOK, http.StatusCreated}, accepted.Code, accepted.String())
	require.NotContains(t, accepted.String(), "s3cret-value", "a secret never comes back")

	var specs struct {
		Revisions []struct {
			Revision int `json:"revision"`
		} `json:"revisions"`
	}
	i.do(admin, http.MethodGet, "/apps/"+appID+"/specs", nil).JSON(t, &specs)
	require.Len(t, specs.Revisions, 1, "one revision: accepting and setting values is one step")

	got := i.do(admin, http.MethodGet, "/apps/"+appID+"/specs/1", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	require.NotContains(t, got.String(), "s3cret-value", "the spec holds a reference, not the secret")
	var rev struct {
		Body spec.AppSpec `json:"body"`
	}
	require.NoError(t, json.Unmarshal(got.Body, &rev))
	env := map[string]spec.EnvEntry{}
	for _, e := range rev.Body.Workloads[0].Env {
		env[e.Key] = e
	}
	require.Equal(t, "https://api.example.com", *env["API_URL"].Value)
	require.Equal(t, "API_KEY", *env["API_KEY"].SecretRef)
	require.Equal(t, "added-during-review", *env["EXTRA"].Value)
	require.Equal(t, spec.EnvFromUser, env["API_URL"].Source)

	// And the secret is where the reference says.
	value, err := i.Secrets.Get(ctx, appID, "API_KEY")
	require.NoError(t, err)
	require.Equal(t, "s3cret-value", value.Reveal())
}

// TestR132_AValueSetDuringReviewFillsItsSlot asserts R-132 at accept: a
// variable filled from a slot — a key the compose importer found named with
// no value in .env.example — gets its value on the slot, so the slot is filled
// and the deploy is not refused for it. Replacing the variable's entry instead
// left the required slot unfilled and moved the refusal to deploy time.
func TestR132_AValueSetDuringReviewFillsItsSlot(t *testing.T) {
	ctx := context.Background()
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "crew")

	slot := "CREW_TOKEN_ENC_KEY"
	proposal := detect.Proposal{
		Status: "ready",
		Winner: detect.Candidate{Detector: "compose", Strategy: "compose"},
		DraftSpec: spec.AppSpec{
			SchemaVersion: 1,
			AppID:         appID,
			Source:        spec.Source{Type: "git", URL: "https://github.com/acme/crew"},
			Build:         spec.Build{Strategy: "dockerfile", Dockerfile: "Dockerfile"},
			Workloads: []spec.Workload{{
				Name:    "app",
				Primary: true,
				Env:     []spec.EnvEntry{{Key: slot, SlotRef: &slot}},
			}},
			Slots: []spec.Slot{{Key: slot, Type: spec.SlotUnknown, Required: true}},
		},
	}
	require.NoError(t, state.NewDetections(i.db).Save(ctx, appID, "ready", proposal, "abc123"))

	accepted := i.do(admin, http.MethodPost, "/apps/"+appID+"/detection/accept", map[string]any{
		"values": []map[string]any{{"key": slot, "value": "enc-key-value", "workload": "app"}},
	})
	require.Contains(t, []int{http.StatusOK, http.StatusCreated}, accepted.Code, accepted.String())

	got := i.do(admin, http.MethodGet, "/apps/"+appID+"/specs/1", nil)
	require.NotContains(t, got.String(), "enc-key-value", "the spec holds a reference, not the value")
	var rev struct {
		Body spec.AppSpec `json:"body"`
	}
	require.NoError(t, json.Unmarshal(got.Body, &rev))
	filled, ok := rev.Body.Slot(slot)
	require.True(t, ok)
	require.NotNil(t, filled.Resolution, "the slot is filled")
	require.Equal(t, spec.ResolutionLiteral, filled.Resolution.Mode)
	require.Equal(t, spec.SlotSecretKey(slot), filled.Resolution.SecretRef)
	require.Equal(t, &slot, rev.Body.Workloads[0].Env[0].SlotRef, "the variable still reads from its slot")

	value, err := i.Secrets.Get(ctx, appID, spec.SlotSecretKey(slot))
	require.NoError(t, err)
	require.Equal(t, "enc-key-value", value.Reveal())
}
