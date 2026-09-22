//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR081_AnAdministratorManagesAnAppSomebodyElseMade asserts
// install.apps.manage: the Administrator sees, and can do everything to, an
// app it holds no grant on — and a custom role holding install.apps.view sees
// it and can change nothing.
func TestR081_AnAdministratorManagesAnAppSomebodyElseMade(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	carol := i.user("carol")
	carolID := i.userID(carol)
	require.Equal(t, http.StatusOK,
		i.do(admin, http.MethodPut, "/users/"+carolID+"/role", map[string]string{"role_id": "role_creator"}).Code)
	appID := i.createApp(carol, "carols-notes")

	// Listed, and every app verb, without a grant on it.
	var list struct {
		Apps []struct {
			ID string `json:"id"`
		} `json:"apps"`
	}
	i.do(admin, http.MethodGet, "/apps", nil).JSON(t, &list)
	require.True(t, listed(list.Apps, appID), "an administrator lists every app")

	var one struct {
		Verbs []string `json:"verbs"`
	}
	i.do(admin, http.MethodGet, "/apps/"+appID, nil).JSON(t, &one)
	require.Contains(t, one.Verbs, "app.delete")
	require.Contains(t, one.Verbs, "app.grants.manage")
	require.Equal(t, http.StatusOK,
		i.do(admin, http.MethodPatch, "/apps/"+appID, map[string]string{"name": "Carol's notes"}).Code)

	// Carol's own role on it, and the administrator may change it in one step.
	type userApps struct {
		Apps []struct {
			AppID     string `json:"app_id"`
			Owner     bool   `json:"owner"`
			CanManage bool   `json:"can_manage"`
			Control   []struct {
				GrantID string `json:"grant_id"`
				RoleID  string `json:"role_id"`
				Via     string `json:"via"`
			} `json:"control"`
		} `json:"apps"`
	}
	var got userApps
	i.do(admin, http.MethodGet, "/users/"+carolID+"/apps", nil).JSON(t, &got)
	require.Len(t, got.Apps, 1)
	require.True(t, got.Apps[0].Owner)
	require.True(t, got.Apps[0].CanManage)
	require.Len(t, got.Apps[0].Control, 1)
	require.Equal(t, "role_owner", got.Apps[0].Control[0].RoleID)

	grant := got.Apps[0].Control[0].GrantID
	require.Equal(t, http.StatusNoContent,
		i.do(admin, http.MethodPatch, "/apps/"+appID+"/grants/"+grant, map[string]string{"role_id": "role_operator"}).Code)
	i.do(admin, http.MethodGet, "/users/"+carolID+"/apps", nil).JSON(t, &got)
	require.Equal(t, "role_operator", got.Apps[0].Control[0].RoleID)

	// An installation role is not an app role.
	bad := i.do(admin, http.MethodPatch, "/apps/"+appID+"/grants/"+grant, map[string]string{"role_id": "role_administrator"})
	require.Equal(t, http.StatusBadRequest, bad.Code, bad.String())

	// View-only: sees the app and Carol's role on it, and may change neither.
	viewerRole := i.customRole(admin, "app auditor", "install", "install.view", "install.apps.view")
	vic := i.user("vic")
	require.Equal(t, http.StatusOK,
		i.do(admin, http.MethodPut, "/users/"+i.userID(vic)+"/role", map[string]string{"role_id": viewerRole}).Code)

	i.do(vic, http.MethodGet, "/apps", nil).JSON(t, &list)
	require.True(t, listed(list.Apps, appID))
	i.do(vic, http.MethodGet, "/apps/"+appID, nil).JSON(t, &one)
	require.ElementsMatch(t, []string{"app.view", "app.logs.read"}, one.Verbs)
	require.Equal(t, http.StatusForbidden,
		i.do(vic, http.MethodPatch, "/apps/"+appID, map[string]string{"name": "Mine now"}).Code)

	i.do(vic, http.MethodGet, "/users/"+carolID+"/apps", nil).JSON(t, &got)
	require.Len(t, got.Apps, 1)
	require.False(t, got.Apps[0].CanManage)
	require.Equal(t, http.StatusForbidden,
		i.do(vic, http.MethodPatch, "/apps/"+appID+"/grants/"+grant, map[string]string{"role_id": "role_owner"}).Code)
}

// An account's app list shows only apps the viewer could see anyway: it is not
// a way to learn that an app exists.
func TestAccountAppsListOnlyWhatTheViewerCanSee(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	dana := i.user("dana")
	danaID := i.userID(dana)
	require.Equal(t, http.StatusOK,
		i.do(admin, http.MethodPut, "/users/"+danaID+"/role", map[string]string{"role_id": "role_creator"}).Code)
	i.createApp(dana, "private")

	// install.view alone reads accounts, but not their apps.
	peeker := i.customRole(admin, "directory", "install", "install.view")
	pat := i.user("pat")
	require.Equal(t, http.StatusOK,
		i.do(admin, http.MethodPut, "/users/"+i.userID(pat)+"/role", map[string]string{"role_id": peeker}).Code)

	got := i.do(pat, http.MethodGet, "/users/"+danaID+"/apps", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	require.JSONEq(t, `{"apps":[]}`, got.String())

	// Dana's own, with nothing administrative.
	got = i.do(dana, http.MethodGet, "/users/"+danaID+"/apps", nil)
	require.Contains(t, got.String(), `"can_manage":true`)
}

func listed(apps []struct {
	ID string `json:"id"`
}, id string) bool {
	for _, a := range apps {
		if a.ID == id {
			return true
		}
	}
	return false
}
