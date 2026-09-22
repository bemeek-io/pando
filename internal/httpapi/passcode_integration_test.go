//go:build integration

package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/proxy"
)

// TestR075a_PublicWithAPasscode asserts the whole API side of it: sharing with
// everyone behind a passcode, the passcode page's two public calls, the unlock
// the proxy checks, and that changing the passcode or making the app private
// ends every unlock.
func TestR075a_PublicWithAPasscode(t *testing.T) {
	ctx := context.Background()
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")
	authzStore := state.NewAuthzStore(i.db)

	// Too short, and a passcode on anything but the grant to everyone.
	short := i.do(admin, http.MethodPost, "/apps/"+appID+"/grants",
		map[string]any{"plane": "data", "principal_kind": "anonymous", "passcode": "abc"})
	require.Equal(t, http.StatusBadRequest, short.Code, short.String())
	var grants struct {
		Grants []struct {
			PrincipalKind string `json:"principal_kind"`
		} `json:"grants"`
	}
	i.do(admin, http.MethodGet, "/apps/"+appID+"/grants", nil).JSON(t, &grants)
	for _, g := range grants.Grants {
		require.NotEqual(t, "anonymous", g.PrincipalKind, "a refused passcode leaves the app private")
	}

	created := i.do(admin, http.MethodPost, "/apps/"+appID+"/grants",
		map[string]any{"plane": "data", "principal_kind": "anonymous", "passcode": "open-sesame"})
	require.Equal(t, http.StatusCreated, created.Code, created.String())
	var grant struct {
		ID       string `json:"id"`
		Passcode bool   `json:"passcode"`
	}
	created.JSON(t, &grant)
	require.True(t, grant.Passcode)
	require.NotContains(t, created.String(), "open-sesame", "never returned")

	granted, passcode, err := authzStore.AnonymousAccess(ctx, appID)
	require.NoError(t, err)
	require.True(t, granted)
	require.True(t, passcode)

	// The passcode page, with no account.
	named := i.do(nil, http.MethodGet, "/apps/"+appID+"/passcode", nil)
	require.Equal(t, http.StatusOK, named.Code, named.String())
	require.Contains(t, named.String(), `"name":"notes"`)

	wrong := i.do(nil, http.MethodPost, "/apps/"+appID+"/passcode", map[string]string{"passcode": "guess"})
	require.NotEqual(t, http.StatusNoContent, wrong.Code)
	require.Contains(t, wrong.String(), "isn't right")

	token := unlock(t, i, appID, "open-sesame")
	ok, err := authzStore.PasscodeUnlocked(ctx, appID, token)
	require.NoError(t, err)
	require.True(t, ok, "the cookie's token is a live unlock")

	// A new passcode asks everyone again.
	require.Equal(t, http.StatusNoContent,
		i.do(admin, http.MethodPatch, "/apps/"+appID+"/grants/"+grant.ID, map[string]string{"passcode": "new-words"}).Code)
	ok, err = authzStore.PasscodeUnlocked(ctx, appID, token)
	require.NoError(t, err)
	require.False(t, ok)

	// And so does making it private.
	token = unlock(t, i, appID, "new-words")
	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodDelete, "/apps/"+appID+"/grants/"+grant.ID, nil).Code)
	ok, err = authzStore.PasscodeUnlocked(ctx, appID, token)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, http.StatusNotFound, i.do(nil, http.MethodGet, "/apps/"+appID+"/passcode", nil).Code,
		"not a passcode app any more, so nothing to name")
}

// TestR076_PolicyCanRequireAPasscodeOrForbidPublicSharing asserts the three
// public_sharing rules at the grants endpoint.
func TestR076_PolicyCanRequireAPasscodeOrForbidPublicSharing(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")
	setSharing := func(mode string) {
		t.Helper()
		var doc map[string]any
		i.do(admin, http.MethodGet, "/policy", nil).JSON(t, &doc)
		doc["public_sharing"] = mode
		got := i.do(admin, http.MethodPut, "/policy", doc)
		require.Equal(t, http.StatusOK, got.Code, got.String())
	}
	public := map[string]any{"plane": "data", "principal_kind": "anonymous"}
	withPasscode := map[string]any{"plane": "data", "principal_kind": "anonymous", "passcode": "open-sesame"}

	setSharing("passcode_only")
	var listed struct {
		PublicSharing string `json:"public_sharing"`
	}
	i.do(admin, http.MethodGet, "/apps/"+appID+"/grants", nil).JSON(t, &listed)
	require.Equal(t, "passcode_only", listed.PublicSharing)

	refused := i.do(admin, http.MethodPost, "/apps/"+appID+"/grants", public)
	require.Equal(t, http.StatusForbidden, refused.Code, refused.String())
	require.Contains(t, refused.String(), "only behind a passcode")
	created := i.do(admin, http.MethodPost, "/apps/"+appID+"/grants", withPasscode)
	require.Equal(t, http.StatusCreated, created.Code, created.String())
	var grant struct {
		ID string `json:"id"`
	}
	created.JSON(t, &grant)
	// Removing the passcode would make it plainly public.
	removed := i.do(admin, http.MethodPatch, "/apps/"+appID+"/grants/"+grant.ID, map[string]string{"passcode": ""})
	require.Equal(t, http.StatusForbidden, removed.Code, removed.String())
	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodDelete, "/apps/"+appID+"/grants/"+grant.ID, nil).Code)

	setSharing("none")
	require.Equal(t, http.StatusForbidden, i.do(admin, http.MethodPost, "/apps/"+appID+"/grants", withPasscode).Code)

	bad := i.do(admin, http.MethodPut, "/policy", map[string]any{"public_sharing": "sometimes"})
	require.Equal(t, http.StatusBadRequest, bad.Code, bad.String())
}

// The share picker finds people and groups by name, for whoever may share.
func TestSharingFindsPeopleAndGroups(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")
	i.user("dana")
	i.do(admin, http.MethodPost, "/groups", map[string]any{"name": "Design team"})

	var found struct {
		Users []struct {
			Username string `json:"username"`
		} `json:"users"`
		Groups []struct {
			Name string `json:"name"`
		} `json:"groups"`
	}
	i.do(admin, http.MethodGet, "/apps/"+appID+"/principals?q=dan", nil).JSON(t, &found)
	require.Len(t, found.Users, 1)
	require.Equal(t, "dana", found.Users[0].Username)
	i.do(admin, http.MethodGet, "/apps/"+appID+"/principals?q=design", nil).JSON(t, &found)
	require.Len(t, found.Groups, 1)

	// Someone who cannot share the app cannot search through it.
	require.Equal(t, http.StatusNotFound,
		i.do(i.user("stranger"), http.MethodGet, "/apps/"+appID+"/principals?q=a", nil).Code)
}

// unlock enters a passcode and returns the token the cookie carries.
func unlock(t *testing.T, i *install, appID, passcode string) string {
	t.Helper()
	got := i.do(nil, http.MethodPost, "/apps/"+appID+"/passcode", map[string]string{"passcode": passcode})
	require.Equal(t, http.StatusNoContent, got.Code, got.String())
	for _, c := range (&http.Response{Header: got.Hdr}).Cookies() {
		if c.Name == proxy.PasscodeCookiePrefix+appID {
			require.True(t, c.HttpOnly)
			return c.Value
		}
	}
	t.Fatal("no unlock cookie was set")
	return ""
}
