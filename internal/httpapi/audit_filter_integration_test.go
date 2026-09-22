//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The audit log answers "who did what to which, and when" — the filters
// combine, so "this person's changes to this app, last hour" is one query.
func TestTheAuditLogFiltersByTargetAndTime(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	notes := i.createApp(admin, "notes")
	i.createApp(admin, "wiki")
	require.Equal(t, http.StatusOK,
		i.do(admin, http.MethodPatch, "/apps/"+notes, map[string]string{"name": "Team notes"}).Code)

	type page struct {
		Events []struct {
			Action   string `json:"action"`
			TargetID string `json:"target_id"`
		} `json:"events"`
	}
	get := func(query string) page {
		t.Helper()
		got := i.do(admin, http.MethodGet, "/audit"+query, nil)
		require.Equal(t, http.StatusOK, got.Code, "%s: %s", query, got.String())
		var p page
		got.JSON(t, &p)
		return p
	}

	onNotes := get("?target_kind=app&target_id=" + notes)
	require.NotEmpty(t, onNotes.Events)
	for _, e := range onNotes.Events {
		require.Equal(t, notes, e.TargetID)
	}

	// Actor and target and action together.
	renamed := get("?principal_id=" + i.AdminID + "&target_id=" + notes + "&action=app.update")
	require.Len(t, renamed.Events, 1)

	// A window that holds it, and windows that do not.
	hourAgo := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	inAnHour := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	require.NotEmpty(t, get("?target_id="+notes+"&since="+hourAgo+"&until="+inAnHour).Events)
	require.Empty(t, get("?target_id="+notes+"&since="+inAnHour).Events)
	require.Empty(t, get("?target_id="+notes+"&until="+hourAgo).Events)

	// By kind of actor: every event here was a person's, none the system's.
	require.NotEmpty(t, get("?principal_kind=user&target_id="+notes).Events)
	require.Empty(t, get("?principal_kind=system&target_id="+notes).Events)

	// A bound that does not parse is refused rather than ignored.
	bad := i.do(admin, http.MethodGet, "/audit?since=yesterday", nil)
	require.Equal(t, http.StatusBadRequest, bad.Code, bad.String())
}

// TestR227_TheAuditLogFindsEverythingToDoWithOneAccount asserts that
// `involving` reads one account's whole history — what it did, and what was
// done to it — which the actor and target filters, combined with AND, cannot.
func TestR227_TheAuditLogFindsEverythingToDoWithOneAccount(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	dana := i.user("dana")
	danaID := i.userID(dana)
	i.createApp(admin, "notes")

	// Something done to her; signing in, above, was something she did.
	require.Equal(t, http.StatusNoContent,
		i.do(admin, http.MethodPatch, "/users/"+danaID, map[string]string{"status": "active"}).Code)

	var page struct {
		Events []struct {
			Action      string `json:"action"`
			PrincipalID string `json:"principal_id"`
			OnBehalfOf  string `json:"on_behalf_of"`
			TargetID    string `json:"target_id"`
		} `json:"events"`
	}
	got := i.do(admin, http.MethodGet, "/audit?involving="+danaID, nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	got.JSON(t, &page)

	var asActor, asTarget bool
	for _, e := range page.Events {
		switch danaID {
		case e.PrincipalID, e.OnBehalfOf:
			asActor = true
		case e.TargetID:
			asTarget = true
		default:
			t.Fatalf("%s neither by nor on %s: %+v", e.Action, danaID, e)
		}
	}
	require.True(t, asActor, "an event she was the actor of")
	require.True(t, asTarget, "an event she was the target of")

	// It combines with the others like any filter.
	got = i.do(admin, http.MethodGet, "/audit?involving="+danaID+"&action=user.update", nil)
	got.JSON(t, &page)
	require.Len(t, page.Events, 1)
}

// GET /users/{id} is the same shape as a row of GET /users: an account's page
// needs its role and when it was made, and should not need the whole list.
func TestOneAccountCarriesItsRoleAndWhenItWasMade(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	var one struct {
		ID            string    `json:"id"`
		InstallRoleID string    `json:"install_role_id"`
		CreatedAt     time.Time `json:"created_at"`
	}
	got := i.do(admin, http.MethodGet, "/users/"+i.AdminID, nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	got.JSON(t, &one)
	require.Equal(t, i.AdminID, one.ID)
	require.NotEmpty(t, one.InstallRoleID)
	require.WithinDuration(t, time.Now(), one.CreatedAt, time.Hour)

	var list struct {
		Users []struct {
			ID            string    `json:"id"`
			InstallRoleID string    `json:"install_role_id"`
			CreatedAt     time.Time `json:"created_at"`
		} `json:"users"`
	}
	i.do(admin, http.MethodGet, "/users", nil).JSON(t, &list)
	for _, u := range list.Users {
		if u.ID == one.ID {
			require.Equal(t, one.InstallRoleID, u.InstallRoleID)
			require.True(t, one.CreatedAt.Equal(u.CreatedAt))
		}
	}
}
