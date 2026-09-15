package buildkit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- workflows --------------------------------------------------------------

// R-094 tier 3 named CI workflows first, and this is why: a Makefile target is
// what somebody wrote down, while a workflow step is what runs on every push.
func TestR094_AWorkflowsBuildJobIsTheBuild(t *testing.T) {
	root := withFiles(t, map[string]string{
		"go.mod": "module example.com/app\n",
		".github/workflows/ci.yml": "name: CI\non: [push]\njobs:\n" +
			"  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: go test ./...\n" +
			"  build:\n    runs-on: ubuntu-latest\n    steps:\n" +
			"      - uses: actions/checkout@v4\n" +
			"      - uses: actions/setup-go@v5\n" +
			"      - run: npm ci --prefix web\n" +
			"      - run: go build -o app ./cmd/server\n",
	})

	d := readDeclaredBuild(root)
	require.Equal(t, rankWorkflow, d.Rank)
	require.Equal(t, ".github/workflows/ci.yml", d.Source)
	require.Equal(t, "npm ci --prefix web && go build -o app ./cmd/server", d.Build,
		"the build job's commands, in order, and not the test job's")
}

// A `run: |` block is several commands in one step.
func TestAWorkflowBlockScalarIsReadAsItsLines(t *testing.T) {
	root := withFiles(t, map[string]string{
		".github/workflows/release.yml": "jobs:\n  build:\n    steps:\n" +
			"      - run: |\n" +
			"          npm ci --prefix web\n" +
			"          npm run build --prefix web\n" +
			"      - run: go build -o app .\n",
	})
	require.Equal(t,
		"npm ci --prefix web && npm run build --prefix web && go build -o app .",
		readDeclaredBuild(root).Build)
}

// The ways a workflow is a program rather than a list of commands, each of
// which is declined rather than guessed at.
func TestAWorkflowThatCannotBeReadPlainlyIsDeclined(t *testing.T) {
	for name, workflow := range map[string]string{
		"a matrix builds several things": "jobs:\n  build:\n    strategy:\n      matrix:\n" +
			"        go: [1.24, 1.25]\n    steps:\n      - run: go build .\n",
		"a reusable workflow is another file": "jobs:\n  build:\n    uses: ./.github/workflows/shared.yml\n",
		"no job called build":                 "jobs:\n  test:\n    steps:\n      - run: go test ./...\n",
		"the build job only calls actions": "jobs:\n  build:\n    steps:\n" +
			"      - uses: actions/checkout@v4\n      - uses: docker/build-push-action@v5\n",
		"a command needing a shell variable": "jobs:\n  build:\n    steps:\n" +
			"      - run: go build -ldflags \"-X main.v=$VERSION\" .\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := withFiles(t, map[string]string{".github/workflows/ci.yml": workflow})
			require.NotEqual(t, rankWorkflow, readDeclaredBuild(root).Rank)
		})
	}
}

// Reporting and bookkeeping steps are in a build job and are not the build.
func TestCiBookkeepingIsNotPartOfTheBuild(t *testing.T) {
	root := withFiles(t, map[string]string{
		".github/workflows/ci.yml": "jobs:\n  build:\n    steps:\n" +
			"      - run: echo building\n" +
			"      - run: go build -o app .\n" +
			"      - run: codecov -f coverage.out\n",
	})
	require.Equal(t, "go build -o app .", readDeclaredBuild(root).Build)
}

// --- Taskfile and justfile --------------------------------------------------

func TestATaskfileDeclaresTheBuildTheSameWayAMakefileDoes(t *testing.T) {
	root := withFiles(t, map[string]string{
		"go.mod": "module example.com/app\n",
		"Taskfile.yml": "version: '3'\ntasks:\n" +
			"  build:\n    cmds:\n      - go build -o app ./cmd/server\n" +
			"  start:\n    cmds:\n      - ./app\n",
	})

	d := readDeclaredBuild(root)
	require.Equal(t, rankRunner, d.Rank)
	require.Equal(t, "Taskfile.yml", d.Source)
	require.Equal(t, "task build", d.Build)
	require.Equal(t, "./app", d.Start)
	require.Contains(t, d.Packages, "go-task", "task is not in any provider's environment")
}

