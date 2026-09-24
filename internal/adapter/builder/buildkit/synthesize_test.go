package buildkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

func repo(t *testing.T, dirs ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}
	return root
}

// TestR110_AStaticSiteNeedsNoDockerfileInTheRepository asserts the strategy
// works without the app carrying build instructions.
//
// A static site is the case R-103 is about — somebody who never thought about
// deployment. Requiring them to write a Dockerfile to serve a folder is the
// per-app setup burden R-002 exists to remove.
func TestR110_AStaticSiteNeedsNoDockerfileInTheRepository(t *testing.T) {
	root := repo(t, "dist")

	gen, err := synthesize(api.BuildRequest{
		Strategy: spec.BuildStatic, StaticDir: "dist",
	}, root)
	require.NoError(t, err)
	defer gen.Cleanup()

	require.Equal(t, "Dockerfile", gen.Name)
	body, err := os.ReadFile(filepath.Join(gen.Dir, gen.Name))
	require.NoError(t, err)

	content := string(body)
	require.Contains(t, content, staticServerImage)
	require.Contains(t, content, "COPY dist/ /usr/share/nginx/html/")

	// Client-side routing works. Without the fallback, reloading on a path the
	// app routes itself asks the server for a file that does not exist.
	require.Contains(t, content, "try_files")

	// Written outside the checkout: the app's own source is never modified by
	// building it.
	require.False(t, strings.HasPrefix(gen.Dir, root), "the Dockerfile is not written into the app's source")
}

// The server configuration reaches the image exactly as written.
//
// It used to go through Go's %q inside a single-quoted printf, so the shell
// expanded $uri to nothing and the newlines arrived as a literal `\n`. nginx
// refused the file and every static site exited on start (issue #55). This runs
// the generated line through a real shell, the way the build does.
func TestR110_AStaticSitesServerConfigIsWrittenAsNginxReadsIt(t *testing.T) {
	root := repo(t, "dist")
	gen, err := synthesize(api.BuildRequest{Strategy: spec.BuildStatic, StaticDir: "dist"}, root)
	require.NoError(t, err)
	defer gen.Cleanup()

	body, err := os.ReadFile(filepath.Join(gen.Dir, gen.Name))
	require.NoError(t, err)

	var run string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "RUN ") {
			run = strings.TrimPrefix(line, "RUN ")
		}
	}
	require.NotEmpty(t, run)

	out := filepath.Join(t.TempDir(), "default.conf")
	run = strings.Replace(run, "/etc/nginx/conf.d/default.conf", out, 1)
	require.NoError(t, exec.Command("sh", "-c", run).Run())

	written, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, staticConfig, string(written))
}

// An answered start command reaches the build plan. It used to stop at the
// workload, and nixpacks failed with "No start command could be found" on the
// apps whose owners had just typed one (issue #55).
func TestR104_AnAnsweredStartCommandReachesTheBuildPlan(t *testing.T) {
	root := repo(t)
	args := declaredFor(api.BuildRequest{StartCommand: "gunicorn app:app"}, root).nixpacksArgs("")
	require.Equal(t, []string{"--start-cmd", "gunicorn app:app"}, args)

	require.Empty(t, declaredFor(api.BuildRequest{}, root).nixpacksArgs(""),
		"nothing is invented when nobody said how the app starts")
}

// A Node app that names no version gets a supported one, and one that names a
// version keeps it. nixpacks defaults to Node 18, which current frameworks
// refuse with EBADENGINE (issue #55).
func TestR095_ANodeAppGetsASupportedNodeUnlessItNamesOne(t *testing.T) {
	write := func(t *testing.T, files map[string]string) string {
		root := t.TempDir()
		for name, body := range files {
			require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
		}
		return root
	}

	silent := write(t, map[string]string{"package.json": `{"name":"a"}`})
	require.Equal(t, []string{"--env", "NIXPACKS_NODE_VERSION=" + defaultNodeVersion}, toolchainDefaults(silent))

	engines := write(t, map[string]string{"package.json": `{"engines":{"node":">=20"}}`})
	require.Empty(t, toolchainDefaults(engines), "engines.node is the author's answer")

	nvmrc := write(t, map[string]string{"package.json": `{}`, ".nvmrc": "20\n"})
	require.Empty(t, toolchainDefaults(nvmrc), "nixpacks reads .nvmrc itself")

	nodeVersion := write(t, map[string]string{"package.json": `{}`, ".node-version": "v20.11.1\n"})
	require.Equal(t, []string{"--env", "NIXPACKS_NODE_VERSION=20.11.1"}, toolchainDefaults(nodeVersion))

	require.Empty(t, toolchainDefaults(write(t, map[string]string{"go.mod": "module x"})),
		"not a Node app")
}

