package detect_test

import (
	"context"
	"io"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
)

// memSource is an in-memory SourceView, so detection can be tested against
// exact repository shapes rather than whatever a real repo happens to contain.
type memSource map[string]string

func (m memSource) Open(name string) (io.ReadCloser, error) {
	content, ok := m[path.Clean(name)]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (m memSource) Stat(name string) (api.FileInfo, error) {
	clean := path.Clean(name)
	if content, ok := m[clean]; ok {
		return api.FileInfo{Name: path.Base(clean), Size: int64(len(content))}, nil
	}
	if isDir, ok := m.paths()[clean]; ok && isDir {
		return api.FileInfo{Name: path.Base(clean), IsDir: true}, nil
	}
	return api.FileInfo{}, io.EOF
}

// Glob mirrors source.dirView.Glob, which matches a pattern against the
// relative path as well as the basename. Implied parent directories are walked
// too, so that a pattern like "apps/*" sees them.
func (m memSource) Glob(pattern string) ([]string, error) {
	var out []string
	for name := range m.paths() {
		if ok, _ := path.Match(pattern, name); ok {
			out = append(out, name)
			continue
		}
		if ok, _ := path.Match(pattern, path.Base(name)); ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// paths returns every file plus the directories they imply.
func (m memSource) paths() map[string]bool {
	all := map[string]bool{}
	for name := range m {
		all[name] = false
		for dir := path.Dir(name); dir != "." && dir != "/"; dir = path.Dir(dir) {
			all[dir] = true
		}
	}
	return all
}

func auction() *detect.Auction {
	return detect.NewAuction(
		detect.DockerfileDetector{},
		detect.ComposeDetector{},
		detect.StaticDetector{},
		detect.BuildpackDetector{},
		detect.MonorepoDetector{},
	)
}

// --- R-105, the hard one ---------------------------------------------------

// TestR105_EveryQuestionAnyDetectorProducesIsSelfContained asserts R-105 across
// every repository shape the detectors recognize.
//
// This is the check the phase plan calls a hard content requirement rather than
// a style note. A question is pasted into the assistant that wrote the app, so
// it has to be answerable by something that cannot see the repository — and the
// only way to keep that true is to assert it on every question, not to review
// them once.
func TestR105_EveryQuestionAnyDetectorProducesIsSelfContained(t *testing.T) {
	shapes := map[string]memSource{
		"dockerfile at root":        {"Dockerfile": "FROM alpine\nCMD [\"sh\"]\n"},
		"dockerfile without expose": {"Dockerfile": "FROM alpine\n"},
		"dockerfiles nested only": {
			"services/api/Dockerfile": "FROM alpine\n",
			"services/web/Dockerfile": "FROM alpine\n",
		},
		"compose with several services": {
			"docker-compose.yml": "services:\n  web:\n    image: nginx\n  db:\n    image: postgres:17\n",
		},
		"static site":     {"index.html": "<h1>hi</h1>"},
		"node project":    {"package.json": `{"name":"app"}`},
		"python project":  {"requirements.txt": "flask\n"},
		"go project":      {"go.mod": "module example.com/app\n"},
		"nothing at all":  {"README.md": "# hello"},
		"static and node": {"index.html": "<h1>hi</h1>", "package.json": `{"name":"app"}`},
	}

	for name, src := range shapes {
		result, err := auction().Run(context.Background(), src)
		require.NoError(t, err, name)

		require.NoError(t, detect.ValidateAll(result.Questions),
			"%s produced a question that does not meet R-105", name)

		// Every question is something a person has to answer before their app
		// runs, and R-005 says they may not know what a port is.
		require.LessOrEqual(t, len(result.Questions), 3,
			"%s asks %d questions; each one is a barrier", name, len(result.Questions))
	}
}

// The validator has to actually reject the design's own counter-example, or it
// is decoration.
func TestR105_ValidatorRejectsTheDesignsCounterExample(t *testing.T) {
	bad := detect.Question{
		Key: "port", Kind: api.QuestionPort,
		Prompt: "Which port?",
		Why:    "Pando needs it.",
	}
	err := bad.Validate()
	require.Error(t, err, `"Which port?" is the design's example of a question that fails R-105`)

	good := detect.Question{
		Key: "port", Kind: api.QuestionPort,
		Prompt: "This app appears to be a Node.js service. Pando could not determine which port it " +
			"serves HTTP on. Valid answer: a port number such as 3000.",
		Why: "Pando needs to know where to send traffic once the app is running.",
	}
	require.NoError(t, good.Validate(), "the design's passing example must pass")
}

func TestR105_ValidatorCatchesTheRealFailureModes(t *testing.T) {
	cases := map[string]detect.Question{
		"assumes the reader can see the repo": {
			Key: "k", Why: "because",
			Prompt: "Pando could not determine the port for the service defined in this file. " +
				"Valid answer: a port number such as 3000.",
		},
		"never says what an answer looks like": {
			Key: "k", Why: "because",
			Prompt: "This app appears to be a Node.js service and Pando could not determine which " +
				"port it serves its web interface on, so it needs to be told before deploying.",
		},
		"no why": {
			Key: "k",
			Prompt: "This app appears to be a Node.js service. Pando could not determine which port " +
				"it serves HTTP on. Valid answer: a port number such as 3000.",
		},
		"choice with nothing to choose": {
			Key: "k", Why: "because", Kind: api.QuestionChoice,
			Prompt: "Pando found several services and could not tell which one serves the web " +
				"interface. Valid answer: one of the service names.",
		},
	}

	for name, q := range cases {
		require.Error(t, q.Validate(), "should reject: %s", name)
	}
}

// --- the auction -----------------------------------------------------------

// R-093: the user sees the auction, not a verdict.
func TestR093_RunnersUpAreReturned(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"index.html":   "<h1>hi</h1>",
		"package.json": `{"name":"app"}`,
	})
	require.NoError(t, err)

	require.NotEmpty(t, result.RunnersUp, "a losing bid is shown, not discarded")
	require.NotEmpty(t, result.Winner.Evidence, "the winner says why it won")
	for _, c := range result.RunnersUp {
		require.NotEmpty(t, c.Detector)
		require.NotEmpty(t, c.Evidence, "a runner-up says why it bid too")
	}
}

// R-102: a close call is asked about, not settled.
func TestR102_ACloseCallBecomesAQuestion(t *testing.T) {
	// A repository that is plausibly a static site and plausibly a Node app.
	result, err := auction().Run(context.Background(), memSource{
		"index.html":   "<h1>hi</h1>",
		"package.json": `{"name":"app","scripts":{"build":"vite build"}}`,
	})
	require.NoError(t, err)

	require.Equal(t, detect.StatusNeedsAnswers, result.Status)

	var asked bool
	for _, q := range result.Questions {
		if q.Key == "build_strategy" || q.Key == "start_command" {
			asked = true
		}
	}
	require.True(t, asked, "an ambiguous repository produces a question rather than a pick")
}

func TestDockerfileAtRootWins(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"Dockerfile":   "FROM node:20\nEXPOSE 3000\nCMD [\"npm\",\"start\"]\n",
		"package.json": `{"name":"app"}`,
	})
	require.NoError(t, err)

	require.Equal(t, spec.BuildDockerfile, result.Winner.Strategy,
		"a Dockerfile says how to build this app; a package.json only says what language it is in")
	require.Greater(t, result.Winner.Confidence, 0.9)
	require.Equal(t, detect.StatusReady, result.Status, "nothing left to ask")
	require.Empty(t, result.Questions)

	// EXPOSE is the author's declaration, not a guess — and the review UI shows
	// which (design 01 §2.3).
	ports := result.Winner.Draft.Workloads[0].Ports
	require.Len(t, ports, 1)
	require.Equal(t, 3000, ports[0].Number)
	require.Equal(t, spec.PortExpose, ports[0].Source)
}

