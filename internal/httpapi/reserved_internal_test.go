package httpapi

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/spec"
)

// TestR167_EveryPandoPathIsReserved asserts that no app can be given a path
// Pando answers itself. The router serves its own routes before the proxy sees
// a request, so an app at /api/x would never receive a request — and worse, an
// app that could claim /login would be a page on Pando's origin that looks
// like Pando's.
//
// Adding a top-level route without adding its first segment to
// spec.ReservedPaths fails here.
func TestR167_EveryPandoPathIsReserved(t *testing.T) {
	first := func(route string) string {
		return strings.SplitN(strings.TrimPrefix(route, "/"), "/", 2)[0]
	}
	check := func(route string) {
		seg := first(route)
		if seg == "" || seg == "*" {
			return // the root is the console's, and no path is ever "/"
		}
		require.True(t, slices.Contains(spec.ReservedPaths, seg),
			"%s is one of Pando's own routes, so %q must be in spec.ReservedPaths", route, seg)
	}

	handler := (&Server{Logger: zap.NewNop()}).Routes()
	routes, ok := handler.(chi.Routes)
	require.True(t, ok)
	walked := 0
	require.NoError(t, chi.Walk(routes, func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		walked++
		check(route)
		return nil
	}))
	require.Positive(t, walked)

	// Mounted only when there is a console, so not in the walk above.
	for _, route := range consoleRoutes {
		check(route)
	}
	check(ReservedPrefix)

	require.Error(t, spec.CheckPathPrefix("/api"), "and the check refuses them")
}