func TestPrintfFormatSurvivesEveryShellSpecialCharacter(t *testing.T) {
	text := "a 'quoted' $var \\n 100% \"done\"\nnext line\n"
	out, err := exec.Command("sh", "-c", "printf "+printfFormat(text)).Output()
	require.NoError(t, err)
	require.Equal(t, text, string(out))
}

// The repository root is the default when no directory is named.
func TestAStaticSiteWithNoDirectoryServesTheRoot(t *testing.T) {
	root := repo(t)

	gen, err := synthesize(api.BuildRequest{Strategy: spec.BuildStatic}, root)
	require.NoError(t, err)
	defer gen.Cleanup()

	body, err := os.ReadFile(filepath.Join(gen.Dir, "Dockerfile"))
	require.NoError(t, err)
	require.Contains(t, string(body), "COPY ./ /usr/share/nginx/html/")
}

// A directory that is not there fails with the name in it, before a build runs.
func TestAMissingStaticDirectoryIsRefusedByName(t *testing.T) {
	_, err := synthesize(api.BuildRequest{
		Strategy: spec.BuildStatic, StaticDir: "public",
	}, repo(t, "dist"))

	require.Error(t, err)
	require.Contains(t, err.Error(), "public", "R-105: the message names what is missing")
}

// A static directory cannot escape the build context.
//
// Without the constraint, "../.." would copy whatever sits beside the checkout
// into a directory served to anyone who can reach the app.
func TestAStaticDirectoryCannotEscapeTheContext(t *testing.T) {
	root := repo(t, "dist")

	gen, err := synthesize(api.BuildRequest{
		Strategy: spec.BuildStatic, StaticDir: "../../etc",
	}, root)
	if err == nil {
		defer gen.Cleanup()
		body, readErr := os.ReadFile(filepath.Join(gen.Dir, "Dockerfile"))
		require.NoError(t, readErr)
		require.NotContains(t, string(body), "..", "a path outside the context never reaches the COPY")
	}
}

// A strategy this builder does not implement is refused here rather than
// producing an empty Dockerfile.
func TestAnUnknownStrategyIsRefused(t *testing.T) {
	_, err := synthesize(api.BuildRequest{Strategy: spec.BuildStrategy("nonsense")}, repo(t))
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonsense")
}