func TestComposeBeatsNestedDockerfiles(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"docker-compose.yml": "services:\n  web:\n    build: ./web\n  worker:\n    build: ./worker\n",
		"web/Dockerfile":     "FROM node:20\n",
		"worker/Dockerfile":  "FROM node:20\n",
	})
	require.NoError(t, err)

	require.Equal(t, spec.BuildCompose, result.Winner.Strategy,
		"the compose file says how the parts fit together, which the Dockerfiles do not")
}

// R-026: one canonical endpoint. Which service that is, the file does not say.
func TestComposeAsksWhichServiceIsPrimary(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"docker-compose.yml": "services:\n  web:\n    image: nginx\n  admin:\n    image: nginx\n",
	})
	require.NoError(t, err)

	var q *detect.Question
	for i := range result.Questions {
		if result.Questions[i].Key == "primary_service" {
			q = &result.Questions[i]
		}
	}
	require.NotNil(t, q, "Pando must ask which service the app's URL points at")
	require.ElementsMatch(t, []string{"web", "admin"}, q.Options,
		"the question names the candidates it found")
	require.Contains(t, q.Prompt, "web")
	require.Contains(t, q.Prompt, "admin")
}

// R-021: fill declared slots, never invent topology.
func TestR021_SlotsComeFromWhatTheRepoDeclares(t *testing.T) {
	withDatabase, err := auction().Run(context.Background(), memSource{
		"docker-compose.yml": "services:\n  web:\n    image: nginx\n  db:\n    image: postgres:17\n",
	})
	require.NoError(t, err)
	require.NotEmpty(t, withDatabase.Winner.Draft.Slots, "a declared postgres service becomes a slot")
	require.Equal(t, spec.SlotPostgres, withDatabase.Winner.Draft.Slots[0].Type)
	require.NotEmpty(t, withDatabase.Winner.Draft.Slots[0].Evidence, "and says why")

	// The same app without the declaration gets no slot invented for it.
	withoutDatabase, err := auction().Run(context.Background(), memSource{
		"docker-compose.yml": "services:\n  web:\n    image: nginx\n",
	})
	require.NoError(t, err)
	require.Empty(t, withoutDatabase.Winner.Draft.Slots,
		"Pando does not decide an app needs a database because it looks like it might")
}

