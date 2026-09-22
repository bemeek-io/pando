package cli_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// R-342: sections are reachable from the CLI (R-261). Each subcommand is the
// one API call it names.
func TestR342_SectionCommandsCallTheirEndpoints(t *testing.T) {
	api := newAPI(t).
		reply("GET /me/apps", map[string]any{
			"apps": []map[string]any{
				{"id": "app_1", "name": "Notes", "section_id": "sect_1", "favorite": true},
				{"id": "app_2", "name": "Wiki"},
			},
			"sections": []map[string]any{{"id": "sect_1", "name": "Work"}, {"id": "sect_2", "name": "Empty"}},
		}).
		reply("POST /me/sections", map[string]any{"id": "sect_3", "name": "Home"})

	got := run(t, api, "", "section", "list")
	require.NoError(t, got.err, got.errOut)
	require.Contains(t, got.out, "Work")
	require.Contains(t, got.out, "Notes")
	require.Contains(t, got.out, "yes", "the favorite is marked")
	require.Contains(t, got.out, "Your apps", "an unfiled app is under Your apps")
	require.Contains(t, got.out, "sect_2", "an empty section is still listed, with its ID")

	for _, tc := range []struct {
		args []string
		call string
		body string
		out  string
	}{
		{[]string{"create", "Home"}, "POST /me/sections", `{"name":"Home"}`, "sect_3"},
		{[]string{"rename", "sect_1", "Office"}, "PATCH /me/sections/sect_1", `{"name":"Office"}`, "Office"},
		{[]string{"delete", "sect_1"}, "DELETE /me/sections/sect_1", "", "back under Your apps"},
		{[]string{"add", "sect_1", "app_2"}, "PUT /me/sections/sect_1/apps/app_2", "", "Moved app_2"},
		{[]string{"remove", "sect_1", "app_1"}, "DELETE /me/sections/sect_1/apps/app_1", "", "back to Your apps"},
	} {
		got := run(t, api, "", "section", tc.args...)
		require.NoError(t, got.err, "%v: %s", tc.args, got.errOut)
		require.Contains(t, got.out, tc.out, tc.args)
		if tc.body != "" {
			require.JSONEq(t, tc.body, api.bodyFor(tc.call), tc.args)
		} else {
			api.bodyFor(tc.call) // fails the test if the call was not made
		}
	}
}

func TestSectionCommandsReportTheServersRefusal(t *testing.T) {
	api := newAPI(t)
	for _, key := range []string{
		"GET /me/apps", "POST /me/sections", "PATCH /me/sections/sect_1", "DELETE /me/sections/sect_1",
		"PUT /me/sections/sect_1/apps/app_1", "DELETE /me/sections/sect_1/apps/app_1",
	} {
		api.fail(key, http.StatusNotFound, map[string]string{"code": "NOT_FOUND", "message": "You have no section with that ID."})
	}
	for _, args := range [][]string{
		{"list"}, {"create", "Home"}, {"rename", "sect_1", "Office"}, {"delete", "sect_1"},
		{"add", "sect_1", "app_1"}, {"remove", "sect_1", "app_1"},
	} {
		got := run(t, api, "", "section", args...)
		require.Error(t, got.err, args)
	}
}