func TestAJustfileDeclaresTheBuildToo(t *testing.T) {
	root := withFiles(t, map[string]string{
		"go.mod":   "module example.com/app\n",
		"justfile": "build:\n    go build -o app ./cmd/server\n\nstart:\n    ./app\n",
	})

	d := readDeclaredBuild(root)
	require.Equal(t, rankRunner, d.Rank)
	require.Equal(t, "justfile", d.Source)
	require.Equal(t, "just build", d.Build)
	require.Equal(t, "./app", d.Start)
	require.Contains(t, d.Packages, "just")
}

// A runner file with no build target is lint and release helpers, not a
// deployment instruction.
func TestARunnerWithoutABuildTargetIsNotADeclaration(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"taskfile": {"Taskfile.yml": "version: '3'\ntasks:\n  lint:\n    cmds:\n      - golangci-lint run\n"},
		"justfile": {"justfile": "lint:\n    golangci-lint run\n"},
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, readDeclaredBuild(withFiles(t, files)).found())
		})
	}
}

// --- Procfile ---------------------------------------------------------------

// A Procfile says how to run the app and nothing about building it, so it
// contributes a start command and leaves the build to nixpacks.
func TestAProcfileDeclaresTheStartCommandOnly(t *testing.T) {
	root := withFiles(t, map[string]string{
		"go.mod":   "module example.com/app\n",
		"Procfile": "web: ./app --port $PORT\nworker: ./app --worker\n",
	})

	// $PORT cannot survive the trip into a Dockerfile, so this Procfile says
	// nothing usable and the repository falls through to convention-matching.
	// Declining is the point: a start command with an unexpanded variable in it
	// would produce a container that fails to start for a reason nobody can see
	// from the proposal.
	require.False(t, readDeclaredBuild(root).found())

	// Without the variable it is taken.
	root = withFiles(t, map[string]string{
		"go.mod":   "module example.com/app\n",
		"Procfile": "web: ./app\nworker: ./app --worker\n",
	})
	d := readDeclaredBuild(root)
	require.Equal(t, rankProcfile, d.Rank)
	require.Equal(t, "./app", d.Start)
	require.Empty(t, d.Build, "nixpacks still plans the build")
}

// R-021: a declared worker is not a request to deploy one.
func TestAProcfilesOtherProcessesAreNotDeployed(t *testing.T) {
	root := withFiles(t, map[string]string{
		"Procfile": "worker: ./app --worker\nrelease: ./migrate\n",
	})
	require.False(t, readDeclaredBuild(root).found(),
		"no web process means nothing here says what the app is")
}

// --- composition ------------------------------------------------------------

// A reading that names the build replaces nixpacks' choice; one that names an
// ordering wraps it.
func TestOnlyAnOrderingNeedsTheSecondPass(t *testing.T) {
	named := declaredBuild{Rank: rankRunner, Build: "make build"}
	require.False(t, named.needsSecondPass())
	require.Contains(t, strings.Join(named.nixpacksArgs(""), " "), "--build-cmd make build")

	ordered := declaredBuild{Rank: rankEmbedded, Before: "(cd web && npm ci)"}
	require.True(t, ordered.needsSecondPass())
	require.NotContains(t, strings.Join(ordered.nixpacksArgs(""), " "), "--build-cmd",
		"on the first pass there is nothing to come before yet")
	require.Contains(t, strings.Join(ordered.nixpacksArgs("go build ."), " "),
		"--build-cmd (cd web && npm ci) && go build .")
}

// The build command is read back out of the plan so the second pass knows what
// it is wrapping.
func TestTheChosenBuildCommandIsReadBackOutOfThePlan(t *testing.T) {
	plan := "FROM ubuntu:noble\n" +
		"RUN nix-env -if .nixpacks/nixpkgs-abc.nix && nix-collect-garbage -d\n" +
		"RUN --mount=type=cache,id=x,target=/root/.cache/go-build go mod download\n" +
		"RUN --mount=type=cache,id=x,target=/root/.cache/go-build go build -o out ./cmd/server\n" +
		"RUN true\n\n" +
		"FROM ubuntu:noble\n" +
		"RUN echo not-a-build\n" +
		"CMD [\"./out\"]\n"

	require.Equal(t, "go build -o out ./cmd/server", planBuildCommand(plan),
		"the last build-stage RUN that is not nixpacks' own bookkeeping")
}