// O-4's [P] fallback: default to optional, let the trial run promote what
// actually breaks (design 01 §2.5).
func TestO4_UnrecognizedEnvKeysStartOptional(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"package.json": `{"name":"app"}`,
		".env.example": "DATABASE_URL=\nLOG_LEVEL=info\nFEATURE_X=\nREDIS_URL=\n",
	})
	require.NoError(t, err)

	byKey := map[string]spec.Slot{}
	for _, s := range result.Winner.Draft.Slots {
		byKey[s.Key] = s
	}

	require.True(t, byKey["DATABASE_URL"].Required, "a key naming a known service is required")
	require.True(t, byKey["REDIS_URL"].Required)
	require.False(t, byKey["LOG_LEVEL"].Required, "a key with a default is not required")
	require.False(t, byKey["FEATURE_X"].Required,
		"an unrecognized key starts optional — the trial run promotes it if its absence breaks the app")
}

// R-102 at its most tempting: a repository with almost nothing in it.
func TestAnUnrecognizableRepositoryAsksRatherThanGuesses(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{"README.md": "# hello"})
	require.NoError(t, err)

	require.Equal(t, detect.StatusUnknown, result.Status)
	require.Zero(t, result.Winner.Confidence, "no detector pretends to recognize it")
	require.Len(t, result.Questions, 1)
	require.NoError(t, result.Questions[0].Validate())
	require.Contains(t, result.Questions[0].Prompt, "Valid answer")
}

// A language is not a deployment, and the bid should say so.
func TestALanguageAloneIsALowConfidenceBid(t *testing.T) {
	for file, language := range map[string]string{
		"package.json":     "Node.js",
		"requirements.txt": "Python",
		"go.mod":           "Go",
		"Gemfile":          "Ruby",
	} {
		result, err := auction().Run(context.Background(), memSource{file: "x"})
		require.NoError(t, err)

		require.Equal(t, spec.BuildBuildpack, result.Winner.Strategy, file)
		require.Less(t, result.Winner.Confidence, 0.6,
			"%s tells Pando the language, not how to run the app", file)
		require.Equal(t, detect.StatusNeedsAnswers, result.Status, file)
		require.Contains(t, result.Winner.Evidence[0], language)
	}
}

