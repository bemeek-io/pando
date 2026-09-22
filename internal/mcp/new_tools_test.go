package mcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every tool this branch added refuses a call missing a required argument —
// or with one of the wrong type — before reaching the API, and says which.
func TestNewToolsRefuseMissingArgumentsWithoutCallingTheAPI(t *testing.T) {
	for _, tc := range []struct{ tool, args, want string }{
		{"pando_set_app_icon", `{}`, "app_id"},
		{"pando_set_app_icon", `{"app_id":"app_1"}`, "image_base64"},
		{"pando_clear_app_icon", `{}`, "app_id"},
		{"pando_favorite_app", `{}`, "app_id"},
		{"pando_unfavorite_app", `{}`, "app_id"},
		{"pando_rename_app", `{}`, "app_id"},
		{"pando_rename_app", `{"app_id":"app_1"}`, "name"},
		{"pando_create_section", `{}`, "name"},
		{"pando_rename_section", `{}`, "section_id"},
		{"pando_rename_section", `{"section_id":"sect_1"}`, "name"},
		{"pando_delete_section", `{}`, "section_id"},
		{"pando_add_app_to_section", `{}`, "section_id"},
		{"pando_add_app_to_section", `{"section_id":"sect_1"}`, "app_id"},
		{"pando_remove_app_from_section", `{}`, "section_id"},
		{"pando_remove_app_from_section", `{"section_id":"sect_1"}`, "app_id"},
		{"pando_list_audit", `{"since":7}`, "since"},
	} {
		t.Run(tc.tool+" "+tc.args, func(t *testing.T) {
			srv, s := newSession()
			replies := s.run(t, srv, call(1, tc.tool, tc.args))
			require.Empty(t, s.calls, "nothing reaches the API")
			require.Equal(t, true, result(t, replies[0])["isError"])
			require.Contains(t, text(t, replies[0]), tc.want)
		})
	}
}
