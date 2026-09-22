//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type launcherApp struct {
	ID       string `json:"id"`
	Favorite bool   `json:"favorite"`
}

func (i *install) launcher(s *session) map[string]bool {
	i.t.Helper()
	var mine struct {
		Apps []launcherApp `json:"apps"`
	}
	got := i.do(s, http.MethodGet, "/me/apps", nil)
	require.Equal(i.t, http.StatusOK, got.Code, got.String())
	got.JSON(i.t, &mine)
	out := map[string]bool{}
	for _, a := range mine.Apps {
		out[a.ID] = a.Favorite
	}
	return out
}

// TestR341_FavoritesArePerPersonAndGrantNothing asserts R-341: a person pins
// apps they can open, sees them marked in their own launcher and nobody
// else's, and cannot pin — or learn of — an app they cannot open.
func TestR341_FavoritesArePerPersonAndGrantNothing(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	notes := i.createApp(admin, "notes")
	wiki := i.createApp(admin, "wiki")

	// Pinning is idempotent and shows up in the launcher list.
	for range 2 {
		got := i.do(admin, http.MethodPut, "/me/favorites/"+notes, nil)
		require.Equal(t, http.StatusNoContent, got.Code, got.String())
	}
	require.Equal(t, map[string]bool{notes: true, wiki: false}, i.launcher(admin))

	// Someone the app is not shared with cannot pin it, and is told the same
	// thing as for an app that does not exist.
	other := i.user("ordinary")
	refused := i.do(other, http.MethodPut, "/me/favorites/"+notes, nil)
	require.Equal(t, http.StatusNotFound, refused.Code, refused.String())
	missing := i.do(other, http.MethodPut, "/me/favorites/app_01JZZZZZZZZZZZZZZZZZZZZZZZ", nil)
	require.Equal(t, http.StatusNotFound, missing.Code, missing.String())

	// Shared with them: their launcher shows it, not pinned — the admin's
	// favorite is the admin's.
	var me struct {
		UserID string `json:"user_id"`
	}
	i.do(other, http.MethodGet, "/me", nil).JSON(t, &me)
	granted := i.do(admin, http.MethodPost, "/apps/"+notes+"/grants", map[string]any{
		"plane": "data", "principal_kind": "user", "principal_id": me.UserID,
	})
	require.Equal(t, http.StatusCreated, granted.Code, granted.String())
	require.Equal(t, map[string]bool{notes: false}, i.launcher(other))

	// Pinning it now works, and grants them nothing on the control plane.
	require.Equal(t, http.StatusNoContent, i.do(other, http.MethodPut, "/me/favorites/"+notes, nil).Code)
	require.Equal(t, map[string]bool{notes: true}, i.launcher(other))
	require.Equal(t, http.StatusNotFound, i.do(other, http.MethodGet, "/apps/"+notes, nil).Code)

	// Unpinning is idempotent too.
	for range 2 {
		got := i.do(admin, http.MethodDelete, "/me/favorites/"+notes, nil)
		require.Equal(t, http.StatusNoContent, got.Code, got.String())
	}
	require.Equal(t, map[string]bool{notes: false, wiki: false}, i.launcher(admin))
	require.Equal(t, map[string]bool{notes: true}, i.launcher(other))
}

// Favorites need a person. The anonymous principal has no launcher.
func TestR341_FavoritesNeedASignedInPerson(t *testing.T) {
	i := newInstall(t)
	notes := i.createApp(i.admin(), "notes")

	got := i.anon(http.MethodPut, "/me/favorites/"+notes, nil)
	require.Equal(t, http.StatusUnauthorized, got.Code, got.String())
}