// R-021 again: a Dockerfile that is not at the root is not automatically the one.
func TestNestedDockerfilesAreAskedAboutNotPicked(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"services/api/Dockerfile": "FROM alpine\n",
		"services/web/Dockerfile": "FROM alpine\n",
	})
	require.NoError(t, err)

	require.Equal(t, detect.StatusNeedsAnswers, result.Status)
	require.NotEmpty(t, result.Questions)
	require.Contains(t, result.Questions[0].Options, "services/api/Dockerfile")
	require.Contains(t, result.Questions[0].Options, "services/web/Dockerfile")
}

// A detector that panics or errors must not take detection down with it.
func TestOneFailingDetectorDoesNotFailDetection(t *testing.T) {
	a := detect.NewAuction(failingDetector{}, detect.DockerfileDetector{})

	result, err := a.Run(context.Background(), memSource{"Dockerfile": "FROM alpine\nEXPOSE 80\n"})
	require.NoError(t, err)
	require.Equal(t, spec.BuildDockerfile, result.Winner.Strategy)
}

type failingDetector struct{}

func (failingDetector) Name() string { return "broken" }
func (failingDetector) Bid(context.Context, api.SourceView) (detect.Candidate, error) {
	return detect.Candidate{}, io.ErrUnexpectedEOF
}

// --- what the corpus caught -------------------------------------------------
//
// The three tests below are each a real detection failure the corpus found on
// its first run against real repositories. They are kept as unit tests because
// the corpus needs the network and these do not: a regression should fail on a
// laptop, not only in CI.

// A Dockerfile under examples/ or fixtures/ builds something the repository is
// demonstrating, not the repository.
//
// vercel/turbo carries five Dockerfiles and not one of them builds turbo. The
// detector bid 0.45 and offered a choice between all five — five wrong answers
// presented as the way forward, to a person R-005 says may not know what a
// Dockerfile is. Saying nothing is better.
func TestADockerfileInAnExampleIsNotEvidenceOfHowToBuild(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"examples/with-docker/apps/web/Dockerfile": "FROM alpine\n",
		"examples/with-docker/apps/api/Dockerfile": "FROM alpine\n",
		".devcontainer/Dockerfile":                 "FROM alpine\n",
		"lockfile-tests/fixtures/a/Dockerfile":     "FROM alpine\n",
		"README.md":                                "# turbo\n",
	})
	require.NoError(t, err)

	require.NotEqual(t, spec.BuildDockerfile, result.Winner.Strategy,
		"Dockerfiles that only exist under examples/, fixtures/ and .devcontainer/ "+
			"are not evidence that this repository builds with a Dockerfile")
	for _, q := range result.Questions {
		require.NotContains(t, q.Options, "examples/with-docker/apps/web/Dockerfile",
			"an example project must not be offered as something to deploy")
	}
}

// A workspace declaration is the repository saying it holds several projects.
func TestAWorkspaceDeclarationIsNotOneApp(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"pnpm-workspace.yaml":      "packages:\n  - apps/*\n",
		"package.json":             `{"name":"turbo","private":true}`,
		"turbo.json":               "{}",
		"apps/web/package.json":    `{"name":"web"}`,
		"apps/docs/package.json":   `{"name":"docs"}`,
		"packages/ui/package.json": `{"name":"ui"}`,
	})
	require.NoError(t, err)

	require.Equal(t, detect.StrategyUnknown, result.Winner.Strategy)
	require.Equal(t, detect.StatusNeedsAnswers, result.Status)
	require.NoError(t, detect.ValidateAll(result.Questions))

	// R-105: the question has to name what it found, because the person reading
	// it — or the assistant they paste it into — cannot see the repository.
	require.Len(t, result.Questions, 1)
	require.ElementsMatch(t,
		[]string{"apps/docs", "apps/web", "packages/ui"},
		result.Questions[0].Options)
}