// TestR095_ABuildpackPlanComesFromNixpacks asserts R-095: wrap an existing
// implementation rather than reimplementing convention-matching.
//
// Skipped where nixpacks is not installed, which is every developer machine
// that has not built the image — the binary ships in the Pando image, not in
// the repository.
func TestR095_ABuildpackPlanComesFromNixpacks(t *testing.T) {
	if _, err := exec.LookPath(nixpacksBinary); err != nil {
		t.Skip("nixpacks is not on PATH; it ships in the Pando image")
	}

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"),
		[]byte(`{"name":"s","version":"1.0.0","scripts":{"start":"node index.js"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "index.js"), []byte("console.log(1)\n"), 0o644))

	gen, err := synthesize(api.BuildRequest{Strategy: spec.BuildBuildpack}, root)
	require.NoError(t, err)
	defer gen.Cleanup()

	// The generated Dockerfile lives inside the checkout, because the one
	// nixpacks writes does `COPY . /app/.` and `COPY .nixpacks/...` — the build
	// context has to be the source with the generated directory inside it.
	require.Equal(t, "Dockerfile", gen.Name)
	require.Equal(t, filepath.Join(root, ".nixpacks"), gen.Dir)

	body, err := os.ReadFile(filepath.Join(gen.Dir, gen.Name))
	require.NoError(t, err)
	require.Contains(t, string(body), "FROM ", "it is a Dockerfile BuildKit can build")
}

// A repository nixpacks cannot place fails with its own reason, before a build
// is attempted.
func TestABuildpackPlanThatFailsSaysWhy(t *testing.T) {
	if _, err := exec.LookPath(nixpacksBinary); err != nil {
		t.Skip("nixpacks is not on PATH; it ships in the Pando image")
	}

	_, err := synthesize(api.BuildRequest{Strategy: spec.BuildBuildpack}, t.TempDir())
	require.Error(t, err)
	require.NotEmpty(t, errs.As(err).Remedy, "R-105 promises a way forward")
}

// TestR020_AnEditedPlanIsTheOneThatRuns asserts the point of storing the plan.
//
// A plan regenerated at every build is not a record of how the app runs: the
// same commit builds differently once the generator is upgraded, and an edit
// made in the console is overwritten before it reaches the build. Files the
// spec carries are replayed, not regenerated.
func TestR020_AnEditedPlanIsTheOneThatRuns(t *testing.T) {
	root := t.TempDir()
	edited := "FROM alpine:3.21\nCOPY . /app\nCMD [\"/app/out\"]\n"

	gen, err := synthesize(api.BuildRequest{
		Strategy:   spec.BuildBuildpack,
		Dockerfile: filepath.Join(".nixpacks", "Dockerfile"),
		GeneratedFiles: map[string]string{
			filepath.Join(".nixpacks", "Dockerfile"): edited,
			filepath.Join(".nixpacks", "build.sh"):   "#!/bin/sh\n",
		},
	}, root)
	require.NoError(t, err)
	defer gen.Cleanup()

	body, err := os.ReadFile(filepath.Join(gen.Dir, gen.Name))
	require.NoError(t, err)
	require.Equal(t, edited, string(body), "the stored plan is what gets built, not a fresh one")

	// The files it references are materialized too, or the COPY fails.
	_, err = os.Stat(filepath.Join(root, ".nixpacks", "build.sh"))
	require.NoError(t, err)
}

// A generated path that climbs out of the build context is refused.
//
// A spec is exportable and importable, and an imported one is untrusted input
// (design 01 §5). This is the last point before its content reaches a
// filesystem.
func TestAGeneratedFileCannotEscapeTheCheckout(t *testing.T) {
	root := t.TempDir()

	_, err := synthesize(api.BuildRequest{
		Strategy:       spec.BuildBuildpack,
		GeneratedFiles: map[string]string{"../../../tmp/pwned": "x"},
	}, root)

	require.Error(t, err)
	require.NoFileExists(t, filepath.Join(filepath.Dir(root), "pwned"))
}

// TestR020_APlansAssetsSurviveIntoTheBuild asserts that a plan is stored whole.
//
// A nixpacks plan that serves static files writes its web server's
// configuration into .nixpacks/assets and a Dockerfile that says
// COPY .nixpacks/assets /assets/. Reading back only the top level of the plan
// directory dropped it, and the build failed on that COPY with
// "/.nixpacks/assets: not found" — Pando's own plan, refused by Pando.
func TestR020_APlansAssetsSurviveIntoTheBuild(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, ".nixpacks")
	require.NoError(t, os.MkdirAll(filepath.Join(plan, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(plan, "Dockerfile"),
		[]byte("FROM alpine:3.21\nCOPY .nixpacks/assets /assets/\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(plan, "assets", "Caddyfile"),
		[]byte(":80\nroot * /app/dist\n"), 0o644))

	files, err := collectPlan(root)
	require.NoError(t, err)
	require.Equal(t, ":80\nroot * /app/dist\n", files[".nixpacks/assets/Caddyfile"],
		"every path the generated Dockerfile copies is carried, whatever directory it sits in")
	require.Contains(t, files, ".nixpacks/Dockerfile")

	// And back out again, into the checkout the build reads.
	built := t.TempDir()
	require.NoError(t, writeInto(built, files))
	_, err = os.Stat(filepath.Join(built, ".nixpacks", "assets", "Caddyfile"))
	require.NoError(t, err)
}

// TestR020_APlansBuildArgumentsAreUsed asserts that a stored plan carries the
// values its Dockerfile's ARG lines need.
//
// nixpacks records them beside the Dockerfile, as the docker build command it
// would have run. Ignoring them left every ARG empty: a single-page app's web
// root resolved to the repository instead of its dist directory, so the site
// served the source index.html and came up blank — with a 200, and nothing in
// the logs to say why.
func TestR020_APlansBuildArgumentsAreUsed(t *testing.T) {
	args := planArgs(map[string]string{
		".nixpacks/build.sh": "docker build /tmp/src -f /tmp/src/.nixpacks/Dockerfile -t x " +
			"--build-arg CI=true --build-arg NIXPACKS_SPA_OUTPUT_DIR=dist --build-arg NODE_ENV=production",
	})
	require.Equal(t, "dist", args["NIXPACKS_SPA_OUTPUT_DIR"])
	require.Equal(t, "true", args["CI"])
	require.Equal(t, "production", args["NODE_ENV"])
}

// A plan with no build script asks for nothing, and a flag written as one
// token is read the same way.
func TestPlanArgumentsAreReadWhateverTheSpelling(t *testing.T) {
	require.Empty(t, planArgs(map[string]string{".nixpacks/Dockerfile": "FROM alpine:3.21\n"}))
	require.Equal(t, map[string]string{"K": "v"},
		planArgs(map[string]string{".nixpacks/build.sh": `docker build . --build-arg=K="v"`}))
}

// A buildpack build is planned by whichever of Pando's own planners recognizes
// the repository, in a fixed order, before nixpacks is asked. Each plan is
// written into .nixpacks/ in the checkout, where detection collects it and the
// build replays it (R-020).
func TestABuildpackRepositoryIsPlannedByTheFirstPlannerThatKnowsIt(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		req   api.BuildRequest
		want  string
	}{
		"a site that builds to static files": {
			files: map[string]string{"package.json": `{"scripts":{"build":"astro build"},"dependencies":{"astro":"^5"}}`},
			want:  "COPY --from=build /app/dist/ /usr/share/nginx/html/",
		},
		"a site somebody chose to build, in the directory it was found": {
			files: map[string]string{"package.json": `{"scripts":{"build":"node build.js"}}`, "yarn.lock": ""},
			req:   api.BuildRequest{StaticDir: "site/"},
			want:  "COPY --from=build /app/site/ /usr/share/nginx/html/",
		},
		"a Node server": {
			files: map[string]string{"package.json": `{"scripts":{"start":"node server.js"}}`},
			want:  "# A Node server, built and run on Node 22.",
		},
		"a Go program": {
			files: map[string]string{"go.mod": "module x\n\ngo 1.24\n", "main.go": "package main\n"},
			want:  "FROM golang:1.24 AS build",
		},
		"a .NET project": {
			files: map[string]string{"Api.csproj": "<TargetFramework>net9.0</TargetFramework>"},
			want:  "FROM mcr.microsoft.com/dotnet/sdk:9.0 AS build",
		},
		"a JVM project": {
			files: map[string]string{"pom.xml": "<project/>"},
			want:  "# A Maven project, built on JDK 21.",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := writeFiles(t, tc.files)
			name, err := buildpackDockerfile(tc.req, root)
			require.NoError(t, err)
			require.Equal(t, filepath.Join(".nixpacks", "Dockerfile"), name)
			body, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			require.Contains(t, string(body), tc.want)
		})
	}
}

// A run-build static directory is only planned as a site when there is a
// package.json to build it with, and never when the directory would climb out
// of the checkout. Otherwise the repository goes on to the planners after it.
func TestARunBuildStaticDirectoryNeedsSomethingToBuildIt(t *testing.T) {
	escaping := writeFiles(t, map[string]string{"package.json": `{"scripts":{"start":"node server.js"}}`})
	name, err := buildpackDockerfile(api.BuildRequest{StaticDir: "../outside"}, escaping)
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(escaping, name))
	require.NoError(t, err)
	require.NotContains(t, string(body), "nginx", "an escaping directory is not served")
	require.Contains(t, string(body), "A Node server")

	noPackage := writeFiles(t, map[string]string{"pom.xml": "<project/>"})
	name, err = buildpackDockerfile(api.BuildRequest{StaticDir: "dist"}, noPackage)
	require.NoError(t, err)
	body, err = os.ReadFile(filepath.Join(noPackage, name))
	require.NoError(t, err)
	require.Contains(t, string(body), "A Maven project", "without a package.json there is nothing to run the build")
}

// An answered start command means the app is run, not served as files, and a
// Go program with one is left to nixpacks rather than compiled by Pando's plan,
// because the command runs in nixpacks' image, which has Go on its PATH.
func TestAnAnsweredStartCommandSkipsTheStaticAndGoPlanners(t *testing.T) {
	site := writeFiles(t, map[string]string{"package.json": `{"scripts":{"build":"astro build"},"dependencies":{"astro":"^5"}}`})
	name, err := buildpackDockerfile(api.BuildRequest{StartCommand: "node serve.js"}, site)
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(site, name))
	require.NoError(t, err)
	require.Contains(t, string(body), `CMD ["node serve.js"]`, "the answered command is the Node server's start")

	if _, lookErr := exec.LookPath(nixpacksBinary); lookErr == nil {
		t.Skip("nixpacks is on PATH, so the Go case would be planned by it")
	}
	program := writeFiles(t, map[string]string{"go.mod": "module x\ngo 1.24\n", "main.go": "package main\n"})
	_, err = buildpackDockerfile(api.BuildRequest{StartCommand: "go run ."}, program)
	require.Equal(t, errs.PlanCapabilityUnsupported, errs.CodeOf(err),
		"without nixpacks, a Go program with a start command has no planner")
	_, statErr := os.Stat(filepath.Join(program, ".nixpacks", "Dockerfile"))
	require.True(t, os.IsNotExist(statErr), "Pando's Go plan was not written")
}

// When nixpacks cannot plan the repository, a start command that runs a
// program committed in it is planned as that program, run as it is (issue
// #55). Without nixpacks installed the generator fails at once, which reaches
// the same fallback.
func TestR094_ACommittedProgramIsPlannedWhenNixpacksHasNothingToSay(t *testing.T) {
	if _, err := exec.LookPath(nixpacksBinary); err == nil {
		t.Skip("nixpacks is on PATH and may plan this repository itself")
	}
	root := writeFiles(t, map[string]string{"server": "\x7fELF", "Procfile": "web: ./server --verbose\n"})
	name, err := buildpackDockerfile(api.BuildRequest{}, root)
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(root, name))
	require.NoError(t, err)
	require.Contains(t, string(body), "FROM "+committedProgramImage)
	require.Contains(t, string(body), `CMD ["./server --verbose"]`)

	// An answered start command outranks the Procfile and is what runs.
	answered := writeFiles(t, map[string]string{"server": "\x7fELF"})
	name, err = buildpackDockerfile(api.BuildRequest{StartCommand: "./server --port $PORT"}, answered)
	require.NoError(t, err)
	body, err = os.ReadFile(filepath.Join(answered, name))
	require.NoError(t, err)
	require.Contains(t, string(body), `CMD ["./server --port $PORT"]`)
}

// A repository nothing recognizes fails with the generator's reason. Without
// nixpacks on the host, that reason is that this installation has no planner,
// with a remedy that names what to do instead (R-105). The generator is still
// prepared for first: a Ruby project gets the .ruby-version nixpacks requires.
func TestAnUnrecognizedRepositoryFailsWithTheGeneratorsReason(t *testing.T) {
	root := writeFiles(t, map[string]string{"Gemfile": "ruby '3.2.4'\n"})
	_, err := buildpackDockerfile(api.BuildRequest{}, root)
	require.Error(t, err)
	require.NotEmpty(t, errs.As(err).Remedy, "R-105 promises a way forward")
	if _, lookErr := exec.LookPath(nixpacksBinary); lookErr != nil {
		require.Equal(t, errs.PlanCapabilityUnsupported, errs.CodeOf(err))
	}

	body, readErr := os.ReadFile(filepath.Join(root, ".ruby-version"))
	require.NoError(t, readErr)
	require.Equal(t, "3.2.4\n", string(body))
}

// Plan returns every file the planner wrote, so the spec carries the plan
// itself (R-020), and names the Dockerfile among them.
func TestR020_PlanReturnsThePlanItWrote(t *testing.T) {
	root := writeFiles(t, map[string]string{"pom.xml": "<project/>"})
	files, name, _, err := New().Plan(t.Context(), view{root: root})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(".nixpacks", "Dockerfile"), name)
	require.Contains(t, files[".nixpacks/Dockerfile"], "A Maven project")
}
