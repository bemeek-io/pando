//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR261_TheReferenceIsServedToAnyoneSignedIn asserts that the manual is not
// an administrative feature.
//
// R-261 makes the API the product and R-262 makes an agent holding a token an
// ordinary principal. A developer with one app shared with them automates
// against this API and mints a token to do it; putting the description of it
// behind an install verb would leave them reading someone's screenshot.
func TestR261_TheReferenceIsServedToAnyoneSignedIn(t *testing.T) {
	i := newInstall(t)
	ordinary := i.user("ordinary")

	got := i.do(ordinary, http.MethodGet, "/reference", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())

	var doc struct {
		API struct {
			BasePath string `json:"base_path"`
			Routes   []struct {
				Method  string `json:"method"`
				Path    string `json:"path"`
				Group   string `json:"group"`
				Summary string `json:"summary"`
			} `json:"routes"`
			Auth []struct {
				Name string `json:"name"`
			} `json:"auth"`
		} `json:"api"`
		CLI []struct {
			Name string `json:"name"`
		} `json:"cli"`
		MCP []struct {
			Name string `json:"name"`
		} `json:"mcp"`
		Errors []struct {
			Code   string `json:"code"`
			Status int    `json:"status"`
		} `json:"errors"`
		Connect struct {
			ServerEnv string `json:"server_env"`
			TokenEnv  string `json:"token_env"`
		} `json:"connect"`
	}
	got.JSON(t, &doc)

	require.Equal(t, "/api/v1", doc.API.BasePath)
	require.NotEmpty(t, doc.API.Auth)
	require.NotEmpty(t, doc.CLI, "the CLI half comes from the cobra tree this binary runs")
	require.NotEmpty(t, doc.MCP, "the MCP half comes from the tool list this binary announces")
	require.NotEmpty(t, doc.Errors)
	require.Equal(t, "PANDO_SERVER", doc.Connect.ServerEnv)
	require.Equal(t, "PANDO_TOKEN", doc.Connect.TokenEnv)

	// Spot-check that it describes this server rather than a fixture: the
	// endpoint that served it is in it, with a summary and a group.
	var self bool
	for _, route := range doc.API.Routes {
		require.NotEmpty(t, route.Summary, "%s %s has no summary", route.Method, route.Path)
		require.NotEmpty(t, route.Group, "%s %s is in no group", route.Method, route.Path)
		if route.Method == http.MethodGet && route.Path == "/api/v1/reference" {
			self = true
		}
	}
	require.True(t, self, "the reference describes the endpoint that served it")

	// And it is readable without signing in at all: the document describes the
	// shape of the API and holds nothing about this installation, and a client
	// that must authenticate before it can read how to authenticate is a
	// product with one client. The same document is in the repository.
	anon := i.do(nil, http.MethodGet, "/reference", nil)
	require.Equal(t, http.StatusOK, anon.Code, anon.String())
	require.NotContains(t, anon.String(), "usr_", "the reference names no account")
	require.NotContains(t, anon.String(), "app_01", "the reference names no app")
}