// The veto stands down when the repository does say what to deploy.
func TestAWorkspaceWithARootDockerfileIsStillBuildable(t *testing.T) {
	for name, src := range map[string]memSource{
		"root Dockerfile": {
			"pnpm-workspace.yaml": "packages:\n  - apps/*\n",
			"Dockerfile":          "FROM alpine\nEXPOSE 8080\n",
			"apps/web/index.js":   "",
		},
		"root compose file": {
			"pnpm-workspace.yaml": "packages:\n  - apps/*\n",
			"compose.yaml":        "services:\n  web:\n    image: nginx\n",
			"apps/web/index.js":   "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := auction().Run(context.Background(), src)
			require.NoError(t, err)
			require.NotEqual(t, detect.StrategyUnknown, result.Winner.Strategy,
				"a root build artifact answers the question the monorepo veto would ask")
		})
	}
}

// Where an index.html sits decides what a package.json beside it means.
//
// h5bp/html5-boilerplate keeps its built site in dist/. A blanket "package.json
// means this needs building" penalty sent it to a buildpack that would have
// tried to npm-start a folder of HTML.
func TestWhereIndexHTMLSitsDecidesWhatPackageJSONMeans(t *testing.T) {
	buildScript := `{"name":"site","scripts":{"build":"gulp build"}}`

	t.Run("committed build output wins, and asks whether it is stale", func(t *testing.T) {
		result, err := auction().Run(context.Background(), memSource{
			"dist/index.html": "<!doctype html>",
			"package.json":    buildScript,
			"src/index.html":  "<!doctype html>",
		})
		require.NoError(t, err)

		require.Equal(t, spec.BuildStatic, result.Winner.Strategy)
		require.GreaterOrEqual(t, result.Winner.Confidence, 0.5)
		require.Len(t, detect.Asked(result.Questions), 1,
			"a committed copy beside a build command is a real fork and worth one question")
		require.NoError(t, detect.ValidateAll(result.Questions))
	})

	t.Run("a root index.html beside a build command is source, not output", func(t *testing.T) {
		result, err := auction().Run(context.Background(), memSource{
			"index.html":   `<script type="module" src="/src/main.jsx"></script>`,
			"package.json": buildScript,
			"src/main.jsx": "",
		})
		require.NoError(t, err)

		require.Equal(t, spec.BuildBuildpack, result.Winner.Strategy,
			"serving an unbuilt Vite root produces a blank page, so static must lose outright")
		for _, q := range result.Questions {
			require.NotEqual(t, "build_strategy", q.Key,
				"static should not land close enough to buildpack to make the auction ask")
		}
	})

	t.Run("no build command leaves it genuinely uncertain", func(t *testing.T) {
		result, err := auction().Run(context.Background(), memSource{
			"index.html":   "<!doctype html>",
			"package.json": `{"name":"site","dependencies":{"normalize.css":"^8"}}`,
		})
		require.NoError(t, err)
		require.Equal(t, detect.StatusNeedsAnswers, result.Status)
	})
}

// R-097: a question the trial run answers is not a question a person answers.
func TestPortQuestionsAreDeferredToTheTrialRun(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"package.json": `{"name":"app"}`,
	})
	require.NoError(t, err)

	var port detect.Question
	for _, q := range result.Questions {
		if q.Key == "primary_port" {
			port = q
		}
	}
	require.Equal(t, "primary_port", port.Key, "a language-only bid should raise the port question")
	require.True(t, port.Deferred,
		"R-097 exists so Pando watches the app bind rather than asking someone who may not know what a port is")

	require.NotContains(t, detect.Asked(result.Questions), port)

	// Deferred is not discarded. The trial run can fail to observe a port, and
	// then it becomes a real question — so it has to still meet R-105.
	require.NoError(t, port.Validate())
}

// --- compose import (R-096, R-099) ------------------------------------------

const composeStack = `
services:
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
    depends_on:
      - db
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost/health"]
      interval: 30s
      timeout: 5s
      retries: 3
    environment:
      DATABASE_URL: postgres://db:5432/app
    restart: unless-stopped
  db:
    image: postgres:16
    volumes:
      - dbdata:/var/lib/postgresql/data
volumes:
  dbdata:
`

