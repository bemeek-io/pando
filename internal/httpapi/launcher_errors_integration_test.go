//go:build integration

package httpapi_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/httpapi"
)

// rawJSON sends a body that is not the JSON the endpoint wants.
func (i *install) rawJSON(s *session, method, path, body string) reply {
	i.t.Helper()
	req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if s != nil && s.cookie != "" {
		req.Header.Set("Cookie", httpapi.SessionCookie+"="+s.cookie)
	}
	if s != nil && s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	rec := httptest.NewRecorder()
	i.handler.ServeHTTP(rec, req)
	return reply{Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

func (i *install) serviceToken(admin *session) *session {
	i.t.Helper()
	created := i.do(admin, http.MethodPost, "/tokens/service", map[string]any{"name": "CI"})
	require.Equal(i.t, http.StatusCreated, created.Code, created.String())
	var tok struct {
		Secret string `json:"secret"`
	}
	created.JSON(i.t, &tok)
	return &session{token: tok.Secret}
}

// R-341, R-342: favorites and sections belong to a person. Anything that is
// not one — the anonymous principal, a service token — is refused with a
// reason, and so is every malformed or unknown ID, as not-found.
func TestR342_PersonalEndpointsRefuseWhatIsNotAPersonOrNotThere(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")
	section := i.createSection(admin, "Work")
	service := i.serviceToken(admin)

	for _, who := range []*session{nil, service} {
		for _, r := range []struct{ method, path string }{
			{http.MethodPut, "/me/favorites/" + appID},
			{http.MethodDelete, "/me/favorites/" + appID},
			{http.MethodPost, "/me/sections"},
			{http.MethodPatch, "/me/sections/" + section},
			{http.MethodDelete, "/me/sections/" + section},
			{http.MethodPut, "/me/sections/" + section + "/apps/" + appID},
			{http.MethodDelete, "/me/sections/" + section + "/apps/" + appID},
		} {
			got := i.do(who, r.method, r.path, map[string]string{"name": "x"})
			require.Equal(t, http.StatusUnauthorized, got.Code, "%s %s: %s", r.method, r.path, got.String())
		}
	}

	missing := "app_01JZZZZZZZZZZZZZZZZZZZZZZZ"
	for _, r := range []struct{ method, path string }{
		{http.MethodPut, "/me/favorites/not-an-id"},
		{http.MethodDelete, "/me/favorites/not-an-id"},
		{http.MethodPatch, "/me/sections/not-an-id"},
		{http.MethodDelete, "/me/sections/not-an-id"},
		{http.MethodPut, "/me/sections/not-an-id/apps/" + appID},
		{http.MethodPut, "/me/sections/" + section + "/apps/not-an-id"},
		{http.MethodPut, "/me/sections/" + section + "/apps/" + missing},
		{http.MethodPut, "/me/sections/sect_01JZZZZZZZZZZZZZZZZZZZZZZZ/apps/" + appID},
		{http.MethodDelete, "/me/sections/" + section + "/apps/not-an-id"},
		{http.MethodDelete, "/me/sections/not-an-id/apps/" + appID},
		{http.MethodPatch, "/me/sections/sect_01JZZZZZZZZZZZZZZZZZZZZZZZ"},
	} {
		got := i.do(admin, r.method, r.path, map[string]string{"name": "Renamed"})
		require.Equal(t, http.StatusNotFound, got.Code, "%s %s: %s", r.method, r.path, got.String())
	}

	// A body that is not JSON, and a rename to a name already taken.
	require.Equal(t, http.StatusBadRequest, i.rawJSON(admin, http.MethodPost, "/me/sections", "{").Code)
	require.Equal(t, http.StatusBadRequest, i.rawJSON(admin, http.MethodPatch, "/me/sections/"+section, "{").Code)
	i.createSection(admin, "Home")
	taken := i.do(admin, http.MethodPatch, "/me/sections/"+section, map[string]string{"name": "Home"})
	require.Equal(t, http.StatusBadRequest, taken.Code, taken.String())
	require.Contains(t, taken.String(), "already have a section")
	tooLong := i.do(admin, http.MethodPost, "/me/sections", map[string]string{"name": string(bytes.Repeat([]byte("x"), 81))})
	require.Equal(t, http.StatusBadRequest, tooLong.Code, tooLong.String())
}

// R-340: the image endpoints' refusals — an ID that is not one, an app that
// does not exist, an app with no image, and an app somebody cannot see.
func TestR340_ImageEndpointsRefuseWhatIsNotThere(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")

	for _, path := range []string{"/apps/not-an-id/icon", "/apps/app_01JZZZZZZZZZZZZZZZZZZZZZZZ/icon", "/apps/" + appID + "/icon"} {
		got := i.do(admin, http.MethodGet, path, nil)
		require.Equal(t, http.StatusNotFound, got.Code, "%s: %s", path, got.String())
	}
	require.Equal(t, http.StatusNotFound, i.do(admin, http.MethodDelete, "/apps/app_01JZZZZZZZZZZZZZZZZZZZZZZZ/icon", nil).Code)
	require.Equal(t, http.StatusNotFound, i.raw(admin, http.MethodPut, "/apps/not-an-id/icon", "image/png", onePixelPNG).Code)

	// Clearing an app that has no image is not an error: the outcome holds.
	require.Equal(t, http.StatusOK, i.do(admin, http.MethodDelete, "/apps/"+appID+"/icon", nil).Code)

	// Somebody who cannot open or manage it gets not-found for its image and
	// is refused changing it.
	other := i.user("ordinary")
	require.Equal(t, http.StatusNotFound, i.do(other, http.MethodDelete, "/apps/"+appID+"/icon", nil).Code)
}

// R-271: GET /config on an install started with no startup configuration
// answers empty lists rather than null.
func TestR271_ConfigWithNothingSetIsEmptyNotNull(t *testing.T) {
	i := newInstall(t)
	got := i.do(i.admin(), http.MethodGet, "/config", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	require.JSONEq(t, `{"file":"","settings":[],"policy":[],"adapters":[]}`, got.String())
}
