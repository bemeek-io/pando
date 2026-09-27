package spec_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
)

// TestR167_WhatAPathMayBe asserts the shape of an app's path: lowercase
// segments of letters, digits and hyphens, at most four deep, never one of
// Pando's own, and stored in one normal form.
func TestR167_WhatAPathMayBe(t *testing.T) {
	for _, ok := range []string{"/notes", "/team/notes", "/a/b/c/d", "/n0tes-2"} {
		require.Nil(t, spec.CheckPathPrefix(ok), ok)
	}
	for bad, why := range map[string]string{
		"/":              "isn't a path",
		"notes":          "isn't a path",
		"/notes/":        "isn't a path",
		"/Notes":         "isn't a path",
		"/a/b/c/d/e":     "at most 4",
		"/-notes":        "isn't a path",
		"/no_underscore": "isn't a path",
		"/api":           "Pando's own",
		"/admin/x":       "Pando's own",
		"/.pando":        "isn't a path",
		"/readyz":        "Pando's own",
	} {
		err := spec.CheckPathPrefix(bad)
		require.NotNil(t, err, bad)
		require.Contains(t, err.Error(), why, bad)
	}

	require.Equal(t, "/team/notes", spec.NormalizePathPrefix(" Team/Notes/ "))
	require.Equal(t, "/", spec.NormalizePathPrefix(""))
}

// TestR167_ValidationUsesThePathRules asserts a whole spec is held to them.
func TestR167_ValidationUsesThePathRules(t *testing.T) {
	s := valid()
	s.Routing = spec.Routing{AdapterRef: "rte_loopback", Mode: spec.RoutingPath, PathPrefix: "/api/x"}
	err := spec.Validate(s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Pando's own")

	s.Routing.PathPrefix = "/team/notes"
	require.NoError(t, spec.Validate(s))
}

func TestAnAddressFollowsTheChosenPath(t *testing.T) {
	require.Equal(t, "/team/notes/", spec.Address("pando.test", "notes-x1", spec.Routing{Mode: spec.RoutingPath, PathPrefix: "/team/notes"}))
	require.Equal(t, "/notes-x1/", spec.Address("pando.test", "notes-x1", spec.Routing{Mode: spec.RoutingPath}),
		"an app with no path recorded is at its slug")
}
