package httpapi_test

import (
	"bufio"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/httpapi"
)

// TestEveryEndpointInDesign04Exists walks the API design document and asserts
// each endpoint it lists is actually routed.
//
// This is the test that would have caught thirteen missing endpoints. Each
// phase met its own "done when" honestly, and no phase's condition said "every
// endpoint in design 04 exists" — so start, stop, restart, slots, volumes,
// groups, custom roles, the verb catalog, secret value reads, adapter
// configuration and user deletion all fell into the space *between* phases. The
// proof it mattered: the CLI shipped `pando slot set`, pointed at a route that
// returned 404, and nothing noticed.
//
// It reads the design rather than a list kept here, because a list kept here is
// a second thing to update and would drift the first time somebody added an
// endpoint to the design and not to this file. R-261 says the API is the
// product; this asserts the product matches its own description.
func TestEveryEndpointInDesign04Exists(t *testing.T) {
	routes := routedPaths(t)

	for _, ep := range designEndpoints(t) {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			require.True(t, routes[ep.method+" "+ep.path],
				"design 04 lists %s %s and no route serves it.\n"+
					"Either build it, or remove it from the design — a documented endpoint that "+
					"does not exist is worse than an undocumented one, because a client is written "+
					"against it.", ep.method, ep.path)
		})
	}
}

type endpoint struct{ method, path string }

// designEndpoints parses the endpoint lines out of design 04.
//
// Deliberately forgiving about spacing and trailing prose, and deliberately
// strict about the shape: a line has to start with a verb and a /api/v1 path to
// count, so tables and sentences mentioning a path are not mistaken for a
// declaration.
func designEndpoints(t *testing.T) []endpoint {
	t.Helper()

	f, err := os.Open("../../docs/design/04-api.md")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	line := regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE)\s+(/api/v1/\S*)`)

	var out []endpoint
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		m := line.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
		if m == nil {
			continue
		}

		path := normalizeDesignPath(m[2])
		if path == "" {
			continue
		}
		key := m[1] + " " + path
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, endpoint{method: m[1], path: path})
	}
	require.NoError(t, scanner.Err())
	require.NotEmpty(t, out, "found no endpoints in design 04 — the parser is broken, not the API")
	return out
}

// normalizeDesignPath turns a documented path into the chi pattern it should
// match, and returns "" for lines that are not really endpoints.
func normalizeDesignPath(raw string) string {
	// Query strings are illustration, not routing.
	if i := strings.Index(raw, "?"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSuffix(raw, "/api/v1")
	path := strings.TrimPrefix(raw, "/api/v1")
	if path == "" {
		return ""
	}

	// The design writes an action as `:verb` in places and `/verb` in others.
	// Routing uses a path segment, because a colon in a path is legal but
	// awkward in every client, and `GET /exec` has to be a plain path for the
	// websocket upgrade to work at all (RFC 6455).
	if i := strings.LastIndex(path, ":"); i >= 0 {
		path = path[:i] + "/" + path[i+1:]
	}

	// Parameter names differ between the document and the router — {id} versus
	// {appID} — and the name is not what is being asserted. Normalized to a
	// placeholder on both sides so the comparison is about shape.
	return regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(path, "{}")
}

// routedPaths walks the built router and returns every method+pattern it serves.
func routedPaths(t *testing.T) map[string]bool {
	t.Helper()

	handler := (&httpapi.Server{Logger: zap.NewNop(), DB: fakeDB{}}).Routes()
	router, ok := handler.(chi.Routes)
	require.True(t, ok, "the router must be walkable")

	out := map[string]bool{}
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimPrefix(route, "/api/v1")
		route = strings.TrimSuffix(route, "/")
		if route == "" {
			return nil
		}
		out[method+" "+regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(route, "{}")] = true
		return nil
	})
	require.NoError(t, err)
	return out
}
