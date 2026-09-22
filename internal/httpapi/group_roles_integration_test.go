//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR078_ANewTeamMemberGetsWhatTheTeamHas asserts roles held by a group: an
// app role shared with the group and an installation role given to it reach
// whoever is added, and leave with whoever is removed (R-079, live).
func TestR078_ANewTeamMemberGetsWhatTheTeamHas(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "team-wiki")

	var team struct {
		ID string `json:"id"`
	}
	created := i.do(admin, http.MethodPost, "/groups", map[string]any{"name": "Team"})
	require.Equal(t, http.StatusCreated, created.Code, created.String())
	created.JSON(t, &team)

	// The team edits its app, and reads the audit log.
	require.Equal(t, http.StatusCreated, i.do(admin, http.MethodPost, "/apps/"+appID+"/grants", map[string]any{
		"plane": "control", "principal_kind": "group", "principal_id": team.ID, "role_id": "role_operator",
	}).Code)
	auditor := i.customRole(admin, "auditor", "install", "install.audit.read")
	require.Equal(t, http.StatusOK,
		i.do(admin, http.MethodPut, "/groups/"+team.ID+"/role", map[string]string{"role_id": auditor}).Code)

	var groups struct {
		Groups []struct {
			ID            string `json:"id"`
			InstallRoleID string `json:"install_role_id"`
		} `json:"groups"`
	}
	i.do(admin, http.MethodGet, "/groups", nil).JSON(t, &groups)
	require.Equal(t, auditor, groups.Groups[0].InstallRoleID)

	var apps struct {
		Apps []struct {
			AppID   string `json:"app_id"`
			Control []struct {
				RoleID string `json:"role_id"`
			} `json:"control"`
		} `json:"apps"`
	}
	i.do(admin, http.MethodGet, "/groups/"+team.ID+"/apps", nil).JSON(t, &apps)
	require.Len(t, apps.Apps, 1)
	require.Equal(t, "role_operator", apps.Apps[0].Control[0].RoleID)

	// A new member: nothing, then everything the team has.
	newbie := i.user("newbie")
	newbieID := i.userID(newbie)
	require.Equal(t, http.StatusNotFound, i.do(newbie, http.MethodGet, "/apps/"+appID, nil).Code)
	require.Equal(t, http.StatusForbidden, i.do(newbie, http.MethodGet, "/audit", nil).Code)

	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodPut, "/groups/"+team.ID+"/members/"+newbieID, nil).Code)
	require.Equal(t, http.StatusOK,
		i.do(newbie, http.MethodPatch, "/apps/"+appID, map[string]string{"name": "Team wiki"}).Code, "operator through the team")
	require.Equal(t, http.StatusOK, i.do(newbie, http.MethodGet, "/audit", nil).Code, "the team's installation role")

	// Out of the team, out of both.
	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodDelete, "/groups/"+team.ID+"/members/"+newbieID, nil).Code)
	require.Equal(t, http.StatusNotFound, i.do(newbie, http.MethodGet, "/apps/"+appID, nil).Code)
	require.Equal(t, http.StatusForbidden, i.do(newbie, http.MethodGet, "/audit", nil).Code)

	// The role can be taken back.
	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodDelete, "/groups/"+team.ID+"/role", nil).Code)
}

// TestR088_TheLastAdministratorCannotBeTakenOutOfTheirGroup asserts the
// people-based lockout check on membership: a group holding the administrator
// role counts only while someone is in it.
func TestR088_TheLastAdministratorCannotBeTakenOutOfTheirGroup(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	ada := i.user("ada")
	adaID := i.userID(ada)

	var admins struct {
		ID string `json:"id"`
	}
	i.do(admin, http.MethodPost, "/groups", map[string]any{"name": "Admins"}).JSON(t, &admins)
	require.Equal(t, http.StatusOK,
		i.do(admin, http.MethodPut, "/groups/"+admins.ID+"/role", map[string]string{"role_id": "role_administrator"}).Code)
	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodPut, "/groups/"+admins.ID+"/members/"+adaID, nil).Code)

	// The first administrator steps down; Ada, through the group, is now the
	// only one.
	require.Equal(t, http.StatusNoContent, i.do(ada, http.MethodDelete, "/users/"+i.AdminID+"/role", nil).Code)

	refused := i.do(ada, http.MethodDelete, "/groups/"+admins.ID+"/members/"+adaID, nil)
	require.Equal(t, http.StatusBadRequest, refused.Code, refused.String())
	require.Contains(t, refused.String(), "nobody who can manage accounts")
	refused = i.do(ada, http.MethodPut, "/groups/"+admins.ID+"/members", map[string]any{"members": []string{}})
	require.Equal(t, http.StatusBadRequest, refused.Code, refused.String())
}

// Editing an account's username, name and email: an administrator may; the
// account holder may change their own name and email but not their username.
func TestAnAccountsProfileCanBeEdited(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	dana := i.user("dana")
	danaID := i.userID(dana)

	got := i.do(admin, http.MethodPatch, "/users/"+danaID, map[string]any{
		"username": "dana.k", "display_name": "Dana K", "email": "dana@example.com",
	})
	require.Equal(t, http.StatusNoContent, got.Code, got.String())
	var one struct {
		ExternalID  string `json:"external_id"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
	}
	i.do(admin, http.MethodGet, "/users/"+danaID, nil).JSON(t, &one)
	require.Equal(t, "dana.k", one.ExternalID)
	require.Equal(t, "Dana K", one.DisplayName)
	require.Equal(t, "dana@example.com", one.Email)

	// Signs in by the new name.
	_, signed := i.signIn("dana.k", "another-correct-horse-staple")
	require.Equal(t, http.StatusOK, signed.Code, signed.String())

	// Their own name, yes; their own username, no.
	require.Equal(t, http.StatusNoContent,
		i.do(dana, http.MethodPatch, "/users/"+danaID, map[string]any{"display_name": "Dana"}).Code)
	require.Equal(t, http.StatusForbidden,
		i.do(dana, http.MethodPatch, "/users/"+danaID, map[string]any{"username": "boss"}).Code)

	// A taken username, and nothing to change.
	taken := i.do(admin, http.MethodPatch, "/users/"+danaID, map[string]any{"username": "admin"})
	require.Equal(t, http.StatusBadRequest, taken.Code, taken.String())
	require.Contains(t, taken.String(), "already an account called")
	require.Equal(t, http.StatusBadRequest, i.do(admin, http.MethodPatch, "/users/"+danaID, map[string]any{}).Code)
}