// R-096: a compose file is a complete answer, so it is imported rather than
// used as evidence for a guess.
func TestR096_AComposeFileIsImportedNotInterpreted(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{"compose.yaml": composeStack})
	require.NoError(t, err)
	require.Equal(t, spec.BuildCompose, result.Winner.Strategy)

	draft := result.Winner.Draft
	require.Len(t, draft.Workloads, 2)

	web := draft.Workloads[1]
	require.Equal(t, "web", web.Name)
	require.Equal(t, "nginx:alpine", web.Image)
	require.Equal(t, []string{"db"}, web.DependsOn, "depends_on ordering is imported")

	require.NotNil(t, web.Health, "healthchecks are imported")
	require.Equal(t, []string{"curl", "-f", "http://localhost/health"}, web.Health.Command,
		"the CMD prefix says how to run the test, not what to run")
	require.Equal(t, 30, web.Health.IntervalSeconds)
	require.Equal(t, 5, web.Health.TimeoutSeconds)
	require.Equal(t, 3, web.Health.Retries)

	require.Len(t, draft.Volumes, 1)
	require.Equal(t, "dbdata", draft.Volumes[0].Name)
	require.Equal(t, spec.VolumeFromCompose, draft.Volumes[0].Declared)

	// R-131: a service running postgres is a dependency the app declares.
	require.True(t, hasSlot(draft.Slots, spec.SlotPostgres))
}

// The container's port is kept; the host's is replaced by Pando's routing.
func TestR099_APublishedHostPortIsRewrittenNotHonored(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{"compose.yaml": composeStack})
	require.NoError(t, err)

	web := result.Winner.Draft.Workloads[1]
	require.Equal(t, []spec.Port{{Number: 80, Protocol: "http", Source: spec.PortCompose}}, web.Ports,
		"80 is what the service listens on; 8080 is a host publishing Pando replaces")

	require.True(t, hasWarning(result.Winner.Draft.Warnings, spec.WarnComposeConstructRewritten,
		"8080:80"), "a rewrite says exactly what changed")
	require.True(t, hasWarning(result.Winner.Draft.Warnings, spec.WarnComposeConstructRewritten,
		"restart: unless-stopped"))
}

// R-099: constructs that cannot cross the boundary are refused, with the reason.
func TestR099_ConstructsThatBreakTheBoundaryAreRejectedWithReasons(t *testing.T) {
	for name, service := range map[string]string{
		"host networking":     "    network_mode: host\n",
		"privileged":          "    privileged: true\n",
		"host PID":            "    pid: host\n",
		"host IPC":            "    ipc: host\n",
		"device pass-through": "    devices:\n      - /dev/kvm\n",
		"replicas":            "    deploy:\n      replicas: 3\n",
		"host bind mount":     "    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := auction().Run(context.Background(), memSource{
				"compose.yaml": "services:\n  web:\n    image: nginx\n" + service,
			})
			require.NoError(t, err, "a rejection is a result, not a failure of detection")

			require.Equal(t, detect.StatusBlocked, result.Status)
			require.Equal(t, spec.BuildCompose, result.Winner.Strategy,
				"a compose file at the root is still the right reading of the repository")

			require.Error(t, result.Blocked)
			e := errs.As(result.Blocked)
			require.Equal(t, errs.PlanComposeConstructRejected, e.Code)
			require.NotEmpty(t, e.Remedy, "a rejection a user cannot act on is just a wall")
			require.Contains(t, e.Details, "rejected")
		})
	}
}

// The reason must reach the user rather than being swallowed into a fallback.
func TestR099_ARejectedComposeFileDoesNotSilentlyBecomeABuildpackGuess(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    privileged: true\n",
		"package.json": `{"name":"app"}`,
	})
	require.NoError(t, err)

	require.Equal(t, spec.BuildCompose, result.Winner.Strategy,
		"falling through to buildpack would hide the one useful thing Pando knows")
	require.Equal(t, detect.StatusBlocked, result.Status)
	require.Empty(t, result.Questions,
		"there is nothing to ask about a compose file that cannot be imported")
}

