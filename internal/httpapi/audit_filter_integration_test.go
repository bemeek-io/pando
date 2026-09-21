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

	// A bound that does not parse is refused rather than ignored.
	bad := i.do(admin, http.MethodGet, "/audit?since=yesterday", nil)
	require.Equal(t, http.StatusBadRequest, bad.Code, bad.String())
}
