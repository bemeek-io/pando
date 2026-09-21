package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/httpapi"
	"github.com/bemeek-io/pando/internal/reference"
)

// The docs are generated so they cannot drift from the code (R-261), and
// `TestR261_EveryRouteIsDocumented` holds the route table to the router. What
// nothing held was the rendering: a route could be in the Document, pass that
// test, and be dropped on the way to the page somebody reads.

// TestR261_EveryRouteReachesThePage asserts R-261.
func TestR261_EveryRouteReachesThePage(t *testing.T) {
	doc := httpapi.Reference()
	page := api(doc)

	require.NotEmpty(t, doc.API.Routes)
	for _, route := range doc.API.Routes {
		require.Contains(t, page, "`"+route.Method+" "+route.Path+"`",
			"%s %s is in the reference and not on the page", route.Method, route.Path)
		require.Contains(t, page, route.Summary)
	}

	// Each group gets a heading, and no group is rendered twice.
	for _, group := range groups(doc.API.Routes) {
		require.Equal(t, 1, strings.Count(page, "### "+group+"\n"), group)
	}

	// Every error code, because the envelope is what a client branches on.
	require.NotEmpty(t, doc.Errors)
	for _, e := range doc.Errors {
		require.Contains(t, page, "`"+string(e.Code)+"`")
	}
}

// A command's subcommands are nested under it, and a flag is rendered with its
// shorthand — the form somebody types.
func TestTheCLIPageNestsSubcommandsAndNamesFlags(t *testing.T) {
	page := cliDoc(reference.Document{
		CLI: []reference.Command{{
			Name: "app", Summary: "Work with apps", Use: "pando app",
			Children: []reference.Command{{
				Name: "stop", Summary: "Stop an app without deleting it",
				Use:     "pando app stop <app>",
				Details: "Nothing is removed.",
				Flags: []reference.Flag{
					{Name: "workload", Shorthand: "w", Description: "which part to read"},
					{Name: "follow", Shorthand: "f", Default: "false", Description: "keep it open"},
					{Name: "tail", Default: "200", Description: "how many lines"},
				},
			}},
		}},
	})

	require.Contains(t, page, "### `app`")
	require.Contains(t, page, "#### `stop`", "a subcommand sits under its parent")
	require.Contains(t, page, "pando app stop <app>")
	require.Contains(t, page, "Nothing is removed.")

	require.Contains(t, page, "`-w`, `--workload`")
	require.Contains(t, page, "`200`", "a real default is shown")

	// `false` is not a default worth printing: every boolean flag has it, and a
	// column of them says nothing.
	require.NotContains(t, page, "`false`")
}

// A command whose long text repeats its summary prints it once.
func TestACommandDoesNotSayTheSameThingTwice(t *testing.T) {
	var b strings.Builder
	writeCommand(&b, reference.Command{
		Name: "plan", Summary: "Show what a deploy would do", Use: "pando plan <app>",
		Details: "Show what a deploy would do",
	}, 3)

	require.Equal(t, 1, strings.Count(b.String(), "Show what a deploy would do"))
}

// The MCP page lists every tool with its arguments, and says which are
// required — an agent reading this has nothing else to go on.
func TestTheMCPPageMarksOptionalArguments(t *testing.T) {
	doc := httpapi.Reference()
	page := mcpDoc(doc)

	require.NotEmpty(t, doc.MCP)
	for _, tool := range doc.MCP {
		require.Contains(t, page, "`"+tool.Name+"`")
	}

	require.Equal(t, "`app_id`, `workload` (optional)", arguments(map[string]any{
		"properties": map[string]any{"app_id": nil, "workload": nil},
		"required":   []string{"app_id"},
	}))
	require.Equal(t, "none", arguments(map[string]any{}))
	require.Equal(t, "`a` (optional), `b` (optional)", arguments(map[string]any{
		"properties": map[string]any{"b": nil, "a": nil},
	}), "sorted, so the page does not reshuffle between runs")
}

// TestR002_TheInstallRecipesAreRunnableAsPrinted asserts R-002.
//
// An instruction with `<version>` in it is one somebody has to assemble before
// they can run it, and assembling it is where they get it wrong. The recipes
// are written out for a real format and architecture, with the one thing that
// is genuinely theirs — which release — as a shell variable above the command.
func TestR002_TheInstallRecipesAreRunnableAsPrinted(t *testing.T) {
	in := reference.Install{
		Download: "https://github.com/bemeek-io/pando/releases/download/v<version>/",
		Package:  "pando_<version>_linux_<arch>.<format>",
		Archive:  "pando_<version>_<os>_<arch>.tar.gz",
	}

	deb := packageRecipe(in, "deb", "amd64")
	require.Contains(t, deb, "VERSION=")
	require.Contains(t, deb, "pando_${VERSION}_linux_amd64.deb")
	require.NotContains(t, deb, "<version>")
	require.NotContains(t, deb, "<arch>")

	tar := archiveRecipe(in, "darwin", "arm64")
	require.Contains(t, tar, "pando_${VERSION}_darwin_arm64.tar.gz")
	require.NotContains(t, tar, "<os>")
}

// Prose lists, in the two forms the page uses.
func TestListsReadAsProse(t *testing.T) {
	require.Equal(t, "`deb`", list([]string{"deb"}, "`%s`"))
	require.Equal(t, "`deb` and `rpm`", list([]string{"deb", "rpm"}, "`%s`"))
	require.Equal(t, "`deb`, `rpm` and `apk`", list([]string{"deb", "rpm", "apk"}, "`%s`"))
	require.Empty(t, list(nil, "`%s`"))
}
