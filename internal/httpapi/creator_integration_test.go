//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR081_ACreatorManagesTheAppsTheyMakeAndNothingElse asserts the built-in
// Creator role end to end: someone given it can make an app and manage it,
// cannot see or touch anyone else's, and reaches no installation setting.
func TestR081_ACreatorManagesTheAppsTheyMakeAndNothingElse(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	theirs := i.createApp(admin, "payroll")

	// An ordinary account, given the Creator role.
	maker := i.user("maker")
	var me struct {
		UserID string `json:"user_id"`
	}
	i.do(maker, http.MethodGet, "/me", nil).JSON(t, &me)

	// Before the role, making an app is refused.
	refused := i.do(maker, http.MethodPost, "/apps", map[string]any{
		"name": "notes", "source": map[string]string{"type": "git", "url": "https://github.com/acme/notes"},
	})
	require.Equal(t, http.StatusForbidden, refused.Code, refused.String())

	granted := i.do(admin, http.MethodPut, "/users/"+me.UserID+"/role", map[string]any{"role_id": "role_creator"})
	require.Contains(t, []int{http.StatusOK, http.StatusCreated, http.StatusNoContent}, granted.Code, granted.String())

	// Now they can, and they own it: rename it, share it, stop it.
	mine := i.createApp(maker, "notes")
	require.Equal(t, http.StatusOK,
		i.do(maker, http.MethodPatch, "/apps/"+mine, map[string]string{"name": "Team notes"}).Code)
	require.Equal(t, http.StatusOK, i.do(maker, http.MethodGet, "/apps/"+mine+"/grants", nil).Code)

	// Their list of apps to manage is theirs alone.
	var listed struct {
		Apps []struct {
			ID string `json:"id"`
		} `json:"apps"`
	}
	i.do(maker, http.MethodGet, "/apps", nil).JSON(t, &listed)
	require.Len(t, listed.Apps, 1)
	require.Equal(t, mine, listed.Apps[0].ID)

	// Someone else's app does not exist, as far as they can tell.
	require.Equal(t, http.StatusNotFound, i.do(maker, http.MethodGet, "/apps/"+theirs, nil).Code)
	require.Equal(t, http.StatusNotFound,
		i.do(maker, http.MethodPatch, "/apps/"+theirs, map[string]string{"name": "Mine now"}).Code)

	// And no installation setting is reachable.
	for _, path := range []string{"/users", "/policy", "/audit", "/adapters", "/backups"} {
		got := i.do(maker, http.MethodGet, path, nil)
		require.Equal(t, http.StatusForbidden, got.Code, "%s: %s", path, got.String())
	}

	// /me reports the one install verb, which is what shows them Admin — an
	// admin console with their apps in it and no settings.
	var verbs struct {
		Verbs []string `json:"verbs"`
	}
	i.do(maker, http.MethodGet, "/me", nil).JSON(t, &verbs)
	require.Equal(t, []string{"app.create"}, verbs.Verbs)
}
