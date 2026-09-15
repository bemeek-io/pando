package buildkit

import (
	"context"
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
		// Files the spec already carries are replayed, not regenerated. That is
		// what makes the build reproducible and what makes an edited plan
		// actually take effect — regenerating would overwrite it every time.
		if len(req.GeneratedFiles) > 0 {
			if err := writeInto(contextDir, req.GeneratedFiles); err != nil {
				return generated{}, err
			}
			name := req.Dockerfile
			if name == "" {
				name = filepath.Join(".nixpacks", "Dockerfile")
			}
			return generated{
				Dir:     filepath.Dir(filepath.Join(contextDir, name)),
				Name:    filepath.Base(name),
				Cleanup: func() {},
			}, nil
		}

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
	return planTwice(contextDir, readDeclaredBuild(contextDir), runNixpacks)
}

// planner generates a plan and returns where it was written, relative to the
// context directory.
type planner func(contextDir string, args []string) (string, error)

// planTwice asks the planner once, and again when the first answer is what the
// second question needs.
//
// A declaration that names the build replaces what nixpacks would have chosen,
// and one call settles it. A declaration that names an *ordering* does not:
// `//go:embed dist` beside a client that builds into that directory says the
// client comes first and says nothing about what it comes before. So: plan,
// read the chosen build command back out of the generated Dockerfile, plan
// again with the client build ahead of it.
//
// Two subprocesses rather than one, which is the honest cost of not
// reimplementing the thing R-095 says to wrap: the alternative to asking
// nixpacks what it would have chosen is being nixpacks.
//
// The planner is a parameter so this can be tested without one. The composition
// is where the bug was last time — `cd web` outliving the client build — and
// subprocess plumbing is a poor place to hide logic worth checking.
func planTwice(contextDir string, declared declaredBuild, plan planner) (string, error) {
	name, err := plan(contextDir, declared.nixpacksArgs(""))
	if err != nil {
		return "", err
	}
	if !declared.needsSecondPass() {
		return name, nil
	}

	body, readErr := os.ReadFile(filepath.Join(contextDir, name))
	if readErr != nil {
		return "", errs.Wrap(errs.BuildFailed, "Could not read the build plan.", readErr)
	}

	// No build step to come before means there is nothing to wrap, and the
	// first plan stands. A repository whose build nixpacks could not work out
	// is one this has nothing to add to.
	chosen := planBuildCommand(string(body))
	if chosen == "" {
		return name, nil
	}

	return plan(contextDir, declared.nixpacksArgs(chosen))
}

// runNixpacks generates a plan, and returns where it was written.
func runNixpacks(contextDir string, extra []string) (string, error) {
	args := append([]string{"build", contextDir, "--out", contextDir}, extra...)

	// G204: exec.Command takes an argv, so nothing here reaches a shell on this
	// host. contextDir is a checkout this process made, and every command in
	// `extra` was read out of the repository and passed safeCommand.
	//
	// Those commands do become the built image's build steps and CMD, which
	// nixpacks runs under `bash -l -c` — so the repository chooses what its own
	// build and container run. That is the same authority a Dockerfile already
	// has, and the container is the boundary either way (R-112, R-114).
	cmd := exec.Command(nixpacksBinary, args...) //nolint:gosec

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

// planBuildCommand pulls the build step out of a generated plan.
//
// A nixpacks plan runs its phases as RUN lines in order: the environment first
// (`nix-env -if ...`), then install, then build. The build is the last RUN in
// the builder stage that is not one of nixpacks' own bookkeeping lines — the
// `RUN true` it emits for an empty phase, and the nix-env line.
func planBuildCommand(dockerfile string) string {
	var last string
	for _, line := range strings.Split(dockerfile, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "RUN ") {
			// A second FROM starts the runtime stage, and nothing after it
			// builds anything.
			if strings.HasPrefix(trimmed, "FROM ") && last != "" {
				break
			}
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(trimmed, "RUN "))
		for strings.HasPrefix(command, "--") {
			_, rest, found := strings.Cut(command, " ")
			if !found {
				command = ""
				break
			}
			command = strings.TrimSpace(rest)
		}
		if command == "" || command == "true" || strings.HasPrefix(command, "nix-env ") {
			continue
		}
		last = command
	}
	return last
}

// writeInto materializes generated build inputs in the checkout.
//
// Into the checkout because they have to be in the build context: the plan's
// Dockerfile references them with COPY. The checkout is a throwaway clone, so
// nothing anybody keeps is written to.
//
// Every path is re-checked here even though spec validation already rejected
// one that climbs out. A spec is exportable and importable, and an imported one
// is untrusted input (design 01 §5) — this is the last point before content
// from it reaches a filesystem, and the cost of checking twice is nothing.
func writeInto(contextDir string, files map[string]string) error {
	for name, content := range files {
		clean := path.Clean("/" + filepath.ToSlash(name))
		rel := strings.TrimPrefix(clean, "/")
		if rel == "" || rel == "." || rel != filepath.ToSlash(name) {
			return errs.Newf(errs.ValidInvalid,
				"A generated build file is written to %q, which is not a path inside the app's source.", name)
		}

		full := filepath.Join(contextDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return errs.Wrap(errs.BuildFailed, "Could not prepare the build.", err)
		}
		// G306: 0644 is deliberate. This is a generated build input inside the
		// build context, and the rootless builder that reads it runs as a
		// different user. It holds no secret — a secret reaches a build as a
		// build secret, never as a file in the context.
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil { //nolint:gosec
			return errs.Wrap(errs.BuildFailed, "Could not prepare the build.", err)
		}
	}
	return nil
}

// Plan makes a build plan without building anything, for detection.
//
// The same generator the build path uses, so what somebody reviews is what runs
// (R-102). It writes into a copy of nothing: the source view's directory is the
// detection checkout, which is discarded when detection finishes.
func (a *Adapter) Plan(_ context.Context, src api.SourceView) (map[string]string, string, *api.PlanDeclaration, error) {
	dir, ok := src.(interface{ Root() string })
	if !ok {
		return nil, "", nil, errs.New(errs.BuildFailed, "Planning needs the source on disk.")
	}
	root := dir.Root()

	name, err := buildpackDockerfile(api.BuildRequest{}, root)
	if err != nil {
		return nil, "", nil, err
	}

	// Everything the generator wrote, read back as content. The spec carries
	// the plan itself rather than a pointer to a directory that will not exist
	// next time.
	files := map[string]string{}
	planDir := filepath.Join(root, ".nixpacks")
	entries, err := os.ReadDir(planDir)
	if err != nil {
		return nil, "", nil, errs.Wrap(errs.BuildFailed, "Could not read the build plan.", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(planDir, e.Name()))
		if readErr != nil {
			return nil, "", nil, errs.Wrap(errs.BuildFailed, "Could not read the build plan.", readErr)
		}
		files[path.Join(".nixpacks", e.Name())] = string(body)
	}
	return files, name, readDeclaredBuild(root).asPlanDeclaration(), nil
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

	// G306: a generated Dockerfile in the build context, read by the rootless
	// builder running as a different user. Not a secret — a secret reaches a
	// build as a build secret, never as a file in the context.
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o644); err != nil { //nolint:gosec
		_ = os.RemoveAll(dir)
		return "", "", errs.Wrap(errs.BuildFailed, "Could not prepare the build.", err)
	}
	return dir, "Dockerfile", nil
}
