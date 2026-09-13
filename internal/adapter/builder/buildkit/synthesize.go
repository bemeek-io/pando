package buildkit

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// generated is where a synthesized Dockerfile ended up.
type generated struct {
	// Dir holds the Dockerfile. Absolute.
	Dir string

	// Name is the Dockerfile's path relative to Dir.
	Name string

	// Cleanup removes anything temporary. Never removes the checkout: nixpacks
	// writes into it by necessity, and deleting it would take the source with
	// it.
	Cleanup func()
}

// synthesize writes a Dockerfile for a strategy that has none.
func synthesize(req api.BuildRequest, contextDir string) (generated, error) {
	switch req.Strategy {
	case spec.BuildStatic:
		dir, name, err := staticDockerfile(req, contextDir)
		if err != nil {
			return generated{}, err
		}
		return generated{Dir: dir, Name: name, Cleanup: func() { _ = os.RemoveAll(dir) }}, nil

	case spec.BuildBuildpack:
		name, err := buildpackDockerfile(req, contextDir)
		if err != nil {
			return generated{}, err
		}
		// Inside the checkout, which the caller owns and removes.
		return generated{
			Dir:     filepath.Dir(filepath.Join(contextDir, name)),
			Name:    filepath.Base(name),
			Cleanup: func() {},
		}, nil

	default:
		return generated{}, errs.Newf(errs.PlanCapabilityUnsupported,
			"This builder cannot build %q.", req.Strategy)
	}
}

// nixpacksBinary is the generator. R-095: wrap an existing implementation
// rather than reimplementing convention-matching.
//
// It is called only to *plan* — `nixpacks build --out` writes a Dockerfile and
// does not build anything, so nothing here needs a container runtime and R-112
// is not in play. The Dockerfile it writes is BuildKit's to build, and is
// readable afterwards: an app owner can see exactly what was decided instead of
// being told a buildpack happened.
const nixpacksBinary = "nixpacks"

// buildpackDockerfile asks nixpacks what this repository needs.
//
// The output goes into the checkout rather than a temporary directory, and that
// is nixpacks' own contract rather than a shortcut: the Dockerfile it writes
// does `COPY . /app/.` and `COPY .nixpacks/...`, so the build context has to be
// the source *with* the generated directory inside it. The checkout is a
// throwaway clone, so nothing anybody keeps is touched.
func buildpackDockerfile(_ api.BuildRequest, contextDir string) (string, error) {
	cmd := exec.Command(nixpacksBinary, "build", contextDir, "--out", contextDir)

	// No network. Generation reads the repository and decides; it does not
	// fetch, and a generator that can reach the internet while reading
	// untrusted source is a larger trust boundary than this needs.
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + contextDir}

	out, err := cmd.CombinedOutput()
	if err != nil {
		// nixpacks' own message, which names the language it could not place.
		// Replacing it with something generic would delete the only useful
		// detail (R-105).
		if errors.Is(err, exec.ErrNotFound) {
			return "", errs.New(errs.PlanCapabilityUnsupported,
				"This installation cannot work out how to build an app that carries no deployment instructions.").
				WithRemedy("Add a Dockerfile to the repository, or ask an administrator to install a build planner.")
		}
		return "", errs.Newf(errs.BuildFailed,
			"Pando could not work out how to build this app: %s", trim(out)).
			WithRemedy("Add a Dockerfile to the repository saying how it should be built.")
	}

	if _, statErr := os.Stat(filepath.Join(contextDir, ".nixpacks", "Dockerfile")); statErr != nil {
		return "", errs.Newf(errs.BuildFailed,
			"Pando could not work out how to build this app: %s", trim(out)).
			WithRemedy("Add a Dockerfile to the repository saying how it should be built.")
	}

	return filepath.Join(".nixpacks", "Dockerfile"), nil
}

// trim bounds a subprocess's output so one runaway generator cannot put a
// megabyte of text into an error envelope.
func trim(out []byte) string {
	const max = 2000
	s := strings.TrimSpace(string(out))
	if len(s) > max {
		return s[len(s)-max:]
	}
	return s
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