// Renaming, favorites and the tile image, from the CLI (R-261, R-340, R-341).
func TestAppRenameFavoriteAndIconCommands(t *testing.T) {
	api := newAPI(t)

	got := run(t, api, "", "app", "rename", "app_1", "Team notes")
	require.NoError(t, got.err, got.errOut)
	require.JSONEq(t, `{"name":"Team notes"}`, api.bodyFor("PATCH /apps/app_1"))

	require.NoError(t, run(t, api, "", "app", "favorite", "app_1").err)
	require.True(t, api.sawPath("/me/favorites/app_1"))
	got = run(t, api, "", "app", "unfavorite", "app_1")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "Unpinned")

	image := filepath.Join(t.TempDir(), "icon.png")
	require.NoError(t, os.WriteFile(image, []byte("\x89PNG\r\n\x1a\n"), 0o600))
	got = run(t, api, "", "app", "icon", "set", "app_1", image)
	require.NoError(t, got.err, got.errOut)
	require.Equal(t, "\x89PNG\r\n\x1a\n", api.bodyFor("PUT /apps/app_1/icon"), "the file itself, not JSON")

	require.NoError(t, run(t, api, "", "app", "icon", "clear", "app_1").err)
	require.True(t, api.sawPath("/apps/app_1/icon"))

	// A file that is not there is an error before any request.
	require.Error(t, run(t, api, "", "app", "icon", "set", "app_1", filepath.Join(t.TempDir(), "missing.png")).err)

	// And the server's refusal of an image reaches the person.
	api.fail("PUT /apps/app_1/icon", http.StatusBadRequest, map[string]string{
		"code": "VALID_INVALID", "message": "An app's image must be a PNG, JPEG, WebP or GIF file.",
	})
	got = run(t, api, "", "app", "icon", "set", "app_1", image)
	require.ErrorContains(t, got.err, "PNG, JPEG, WebP or GIF")

	for _, args := range [][]string{{"rename", "app_1", "x"}, {"favorite", "app_1"}, {"icon", "clear", "app_1"}} {
		key := map[string]string{"rename": "PATCH /apps/app_1", "favorite": "PUT /me/favorites/app_1", "icon": "DELETE /apps/app_1/icon"}[args[0]]
		api.fail(key, http.StatusNotFound, map[string]string{"code": "NOT_FOUND", "message": "There is no app with that ID."})
		require.Error(t, run(t, api, "", "app", args...).err, args)
	}
}

// R-271: pando config shows each setting, where it came from, and what is
// fixed in the startup policy.
func TestR271_ConfigShowsSettingsSourcesAndFixedPolicy(t *testing.T) {
	api := newAPI(t).reply("GET /config", map[string]any{
		"file": "/etc/pando/pando.yaml",
		"settings": []map[string]any{
			{"key": "log.level", "value": "info", "source": map[string]string{"kind": "default"}, "env": "PANDO_LOG_LEVEL"},
			{"key": "server.base_domain", "value": "example.test", "source": map[string]string{"kind": "env", "name": "PANDO_SERVER_BASE_DOMAIN"}},
			{"key": "server.routing_mode", "value": "port", "source": map[string]string{"kind": "file", "name": "/etc/pando/pando.yaml", "key": "server.routing_mode"}},
		},
		"policy": []map[string]any{
			{"key": "min_security_score", "value": 80, "source": map[string]string{"kind": "env", "name": "PANDO_POLICY_MIN_SECURITY_SCORE"}},
		},
	})

	got := run(t, api, "", "config")
	require.NoError(t, got.err, got.errOut)
	for _, want := range []string{
		"Config file: /etc/pando/pando.yaml",
		"default (set with PANDO_LOG_LEVEL)",
		"env PANDO_SERVER_BASE_DOMAIN",
		"file /etc/pando/pando.yaml (server.routing_mode)",
		"FIXED POLICY", "min_security_score", "env PANDO_POLICY_MIN_SECURITY_SCORE",
		"docker compose up -d pando",
	} {
		require.Contains(t, got.out, want)
	}

	// With no file and nothing fixed, it says so and lists no policy table.
	none := newAPI(t).reply("GET /config", map[string]any{"file": "", "settings": []any{}, "policy": []any{}})
	got = run(t, none, "", "config")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "Config file: none")
	require.NotContains(t, got.out, "FIXED POLICY")

	refused := newAPI(t).fail("GET /config", http.StatusForbidden, map[string]string{"code": "PERM_DENIED", "message": "This needs install.view."})
	require.ErrorContains(t, run(t, refused, "", "config").err, "install.view")
}
