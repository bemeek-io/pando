package spec

import (
	"regexp"
	"strings"

	"github.com/bemeek-io/pando/internal/errs"
)

// ReservedPaths are the first path segments Pando answers itself, on every
// hostname that is not an app's own. An app on a path cannot take one: the
// router serves these before the proxy sees the request, so the app would
// never receive them — and a prefix that shadowed /login or /api would be a
// page on Pando's own origin that looks like Pando.
//
// The router's own routes are checked against this list by a test
// (httpapi TestR167_EveryPandoPathIsReserved), so adding a top-level route
// without reserving it fails the build.
var ReservedPaths = []string{
	"api", "admin", "assets", "login", "index.html", "healthz", "readyz", ".well-known", ".pando",
}

// pathSegment is one segment of an app's path: lowercase letters, digits and
// hyphens, as a slug is, so a prefix reads the same way every slug does and
// never needs escaping in a URL, a Traefik rule or a regular expression.
var pathSegment = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// maxPathSegments bounds how deep an app's path goes. Four is room for a team
// and a tool within it; more is a path nobody types.
const maxPathSegments = 4

// NormalizePathPrefix is a path as Pando stores it: one leading slash, no
// trailing one, lowercase.
func NormalizePathPrefix(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	p = "/" + strings.Trim(p, "/")
	return p
}

// CheckPathPrefix refuses a path an app cannot be served under, saying why.
func CheckPathPrefix(p string) *errs.Error {
	if p != NormalizePathPrefix(p) || p == "/" {
		return errs.Newf(errs.ValidInvalid, "%q isn't a path an app can be served under.", p).
			WithRemedy("Use a path like /notes or /team/notes: a slash before each part, lowercase, and no slash at the end.")
	}
	segments := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(segments) > maxPathSegments {
		return errs.Newf(errs.ValidInvalid, "%s has %d parts, and an app's path can have at most %d.", p, len(segments), maxPathSegments)
	}
	for _, s := range segments {
		if !pathSegment.MatchString(s) {
			return errs.Newf(errs.ValidInvalid, "%q isn't a path an app can be served under.", p).
				WithRemedy("Each part of the path is lowercase letters, digits and hyphens, such as /team/notes.")
		}
	}
	for _, r := range ReservedPaths {
		if segments[0] == r {
			return errs.Newf(errs.ValidInvalid, "%s is Pando's own, so an app can't be served under it.", "/"+r).
				WithRemedy("Choose a path that starts with something else, such as /notes.")
		}
	}
	return nil
}
