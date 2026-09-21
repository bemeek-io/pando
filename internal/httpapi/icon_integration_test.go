//go:build integration

package httpapi_test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/httpapi"
)

// A 1×1 transparent PNG.
var onePixelPNG, _ = base64.StdEncoding.DecodeString(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=")

// raw sends a body as-is, which the JSON helper cannot: an icon upload is the
// image bytes.
func (i *install) raw(s *session, method, path, contentType string, body []byte) reply {
	i.t.Helper()
	req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if s != nil && s.cookie != "" {
		req.Header.Set("Cookie", httpapi.SessionCookie+"="+s.cookie)
	}
	rec := httptest.NewRecorder()
	i.handler.ServeHTTP(rec, req)
	return reply{Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

// TestR340_AppImageSetByAppAdminSeenByAppUsers asserts R-340: an app's image
// is set with app.spec.edit, shown to whoever can open the app, and hidden —
// as not-found — from everyone else.
func TestR340_AppImageSetByAppAdminSeenByAppUsers(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")

	// No image yet: the app says so, and there is nothing to load.
	var app struct {
		IconUpdatedAt *string `json:"icon_updated_at"`
	}
	i.do(admin, http.MethodGet, "/apps/"+appID, nil).JSON(t, &app)
	require.Nil(t, app.IconUpdatedAt)
	require.Equal(t, http.StatusNotFound, i.do(admin, http.MethodGet, "/apps/"+appID+"/icon", nil).Code)

	// The owner sets one. The type comes from the bytes, not the header.
	set := i.raw(admin, http.MethodPut, "/apps/"+appID+"/icon", "application/octet-stream", onePixelPNG)
	require.Equal(t, http.StatusOK, set.Code, set.String())
	set.JSON(t, &app)
	require.NotNil(t, app.IconUpdatedAt)

	got := i.do(admin, http.MethodGet, "/apps/"+appID+"/icon", nil)
	require.Equal(t, http.StatusOK, got.Code)
	require.Equal(t, "image/png", got.Hdr.Get("Content-Type"))
	require.Equal(t, "nosniff", got.Hdr.Get("X-Content-Type-Options"))
	require.Equal(t, onePixelPNG, got.Body)

	// The launcher list carries the timestamp too.
	var mine struct {
		Apps []struct {
			ID            string  `json:"id"`
			IconUpdatedAt *string `json:"icon_updated_at"`
		} `json:"apps"`
	}
	i.do(admin, http.MethodGet, "/me/apps", nil).JSON(t, &mine)
	require.Len(t, mine.Apps, 1)
	require.NotNil(t, mine.Apps[0].IconUpdatedAt)

	// Somebody the app is not shared with cannot load it, and is not told it
	// exists; nor can they change it.
	other := i.user("ordinary")
	require.Equal(t, http.StatusNotFound, i.do(other, http.MethodGet, "/apps/"+appID+"/icon", nil).Code)
	require.Equal(t, http.StatusNotFound,
		i.raw(other, http.MethodPut, "/apps/"+appID+"/icon", "image/png", onePixelPNG).Code)

	// Shared on the data plane: they see the image, and still cannot change it.
	var me struct {
		UserID string `json:"user_id"`
	}
	i.do(other, http.MethodGet, "/me", nil).JSON(t, &me)
	granted := i.do(admin, http.MethodPost, "/apps/"+appID+"/grants", map[string]any{
		"plane": "data", "principal_kind": "user", "principal_id": me.UserID,
	})
	require.Equal(t, http.StatusCreated, granted.Code, granted.String())
	require.Equal(t, http.StatusOK, i.do(other, http.MethodGet, "/apps/"+appID+"/icon", nil).Code)
	require.Equal(t, http.StatusNotFound,
		i.raw(other, http.MethodPut, "/apps/"+appID+"/icon", "image/png", onePixelPNG).Code)

	// Clearing it puts the app back to no image.
	cleared := i.do(admin, http.MethodDelete, "/apps/"+appID+"/icon", nil)
	require.Equal(t, http.StatusOK, cleared.Code, cleared.String())
	require.Equal(t, http.StatusNotFound, i.do(admin, http.MethodGet, "/apps/"+appID+"/icon", nil).Code)
}

// TestR340_AppImageRefusesWhatIsNotARasterImage asserts the limits on R-340's
// image: SVG and non-images are refused by what the bytes are, whatever the
// header claims, and so is anything over 256 KB.
func TestR340_AppImageRefusesWhatIsNotARasterImage(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	refused := i.raw(admin, http.MethodPut, "/apps/"+appID+"/icon", "image/png", svg)
	require.Equal(t, http.StatusBadRequest, refused.Code, refused.String())
	require.Contains(t, refused.String(), "PNG, JPEG, WebP or GIF")

	big := append(append([]byte{}, onePixelPNG...), make([]byte, 256<<10)...)
	tooBig := i.raw(admin, http.MethodPut, "/apps/"+appID+"/icon", "image/png", big)
	require.Equal(t, http.StatusBadRequest, tooBig.Code, tooBig.String())
	require.Contains(t, tooBig.String(), "256 KB")

	empty := i.raw(admin, http.MethodPut, "/apps/"+appID+"/icon", "image/png", nil)
	require.Equal(t, http.StatusBadRequest, empty.Code, empty.String())
}