// A relative bind mount is data until proven otherwise (R-203).
func TestARelativeBindMountBecomesAManagedVolume(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"compose.yaml": "services:\n  db:\n    image: postgres:16\n" +
			"    volumes:\n      - ./pgdata:/var/lib/postgresql/data\n",
	})
	require.NoError(t, err)
	require.NotEqual(t, detect.StatusBlocked, result.Status,
		"refusing this would refuse most real compose stacks")

	draft := result.Winner.Draft
	require.Len(t, draft.Volumes, 1)
	require.Len(t, draft.Workloads[0].Mounts, 1)
	require.Equal(t, "/var/lib/postgresql/data", draft.Workloads[0].Mounts[0].Path)
	require.True(t, hasWarning(draft.Warnings, spec.WarnComposeConstructRewritten, "./pgdata"),
		"the rewrite names the path, so the user can see what moved")
}

// Compose services live in a map, and Go randomizes map iteration.
func TestComposeImportIsStableAcrossRuns(t *testing.T) {
	src := memSource{"compose.yaml": composeStack}

	first, err := auction().Run(context.Background(), src)
	require.NoError(t, err)

	for range 20 {
		again, err := auction().Run(context.Background(), src)
		require.NoError(t, err)
		require.Equal(t, first.Winner.Draft.Workloads, again.Winner.Draft.Workloads)
		require.Equal(t, first.Questions, again.Questions,
			"the options in a question must not reshuffle between runs of the same repo")
	}
}

func hasSlot(slots []spec.Slot, want spec.SlotType) bool {
	for _, s := range slots {
		if s.Type == want {
			return true
		}
	}
	return false
}

func hasWarning(warnings []spec.Warning, code, mentions string) bool {
	for _, w := range warnings {
		if w.Code == code && strings.Contains(w.Message, mentions) {
			return true
		}
	}
	return false
}

// --- what real repositories caught -----------------------------------------
//
// Both of these came from running detection against apps in the bemeek-io org.
// Neither shape appears in the corpus or in any fixture written from
// imagination, and both were producing confidently wrong output.

// A HEALTHCHECK's continuation line begins with CMD.
//
// From bemeek-io/skyjo-online. Detection reported the health probe as the app's
// start command and never reached the real CMD two lines below it.
func TestAHealthcheckContinuationIsNotTheStartCommand(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"Dockerfile": "FROM node:22-alpine\n" +
			"EXPOSE 3001\n" +
			"HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \\\n" +
			"  CMD wget --no-verbose --tries=1 --spider http://localhost:3001/ || exit 1\n" +
			"\n" +
			`CMD ["node", "dist/server/index.js"]` + "\n",
	})
	require.NoError(t, err)

	evidence := strings.Join(result.Winner.Evidence, " | ")
	require.Contains(t, evidence, "dist/server/index.js",
		"the real CMD is two lines below the healthcheck and has to win")
	require.NotContains(t, evidence, "--spider",
		"evidence that is confidently wrong is worse than none")
	require.Contains(t, evidence, "EXPOSE 3001")
}

// A compose volume source carrying a variable substitution.
//
// From bemeek-io/mashboard: "${CONFIG_DIR:-./config}:/app/config:delegated".
// Splitting that on ":" yields a volume named "${CONFIG_DIR" mounted at
// "-./config}" — not a parse error anywhere, just a bundle that comes up with a
// garbage volume on a nonsense path and an app that cannot find its config.
func TestAComposeVolumeWithAVariableDefaultIsResolved(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"compose.yml": "services:\n" +
			"  backend:\n" +
			"    image: example/backend:latest\n" +
			"    volumes:\n" +
			"      - ${CONFIG_DIR:-./config}:/app/config:delegated\n" +
			"      - redis-data:/data\n" +
			"volumes:\n" +
			"  redis-data:\n",
	})
	require.NoError(t, err)
	require.NotEqual(t, detect.StatusBlocked, result.Status)

	draft := result.Winner.Draft
	for _, v := range draft.Volumes {
		require.NotContains(t, v.Name, "$", "a volume name is not a shell expression")
		require.NotContains(t, v.Name, "{")
	}

	mounts := draft.Workloads[0].Mounts
	require.Len(t, mounts, 2)

	var configPath string
	for _, m := range mounts {
		if m.Path != "/data" {
			configPath = m.Path
		}
	}
	require.Equal(t, "/app/config", configPath,
		"the container path is the field after the source, not the middle of a substitution")
}

