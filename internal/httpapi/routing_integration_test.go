//go:build integration

package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/address"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
)

type routingStub struct {
	adapterapi.RoutingAdapter
	caps adapterapi.RoutingCapabilities
}

func (routingStub) Kind() string                                     { return "stub" }
func (routingStub) Category() adapterapi.Category                    { return adapterapi.CategoryRouting }
func (routingStub) Configure(context.Context, json.RawMessage) error { return nil }
func (routingStub) HealthCheck(context.Context) error                { return nil }
func (r routingStub) Capabilities(context.Context) (adapterapi.RoutingCapabilities, error) {
	return r.caps, nil
}

// withRouting gives an install a Cloudflare-like default adapter and a
// loopback one, and the address service, and returns an app the admin made
// with a user "ops" holding Operator on it.
func withRouting(t *testing.T) (*install, *session, *session, string) {
	t.Helper()
	i := newInstall(t)
	reg := i.Server.Registry
	require.NoError(t, reg.Register("rte_cf", routingStub{caps: adapterapi.RoutingCapabilities{
		Modes:       []spec.RoutingMode{spec.RoutingSubdomain, spec.RoutingPath},
		DefaultMode: spec.RoutingSubdomain, BaseDomain: "bemeek.io"}}))
	require.NoError(t, reg.Register("rte_loopback", routingStub{caps: adapterapi.RoutingCapabilities{
		Modes: []spec.RoutingMode{spec.RoutingPort, spec.RoutingPath}, DefaultMode: spec.RoutingPort}}))
	require.NoError(t, reg.SetDefault(adapterapi.CategoryRouting, "rte_cf"))
	i.Server.Address = &address.Service{Registry: reg, Ports: state.NewPorts(i.db),
		PortRangeStart: 9000, PortRangeEnd: 9019, BaseDomain: "apps.test"}

	admin := i.admin()
	appID := i.appWithSpec(admin, "notes")

	ops := i.user("ops")
	var users struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"external_id"`
		} `json:"users"`
	}
	i.do(admin, http.MethodGet, "/users", nil).JSON(t, &users)
	var opsID string
	for _, u := range users.Users {
		if u.Username == "ops" {
			opsID = u.ID
		}
	}
	require.NotEmpty(t, opsID)
	granted := i.do(admin, http.MethodPost, "/apps/"+appID+"/grants", map[string]any{
		"plane": "control", "principal_kind": "user", "principal_id": opsID, "role_id": "role_operator",
	})
	require.Equal(t, http.StatusCreated, granted.Code, granted.String())
	return i, admin, ops, appID
}

func (i *install) newestRouting(s *session, appID string) spec.Routing {
	i.t.Helper()
	var list struct {
		Revisions []struct {
			Revision int `json:"revision"`
		} `json:"revisions"`
	}
	i.do(s, http.MethodGet, "/apps/"+appID+"/specs", nil).JSON(i.t, &list)
	require.NotEmpty(i.t, list.Revisions)
	var rev struct {
		Body spec.AppSpec `json:"body"`
	}
	got := i.do(s, http.MethodGet, "/apps/"+appID+"/specs/"+strconv.Itoa(list.Revisions[0].Revision), nil)
	require.Equal(i.t, http.StatusOK, got.Code, got.String())
	got.JSON(i.t, &rev)
	return rev.Body.Routing
}

// TestR165_ChangingAnAddressNeedsConfirmation asserts R-165: a change is
// explained — from where, to where, and that bookmarks break — and nothing is
// written until it is confirmed.
func TestR165_ChangingAnAddressNeedsConfirmation(t *testing.T) {
	i, _, ops, appID := withRouting(t)
	// Named in the adapter's zone (R-162), from the app's slug.
	original := i.newestRouting(ops, appID).Hostname
	require.Regexp(t, `^notes.*\.bemeek\.io$`, original)

	unconfirmed := i.do(ops, http.MethodPut, "/apps/"+appID+"/routing", map[string]any{"hostname": "crew.bemeek.io"})
	require.Equal(t, http.StatusBadRequest, unconfirmed.Code, unconfirmed.String())
	require.Contains(t, unconfirmed.String(), original)
	require.Contains(t, unconfirmed.String(), "crew.bemeek.io")
	require.Contains(t, unconfirmed.String(), "bookmarks")
	require.Equal(t, original, i.newestRouting(ops, appID).Hostname, "nothing written")

	confirmed := i.do(ops, http.MethodPut, "/apps/"+appID+"/routing", map[string]any{"hostname": "crew.bemeek.io", "confirm": true})
	require.Equal(t, http.StatusCreated, confirmed.Code, confirmed.String())
	require.Equal(t, "crew.bemeek.io", i.newestRouting(ops, appID).Hostname)
}

// TestR261_WhoeverEditsAnAppCanSeeWhereItCanMove asserts R-261: the choices
// come from an endpoint the app's editor can read, without install.view, and
// a saved change shows as where the app will be after the next deploy.
func TestR261_WhoeverEditsAnAppCanSeeWhereItCanMove(t *testing.T) {
	i, _, ops, appID := withRouting(t)

	var got struct {
		Address     string `json:"address"`
		NextAddress string `json:"next_address"`
		Options     []struct {
			AdapterRef  string `json:"adapter_ref"`
			DefaultMode string `json:"default_mode"`
			BaseDomain  string `json:"base_domain"`
			IsDefault   bool   `json:"is_default"`
		} `json:"options"`
	}
	read := i.do(ops, http.MethodGet, "/apps/"+appID+"/routing", nil)
	require.Equal(t, http.StatusOK, read.Code, read.String())
	read.JSON(t, &got)
	require.Len(t, got.Options, 2)
	require.Empty(t, got.NextAddress, "nothing saved yet")

	changed := i.do(ops, http.MethodPut, "/apps/"+appID+"/routing", map[string]any{"hostname": "crew.bemeek.io", "confirm": true})
	require.Equal(t, http.StatusCreated, changed.Code, changed.String())
	i.do(ops, http.MethodGet, "/apps/"+appID+"/routing", nil).JSON(t, &got)
	require.Equal(t, "https://crew.bemeek.io/", got.NextAddress)
}

// TestR163_AnOperatorCannotMoveAnAppOffTheAdaptersDefaultMode asserts R-163
// on both ways of writing routing: PUT /routing and a whole spec.
func TestR163_AnOperatorCannotMoveAnAppOffTheAdaptersDefaultMode(t *testing.T) {
	i, admin, ops, appID := withRouting(t)

	refused := i.do(ops, http.MethodPut, "/apps/"+appID+"/routing", map[string]any{"mode": "path", "confirm": true})
	require.Equal(t, http.StatusForbidden, refused.Code, refused.String())
	require.Contains(t, refused.String(), "app.routing.override")

	whole := minimalSpec()
	whole["routing"] = map[string]any{"adapter_ref": "rte_cf", "mode": "path", "path_prefix": "/notes"}
	refused = i.do(ops, http.MethodPost, "/apps/"+appID+"/specs", whole)
	require.Equal(t, http.StatusForbidden, refused.Code, refused.String())

	// The owner's side of R-163: whoever holds the verb may.
	allowed := i.do(admin, http.MethodPut, "/apps/"+appID+"/routing", map[string]any{"mode": "path", "confirm": true})
	require.Equal(t, http.StatusCreated, allowed.Code, allowed.String())
	got := i.newestRouting(admin, appID)
	require.Equal(t, spec.RoutingPath, got.Mode)
	require.Equal(t, spec.ModeFromUserOverride, got.ModeSource)
}

// TestR162_MovingAnAppToTheDefaultAdapterNeedsNoOverride: the ordinary move
// an install makes when it switches routing adapters.
func TestR162_MovingAnAppToTheDefaultAdapterNeedsNoOverride(t *testing.T) {
	i, admin, ops, appID := withRouting(t)
	original := i.newestRouting(admin, appID).Hostname

	// Onto loopback first — loopback's default mode is its own, so this is
	// not an override either — and then back.
	moved := i.do(admin, http.MethodPut, "/apps/"+appID+"/routing", map[string]any{"adapter_ref": "rte_loopback", "confirm": true})
	require.Equal(t, http.StatusCreated, moved.Code, moved.String())
	got := i.newestRouting(admin, appID)
	require.Equal(t, spec.RoutingPort, got.Mode)
	require.Positive(t, got.Port, "a port is allocated, not left at zero")

	back := i.do(ops, http.MethodPut, "/apps/"+appID+"/routing", map[string]any{"adapter_ref": "rte_cf", "confirm": true})
	require.Equal(t, http.StatusCreated, back.Code, back.String())
	require.Equal(t, original, i.newestRouting(ops, appID).Hostname)
}
