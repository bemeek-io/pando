package buildkit

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// Strategies that have no Dockerfile in the repository get one written for
// them.
//
// The Dockerfile is the builder's vocabulary, not Pando's (R-251). Core says
// "this app is a directory of files to serve"; what image serves them and how
// is decided here, and a different builder is free to decide differently.
//
// BuildKit already takes the context and the Dockerfile as two separate
// filesystems, so this needs no copy of the source: the context stays the
// checkout and only the generated Dockerfile lives in a temporary directory.

// staticServerImage serves a built static site.
//
// [P]. nginx because it is the smallest well-understood thing that serves a
// directory over HTTP, alpine for the same reason the provisioned services use
// alpine, and pinned to a minor version so a rebuild a year from now produces
// the same server. Listed in design 03 §8 with the other images Pando supplies.
const staticServerImage = "nginx:1.27-alpine"

// staticConfig makes nginx behave the way a single-page app needs.
//
// try_files falling back to index.html is what makes client-side routing work:
// without it, reloading on /admin/apps/123 asks nginx for a file that does not
// exist and gets a 404 — the same trap Pando's own console needed the server to
// handle. A static site with a router is the common case, and a site without
// one is unaffected by the fallback.
const staticConfig = `server {
  listen 80;
  root /usr/share/nginx/html;
  location / { try_files $uri $uri/ /index.html; }
}
`

// synthesize writes a Dockerfile for a strategy that has none, and returns the
// directory holding it. The caller removes that directory.
func synthesize(req api.BuildRequest, contextDir string) (dir string, dockerfile string, err error) {
	switch req.Strategy {
	case spec.BuildStatic:
		return staticDockerfile(req, contextDir)
	default:
		return "", "", errs.Newf(errs.PlanCapabilityUnsupported,
			"This builder cannot build %q.", req.Strategy)
	}
}

func staticDockerfile(req api.BuildRequest, contextDir string) (string, string, error) {
	// The directory to serve, relative to the build context and constrained to
	// it. A StaticDir of "../../etc" would otherwise copy whatever the build
	// context's parent holds into a public web root.
	serve := path.Clean("/" + filepath.ToSlash(req.StaticDir))
	serve = strings.TrimPrefix(serve, "/")
	if serve == "" || serve == "." {
		serve = "."
	}

	if info, statErr := os.Stat(filepath.Join(contextDir, filepath.FromSlash(serve))); statErr != nil || !info.IsDir() {
		return "", "", errs.Newf(errs.BuildFailed,
			"This app is set to serve %q, and there is no such directory in the repository.",
			req.StaticDir).
			WithRemedy("Check the directory name in the app's configuration, or point it at the folder holding index.html.")
	}

	dir, err := os.MkdirTemp("", "pando-static-*")
	if err != nil {
		return "", "", errs.Wrap(errs.BuildFailed, "Could not prepare the build.", err)
	}

	content := fmt.Sprintf(`FROM %s
COPY %s/ /usr/share/nginx/html/
RUN printf '%%s' %q > /etc/nginx/conf.d/default.conf
`, staticServerImage, serve, staticConfig)

	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", errs.Wrap(errs.BuildFailed, "Could not prepare the build.", err)
	}
	return dir, "Dockerfile", nil
}
