//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type myLauncher struct {
	Apps []struct {
		ID        string `json:"id"`
		SectionID string `json:"section_id"`
	} `json:"apps"`
	Sections []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"sections"`
}

func (i *install) myLauncher(s *session) (myLauncher, map[string]string) {
	i.t.Helper()
	var out myLauncher
	got := i.do(s, http.MethodGet, "/me/apps", nil)
	require.Equal(i.t, http.StatusOK, got.Code, got.String())
	got.JSON(i.t, &out)
	placed := map[string]string{}
	for _, a := range out.Apps {
		placed[a.ID] = a.SectionID
	}
	return out, placed
}

func (i *install) createSection(s *session, name string) string {
	i.t.Helper()
	got := i.do(s, http.MethodPost, "/me/sections", map[string]string{"name": name})
	require.Equal(i.t, http.StatusCreated, got.Code, got.String())
	var section struct {
		ID string `json:"id"`
	}
	got.JSON(i.t, &section)
	return section.ID
}

// TestR342_SectionsGroupAPersonsOwnLauncher asserts R-342: a person makes
// sections, files apps into them one section per app, renames and deletes
// them, and deleting one returns its apps to "Your apps".
func TestR342_SectionsGroupAPersonsOwnLauncher(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	notes := i.createApp(admin, "notes")
	wiki := i.createApp(admin, "wiki")

	// No sections: everything is under "Your apps", and the list says so.
	before, placed := i.myLauncher(admin)
	require.Empty(t, before.Sections)
	require.Equal(t, map[string]string{notes: "", wiki: ""}, placed)

	work := i.createSection(admin, "  Work  ")
	home := i.createSection(admin, "Home")

	// The name is trimmed, and a second section by the same name is refused.
	dup := i.do(admin, http.MethodPost, "/me/sections", map[string]string{"name": "Work"})
	require.Equal(t, http.StatusBadRequest, dup.Code, dup.String())
	empty := i.do(admin, http.MethodPost, "/me/sections", map[string]string{"name": "   "})
	require.Equal(t, http.StatusBadRequest, empty.Code, empty.String())

	// Filing moves: one section per app.
	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodPut, "/me/sections/"+work+"/apps/"+notes, nil).Code)
	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodPut, "/me/sections/"+home+"/apps/"+notes, nil).Code)
	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodPut, "/me/sections/"+work+"/apps/"+wiki, nil).Code)
	listed, placed := i.myLauncher(admin)
	require.Equal(t, map[string]string{notes: home, wiki: work}, placed)
	require.Len(t, listed.Sections, 2)
	require.Equal(t, "Work", listed.Sections[0].Name, "sections come back in the order they were made")

	// Renaming.
	renamed := i.do(admin, http.MethodPatch, "/me/sections/"+work, map[string]string{"name": "Office"})
	require.Equal(t, http.StatusOK, renamed.Code, renamed.String())
	listed, _ = i.myLauncher(admin)
	require.Equal(t, "Office", listed.Sections[0].Name)

	// Taking one out puts it back under "Your apps".
	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodDelete, "/me/sections/"+home+"/apps/"+notes, nil).Code)
	_, placed = i.myLauncher(admin)
	require.Equal(t, map[string]string{notes: "", wiki: work}, placed)

	// Deleting a section returns its apps rather than taking them with it.
	require.Equal(t, http.StatusNoContent, i.do(admin, http.MethodDelete, "/me/sections/"+work, nil).Code)
	listed, placed = i.myLauncher(admin)
	require.Equal(t, map[string]string{notes: "", wiki: ""}, placed)
	require.Len(t, listed.Sections, 1)
	require.Equal(t, http.StatusNotFound, i.do(admin, http.MethodDelete, "/me/sections/"+work, nil).Code)
}

// TestR342_SectionsAreNobodyElses asserts that a section is its maker's alone:
// another person cannot see it, rename it, delete it or file into it, and
// cannot file an app they cannot open into their own.
func TestR342_SectionsAreNobodyElses(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	notes := i.createApp(admin, "notes")
	mine := i.createSection(admin, "Mine")

	other := i.user("ordinary")
	theirs, _ := i.myLauncher(other)
	require.Empty(t, theirs.Sections)

	require.Equal(t, http.StatusNotFound,
		i.do(other, http.MethodPatch, "/me/sections/"+mine, map[string]string{"name": "Taken"}).Code)
	require.Equal(t, http.StatusNotFound, i.do(other, http.MethodDelete, "/me/sections/"+mine, nil).Code)

	// Their own section, and an app they cannot open: not-found, as for an
	// app that does not exist.
	own := i.createSection(other, "Mine")
	require.Equal(t, http.StatusNotFound, i.do(other, http.MethodPut, "/me/sections/"+own+"/apps/"+notes, nil).Code)

	// Shared with them, they can file it — into their own section, not the
	// admin's.
	var me struct {
		UserID string `json:"user_id"`
	}
	i.do(other, http.MethodGet, "/me", nil).JSON(t, &me)
	granted := i.do(admin, http.MethodPost, "/apps/"+notes+"/grants", map[string]any{
		"plane": "data", "principal_kind": "user", "principal_id": me.UserID,
	})
	require.Equal(t, http.StatusCreated, granted.Code, granted.String())
	require.Equal(t, http.StatusNotFound, i.do(other, http.MethodPut, "/me/sections/"+mine+"/apps/"+notes, nil).Code)
	require.Equal(t, http.StatusNoContent, i.do(other, http.MethodPut, "/me/sections/"+own+"/apps/"+notes, nil).Code)

	// And where they filed it is theirs: the admin's launcher is unchanged.
	_, adminPlaced := i.myLauncher(admin)
	require.Equal(t, "", adminPlaced[notes])
	_, otherPlaced := i.myLauncher(other)
	require.Equal(t, own, otherPlaced[notes])
}