// ":ro" is the third field, and finding it depends on splitting correctly.
func TestAReadOnlyMountBehindAVariableIsStillReadOnly(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"compose.yml": "services:\n  web:\n    image: nginx\n" +
			"    volumes:\n      - ${CONFIG_DIR:-./config}:/app/config:ro\n",
	})
	require.NoError(t, err)

	mounts := result.Winner.Draft.Workloads[0].Mounts
	require.Len(t, mounts, 1)
	require.Equal(t, "/app/config", mounts[0].Path)
	require.True(t, mounts[0].ReadOnly)
}

// A substitution with no default cannot be resolved, and says so rather than
// becoming an empty string — which would mount the repository root.
func TestAVariableWithNoDefaultIsNotSilentlyEmptied(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{
		"compose.yml": "services:\n  web:\n    image: nginx\n" +
			"    volumes:\n      - ${DATA_DIR}:/var/data\n",
	})
	require.NoError(t, err)

	mounts := result.Winner.Draft.Workloads[0].Mounts
	require.Len(t, mounts, 1)
	require.Equal(t, "/var/data", mounts[0].Path,
		"an unresolvable source must not shift every other field along")
}

// A compose file that builds from source resolves to a build the builder can
// actually do.
//
// It used to import as `strategy: compose` with a pointer to the file, and
// nothing implements compose — BuildKit declares dockerfile and nothing else.
// So every compose app needing a build was refused at plan time with
// "bld_buildkit cannot build this app the way it is set up", which names the
// builder and not the cause.
//
// It also left the build instructions in the repository to be read later, which
// R-020 forbids: the spec is the sole record of how an app runs.
func TestR020_AComposeBuildIsResolvedIntoTheSpec(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{"compose.yaml": composeBuilds})
	require.NoError(t, err)

	build := result.Winner.Draft.Build
	require.Equal(t, spec.BuildDockerfile, build.Strategy,
		"a compose service with build: is a Dockerfile build, and that is what the builder supports")
	require.Equal(t, "./frontend", build.Context)
	require.Equal(t, "Dockerfile.prod", build.Dockerfile)
	require.Equal(t, "release", build.Target)

	// Kept for provenance: somebody reading this spec later should be able to
	// see where it came from without guessing.
	require.Equal(t, "compose.yaml", build.ComposeFile)
}

// The short spelling — `build: ./dir` — is the context and nothing else.
func TestAComposeBuildStringIsTheContext(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{"compose.yaml": composeBuildString})
	require.NoError(t, err)

	build := result.Winner.Draft.Build
	require.Equal(t, spec.BuildDockerfile, build.Strategy)
	require.Equal(t, ".", build.Context)
	require.Empty(t, build.Dockerfile, "compose's own default applies; Pando does not invent a path")
}

// Every service carrying an image: needs no builder at all.
func TestAComposeFileOfPrebuiltImagesNeedsNoBuilder(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{"compose.yaml": composeStack})
	require.NoError(t, err)

	require.Equal(t, spec.BuildPrebuilt, result.Winner.Draft.Build.Strategy,
		"nothing here is built from source, so requiring a builder would refuse an app that needs none")
}

// More than one buildable service is said out loud rather than silently
// half-done: a service that is quietly not built is one running an image
// somebody forgot they had.
func TestMoreThanOneBuildableServiceWarns(t *testing.T) {
	result, err := auction().Run(context.Background(), memSource{"compose.yaml": composeTwoBuilds})
	require.NoError(t, err)

	draft := result.Winner.Draft
	require.Equal(t, spec.BuildDockerfile, draft.Build.Strategy)

	var found bool
	for _, w := range draft.Warnings {
		if strings.Contains(w.Message, "builds more than one service") {
			found = true
			require.Contains(t, w.Message, "will need an image of their own")
		}
	}
	require.True(t, found, "warnings: %+v", draft.Warnings)
}

const composeBuilds = `
services:
  web:
    build:
      context: ./frontend
      dockerfile: Dockerfile.prod
      target: release
    ports:
      - "3000:3000"
`

const composeBuildString = `
services:
  web:
    build: .
    ports:
      - "3000:3000"
`

const composeTwoBuilds = `
services:
  web:
    build: ./web
    ports:
      - "3000:3000"
  worker:
    build: ./worker
`
