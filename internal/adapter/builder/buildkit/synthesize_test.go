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
