package detect

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// QuestionKindChoice is a small indirection so question.go does not import api.
func QuestionKindChoice() api.QuestionKind { return api.QuestionChoice }

// --- Dockerfile ------------------------------------------------------------

// DockerfileDetector recognizes a repository that says how to build itself.
type DockerfileDetector struct{}

func (DockerfileDetector) Name() string { return "dockerfile" }

func (d DockerfileDetector) Bid(_ context.Context, src api.SourceView) (Candidate, error) {
	root, rootErr := src.Stat("Dockerfile")
	found, _ := src.Glob("Dockerfile")
	nested := deployable(found)

	switch {
	case rootErr == nil && !root.IsDir:
		return d.bidForRoot(src)

	case len(nested) > 0:
		// A Dockerfile exists but not at the root. Pando does not pick one —
		// which service a monorepo means to deploy is not something the file
		// layout answers (R-021).
		sort.Strings(nested)
		return Candidate{
			Strategy:   spec.BuildDockerfile,
			Confidence: 0.45,
			Evidence:   []string{fmt.Sprintf("%d Dockerfiles, none at the repository root", len(nested))},
			Questions: []Question{{
				Key:     "dockerfile_path",
				Kind:    api.QuestionChoice,
				Options: nested,
				Prompt: fmt.Sprintf(
					"This repository contains %d Dockerfiles and none at the top level, so Pando could not "+
						"tell which one builds the app you want to deploy. The files are: %s. "+
						"Valid answer: one of those paths.",
					len(nested), strings.Join(truncate(nested, 8), ", ")),
				Why: "Pando builds one app per repository and needs to know which Dockerfile describes it.",
			}},
			Draft: Draft{Build: spec.Build{Strategy: spec.BuildDockerfile}},
		}, nil

	default:
		// Either there are no Dockerfiles, or every one of them sits somewhere
		// that is not this repository's app. Both are "nothing to bid on":
		// offering a choice between five Dockerfiles that all build something
		// else is worse than saying nothing, because R-005 says the person
		// choosing may not know what a Dockerfile is.
		return Candidate{Confidence: 0}, nil
	}
}

// notDeployable are directories whose contents demonstrate, test, or develop
// something rather than being the thing this repository deploys.
//
// This exists because a file's presence is not evidence on its own — where it
// sits is part of what it means. vercel/turbo carries five Dockerfiles and not
// one of them builds turbo: two are sample projects under examples/, two are
// lockfile test fixtures, and one is a devcontainer. A detector that reads
// "5 Dockerfiles" as "this repository builds with a Dockerfile" has mistaken
// file-counting for understanding.
var notDeployable = []string{
	"example", "examples", "sample", "samples",
	"test", "tests", "testdata", "fixture", "fixtures", "__tests__",
	"doc", "docs", "template", "templates", "demo", "demos",
	".devcontainer", ".github", "vendor", "node_modules", "third_party",
}

// deployable filters out paths that live under a directory in notDeployable.
func deployable(paths []string) []string {
	var kept []string
	for _, p := range paths {
		if !underNotDeployable(p) {
			kept = append(kept, p)
		}
	}
	return kept
}

func underNotDeployable(p string) bool {
	for _, segment := range strings.Split(path.Clean(p), "/") {
		lower := strings.ToLower(segment)
		for _, skip := range notDeployable {
			if lower == skip {
				return true
			}
		}
	}
	return false
}

func (d DockerfileDetector) bidForRoot(src api.SourceView) (Candidate, error) {
	c := Candidate{
		Strategy:   spec.BuildDockerfile,
		Confidence: 0.92,
		Evidence:   []string{"Dockerfile at repository root"},
		Draft:      Draft{Build: spec.Build{Strategy: spec.BuildDockerfile, Dockerfile: "Dockerfile"}},
	}

	ports, cmd := readDockerfile(src, "Dockerfile")
	workload := spec.Workload{Name: "web", Primary: true, Exposed: true}

	for _, p := range ports {
		// EXPOSE is a declaration by the person who wrote the Dockerfile, which
		// is better evidence than a framework guess and worse than watching the
		// app bind. The source is recorded so the review UI can say which
		// (design 01 §2.3).
		workload.Ports = append(workload.Ports, spec.Port{
			Number: p, Protocol: "http", Source: spec.PortExpose,
		})
	}
	if len(ports) > 0 {
		c.Evidence = append(c.Evidence, fmt.Sprintf("EXPOSE %s", joinInts(ports)))
		c.Confidence = 0.95
	} else {
		// No EXPOSE. The trial run may still observe a bound port (R-097), so
		// this is a question the trial run can answer rather than one the user
		// must.
		c.Questions = append(c.Questions, Question{
			Key:      "primary_port",
			Kind:     api.QuestionPort,
			Deferred: true,
			Prompt: "This app builds from a Dockerfile that does not declare which port it listens on. " +
				"Pando will watch the app start and try to work it out, but if that does not " +
				"succeed it needs to be told. Valid answer: a port number, such as 3000 or 8080.",
			Why: "Pando needs to know where to send traffic once the app is running.",
		})
	}
	if cmd != "" {
		c.Evidence = append(c.Evidence, "CMD "+cmd)
	}

	c.Draft.Workloads = []spec.Workload{workload}
	c.Draft.Slots = slotsFromEnvExample(src)
	return c, nil
}

// --- Compose ---------------------------------------------------------------

// ComposeDetector recognizes a repository that says how its parts fit together.
type ComposeDetector struct{}

func (ComposeDetector) Name() string { return "compose" }

var composeNames = []string{
	"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml",
}

func (d ComposeDetector) Bid(_ context.Context, src api.SourceView) (Candidate, error) {
	var found string
	for _, name := range composeNames {
		if info, err := src.Stat(name); err == nil && !info.IsDir {
			found = name
			break
		}
	}
	if found == "" {
		return Candidate{Confidence: 0}, nil
	}

	c := Candidate{
		Strategy:   spec.BuildCompose,
		Confidence: 0.88,
		Evidence:   []string{found + " at repository root"},
	}

	// R-096: a compose file is a complete answer, not a hint. The import is the
	// bid — there is nothing left to infer once the author has said what runs.
	draft, err := ImportCompose(src, found)
	if err != nil {
		// The bid stands, carrying the reason. A compose file at the root is
		// still the right reading of this repository even when it cannot be
		// imported, and "here is the construct that cannot run" is a better
		// answer than quietly bidding buildpack instead (R-099).
		c.Blocked = err
		c.Draft = Draft{Build: spec.Build{Strategy: spec.BuildCompose, ComposeFile: found}}
		//nolint:nilerr // Returning the error here would make the auction drop
		// this detector, which is right for a detector that broke and wrong for
		// one that worked and found a reason. The reason travels on the
		// candidate instead, and the auction surfaces it as StatusBlocked.
		return c, nil
	}
	c.Draft = draft

	services := make([]string, 0, len(draft.Workloads))
	for _, w := range draft.Workloads {
		services = append(services, w.Name)
	}
	if len(services) > 0 {
		c.Evidence = append(c.Evidence,
			fmt.Sprintf("%d services: %s", len(services), strings.Join(truncate(services, 6), ", ")))
	}
	if len(draft.Volumes) > 0 {
		c.Evidence = append(c.Evidence, fmt.Sprintf("%d declared volume(s)", len(draft.Volumes)))
	}

	// A compose file describes several services; exactly one is the app's
	// canonical endpoint (R-026). Which one is not something the file says, so
	// Pando asks rather than picking the first.
	if len(services) > 1 {
		c.Questions = append(c.Questions, Question{
			Key:     "primary_service",
			Kind:    api.QuestionChoice,
			Options: services,
			Prompt: fmt.Sprintf(
				"This app is made of %d parts that run together: %s. Pando needs to know which one "+
					"serves the app's web interface — the one a person opens in a browser. "+
					"Valid answer: one of those names.",
				len(services), strings.Join(services, ", ")),
			Why: "Pando gives the app one address, and sends traffic there.",
		})
	}

	return c, nil
}

// --- Static ----------------------------------------------------------------

// StaticDetector recognizes a site with no server.
type StaticDetector struct{}

func (StaticDetector) Name() string { return "static" }

func (d StaticDetector) Bid(_ context.Context, src api.SourceView) (Candidate, error) {
	// An index.html at the root, or in one of the usual output directories.
	candidates := []string{"index.html", "public/index.html", "dist/index.html", "site/index.html"}

	var dir string
	for _, candidate := range candidates {
		if info, err := src.Stat(candidate); err == nil && !info.IsDir {
			dir = path.Dir(candidate)
			break
		}
	}
	if dir == "" {
		return Candidate{Confidence: 0}, nil
	}

	confidence := 0.7
	evidence := []string{"index.html in " + displayDir(dir)}
	var questions []Question

	// A package.json alongside means there is probably a build step. What that
	// implies depends entirely on where the index.html was found, and reading it
	// as one thing was wrong:
	//
	//   - at the repository root, the file is source. Serving the source
	//     directory ships an unbuilt site, so bid low and let buildpack win.
	//   - in dist/ or public/, the file is build output that is committed and
	//     already there. That is stronger evidence than no package.json at all,
	//     not weaker — the site is built and sitting in the repository.
	//
	// h5bp/html5-boilerplate is the second case, and the blanket penalty had it
	// losing to a buildpack that would have tried to npm-start a folder of HTML.
	if _, err := src.Stat("package.json"); err == nil {
		switch {
		case dir == "." && hasBuildScript(src):
			// A root index.html next to a declared build command is source, not
			// output — the file almost certainly references modules that only
			// resolve after the build. Serving it unbuilt produces a blank page,
			// so this bid should lose to buildpack outright rather than land
			// close enough to it to make the auction ask which.
			confidence = 0.2
			evidence = append(evidence,
				"package.json declares a build command, so the repository root is source rather than a built site")

		case dir == ".":
			// A package.json with no build command may just be a dependency
			// manifest beside a page that is already servable. Uncertain rather
			// than wrong.
			confidence = 0.35
			evidence = append(evidence, "package.json is also present, so this may need building first")

		case hasBuildScript(src):
			// Committed output plus a build script is a genuine fork: serve what
			// is in the repository, or run the build and serve what comes out.
			// The committed copy can be stale, and that failure is silent — the
			// site deploys and looks fine while being several commits behind.
			evidence = append(evidence,
				"package.json declares a build script, so "+dir+"/ may be older than the source")
			questions = append(questions, Question{
				Key:     "static_source",
				Kind:    api.QuestionChoice,
				Options: []string{"serve-committed", "run-build"},
				Prompt: fmt.Sprintf(
					"This looks like a static website. A built copy of it is already committed in the "+
						"%s/ directory, but the project also declares a build command, so that copy may be "+
						"older than the rest of the source. Pando can either publish the committed copy as "+
						"it is, or run the build first and publish what it produces. "+
						"Valid answer: either \"serve-committed\" or \"run-build\".",
					dir),
				Why: "Publishing a stale copy of a site usually looks like it worked, which is why Pando asks rather than choosing.",
			})
		}
	}

	return Candidate{
		Strategy:   spec.BuildStatic,
		Confidence: confidence,
		Evidence:   evidence,
		Questions:  questions,
		Draft: Draft{
			Build: spec.Build{Strategy: spec.BuildStatic, StaticDir: dir},
			Workloads: []spec.Workload{{
				Name: "web", Primary: true, Exposed: true,
				Ports: []spec.Port{{Number: 80, Protocol: "http", Source: spec.PortFramework}},
			}},
		},
	}, nil
}

// --- Buildpack -------------------------------------------------------------

// BuildpackDetector recognizes a project by its language, for repositories that
// carry no deployment instructions at all.
//
// This is the case R-103 is really about: an app written by someone who never
// thought about deployment. The bid is deliberately modest, because a language
// is not a deployment — but modest is not the same as inquisitive, and what it
// can work out it does not ask. With a planner attached it asks nothing at all.
type BuildpackDetector struct {
	// Planner turns a repository with no deployment instructions into build
	// inputs. Supplied by the builder adapter, because what those inputs are is
	// the builder's vocabulary and core does not learn it (R-251): core asks
	// "how would you build this", stores whatever comes back, and interprets
	// none of it.
	//
	// Nil means detection still recognizes the language and asks its questions;
	// the plan simply is not part of the proposal, and the builder makes one at
	// build time instead. That is the behavior every install had before the
	// plan was reviewable.
	Planner BuildPlanner
}

// BuildPlanner produces the files a buildpack build needs.
//
// Planning reads the repository and decides; it runs nothing from it and builds
// nothing, so it is not a build and R-024's "never on the host" does not bite.
// What it returns is opaque to core.
type BuildPlanner interface {
	Plan(ctx context.Context, src api.SourceView) (files map[string]string, dockerfile string, err error)
}

func (BuildpackDetector) Name() string { return "buildpack" }

type languageSignal struct {
	file     string
	language string
	start    string
	port     int
}

var languages = []languageSignal{
	{"package.json", "Node.js", "npm start", 3000},
	{"requirements.txt", "Python", "python app.py", 8000},
	{"pyproject.toml", "Python", "python -m app", 8000},
	{"go.mod", "Go", "the compiled binary", 8080},
	{"Gemfile", "Ruby", "bundle exec rails server", 3000},
	{"composer.json", "PHP", "php -S", 8000},
	{"Cargo.toml", "Rust", "the compiled binary", 8080},
	{"pom.xml", "Java", "java -jar", 8080},
}

func (d BuildpackDetector) Bid(ctx context.Context, src api.SourceView) (Candidate, error) {
	var signal *languageSignal
	for i := range languages {
		if info, err := src.Stat(languages[i].file); err == nil && !info.IsDir {
			signal = &languages[i]
			break
		}
	}
	if signal == nil {
		return Candidate{Confidence: 0}, nil
	}

	c := Candidate{
		Strategy: spec.BuildBuildpack,
		// Modest on purpose. Knowing the language is not knowing how to run the
		// app, and a confident wrong answer is worse than an honest question.
		Confidence: 0.45,
		Evidence:   []string{signal.file + " — this looks like a " + signal.language + " project"},
		Draft: Draft{
			Build: spec.Build{Strategy: spec.BuildBuildpack},
			Workloads: []spec.Workload{{
				Name: "web", Primary: true, Exposed: true,
				Ports: []spec.Port{{Number: signal.port, Protocol: "http", Source: spec.PortFramework}},
			}},
		},
	}

	// The plan, so the proposal shows how this app would be built rather than
	// only that it would be. A planner that fails leaves the bid standing: a
	// language Pando recognizes is still the right reading of the repository,
	// and the builder will try again at build time with a real error to show.
	var planned string
	if d.Planner != nil {
		if files, dockerfile, err := d.Planner.Plan(ctx, src); err == nil && len(files) > 0 {
			c.Draft.Build.GeneratedFiles = files
			c.Draft.Build.Dockerfile = dockerfile
			c.Evidence = append(c.Evidence, "build plan generated, and editable before it runs")
			_, planned = scanDockerfile(strings.NewReader(files[dockerfile]))
		}
	}

	// Asked only when nothing worked it out. The plan is the thing that works it
	// out: nixpacks reads the repository and writes a Dockerfile ending in the
	// command that starts the app, so on a plain Go module the answer is already
	// in hand before the question would be put.
	//
	// It used to be asked either way, two lines after the plan was attached to
	// the draft — which made the prompt's own first clause ("it does not include
	// a Dockerfile or any other instructions for running it") false at the
	// moment it was shown. R-103 counts questions as the product metric and
	// R-104 says Pando asks only when it genuinely cannot proceed; it could.
	if planned == "" {
		c.Questions = append(c.Questions, Question{
			Key:  "start_command",
			Kind: api.QuestionText,
			Prompt: fmt.Sprintf(
				"This app appears to be a %s project, but it does not include a Dockerfile or any other "+
					"instructions for running it. Pando needs the command that starts it. "+
					"Valid answer: a shell command, such as %q.",
				signal.language, signal.start),
			Why: "Pando runs this command to start the app.",
		})
	} else {
		c.Evidence = append(c.Evidence, "starts with "+planned)
	}

	// The port is not asked at all, and the draft's framework default stands.
	//
	// R-097 says a port is observed rather than asked, and for an app that comes
	// with an image that is what happens. A source build has no image until it
	// is built, so the trial run has nothing to start (Job.trial returns early
	// on an empty image) — and a deferred question whose trial never runs is
	// promoted to one a person has to answer. That put a port question in front
	// of someone R-005 says may not know what a port is, on every repository
	// with no Dockerfile.
	//
	// R-104 settles it: anything with a reasonable default gets the default and
	// is changeable later. The default is already in the draft above, carried as
	// PortFramework so the review screen says where it came from, and editable
	// there before anything is pinned.
	c.Evidence = append(c.Evidence, fmt.Sprintf(
		"assumed to serve HTTP on %d, the usual port for a %s app — change it below if it does not",
		signal.port, signal.language))

	c.Draft.Slots = slotsFromEnvExample(src)
	return c, nil
}

// --- shared helpers --------------------------------------------------------

var exposePattern = regexp.MustCompile(`(?i)^\s*EXPOSE\s+(.+)`)
var cmdPattern = regexp.MustCompile(`(?i)^\s*(CMD|ENTRYPOINT)\s+(.+)`)

// readDockerfile pulls the declared ports and start command out of a Dockerfile.
//
// Deliberately shallow: this reads what the file states, and does not try to
// interpret the build. R-021 — Pando fills what is declared and does not invent.
func readDockerfile(src api.SourceView, name string) ([]int, string) {
	f, err := src.Open(name)
	if err != nil {
		return nil, ""
	}
	defer func() { _ = f.Close() }()
	return scanDockerfile(f)
}

// scanDockerfile is readDockerfile's parser, over content rather than a file.
//
// Separate because a generated build plan arrives as a string — the planner
// hands back the Dockerfile it wrote, and there is no second copy on disk to
// open.
//
// The last start instruction wins, not the first. A multi-stage build declares
// one per stage and only the final stage's survives into the image, so taking
// the first reports a builder stage's command as the app's. nixpacks writes
// exactly that shape: `ENTRYPOINT ["/bin/bash", "-l", "-c"]` in the build stage
// and the real `CMD` at the end.
func scanDockerfile(r io.Reader) ([]int, string) {
	var ports []int
	var command string

	scanner := bufio.NewScanner(io.LimitReader(r, 256<<10))
	for _, line := range joinContinuations(scanner) {
		if m := exposePattern.FindStringSubmatch(line); m != nil {
			for _, field := range strings.Fields(m[1]) {
				if n, err := strconv.Atoi(strings.SplitN(field, "/", 2)[0]); err == nil {
					ports = append(ports, n)
				}
			}
		}
		if m := cmdPattern.FindStringSubmatch(line); m != nil {
			command = strings.TrimSpace(m[2])
		}
	}
	return ports, command
}

// joinContinuations folds backslash-continued lines into one.
//
// Without this a Dockerfile written the ordinary way lies to the reader:
//
//	HEALTHCHECK --interval=30s --timeout=3s --retries=3 \\
//	  CMD wget --spider http://localhost:3001/ || exit 1
//
//	CMD ["node", "dist/server/index.js"]
//
// The second physical line begins with CMD, so a line-at-a-time scan reports
// the health probe as the app's start command and never reaches the real one.
// This is shown to a user as the evidence for what Pando decided, and evidence
// that is confidently wrong is worse than none at all.
func joinContinuations(scanner *bufio.Scanner) []string {
	var lines []string
	var pending strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if trimmed := strings.TrimRight(line, " 	"); strings.HasSuffix(trimmed, "\\") {
			pending.WriteString(strings.TrimSuffix(trimmed, "\\"))
			pending.WriteString(" ")
			continue
		}
		if pending.Len() > 0 {
			pending.WriteString(line)
			lines = append(lines, pending.String())
			pending.Reset()
			continue
		}
		lines = append(lines, line)
	}
	if pending.Len() > 0 {
		lines = append(lines, pending.String())
	}
	return lines
}

// slotsFromComposeServices turns recognizable backing services into slots.
//
// A service whose image is postgres or redis is a dependency the app declares,
// which is exactly what a slot is for (R-131). Anything unrecognized stays a
// workload — Pando does not guess at what an unfamiliar image is.
func slotsFromComposeServices(services []string, images map[string]string) []spec.Slot {
	known := map[string]spec.SlotType{
		"postgres": spec.SlotPostgres, "postgis": spec.SlotPostgres,
		"mysql": spec.SlotMySQL, "mariadb": spec.SlotMySQL,
		"redis": spec.SlotRedis, "valkey": spec.SlotRedis,
	}

	var slots []spec.Slot
	for _, service := range services {
		image := strings.ToLower(images[service])
		for prefix, slotType := range known {
			if !strings.Contains(image, prefix) && !strings.Contains(strings.ToLower(service), prefix) {
				continue
			}
			slots = append(slots, spec.Slot{
				Key:      strings.ToUpper(service) + "_URL",
				Type:     slotType,
				Required: true,
				Evidence: []string{fmt.Sprintf("compose service %q runs %s", service, orUnknownImage(images[service]))},
			})
			break
		}
	}
	return slots
}

var envAssignment = regexp.MustCompile(`^\s*([A-Z][A-Z0-9_]*)\s*=\s*(.*)$`)

// slotsFromEnvExample reads .env.example for declared configuration.
//
// O-4 is unresolved: which of forty keys actually matter has no reliable
// derivation. The [P] fallback is to default everything not typed to a known
// service to optional, and let the trial run promote what actually breaks
// (design 01 §2.5). That turns an unanswerable question into an observation.
func slotsFromEnvExample(src api.SourceView) []spec.Slot {
	var slots []spec.Slot

	for _, name := range []string{".env.example", ".env.sample", ".env.template"} {
		f, err := src.Open(name)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(io.LimitReader(f, 64<<10))
		for scanner.Scan() {
			m := envAssignment.FindStringSubmatch(scanner.Text())
			if m == nil {
				continue
			}
			key, value := m[1], strings.TrimSpace(m[2])

			slotType := slotTypeFor(key)
			slots = append(slots, spec.Slot{
				Key:  key,
				Type: slotType,
				// Required only when the key names a service Pando recognizes.
				// Everything else starts optional and is promoted by the trial
				// run if its absence actually breaks the app.
				Required: slotType != spec.SlotUnknown && value == "",
				Evidence: []string{"declared in " + name},
			})
		}
		_ = f.Close()
		break
	}
	return slots
}

func slotTypeFor(key string) spec.SlotType {
	upper := strings.ToUpper(key)
	switch {
	case strings.Contains(upper, "POSTGRES"), strings.Contains(upper, "DATABASE_URL"),
		strings.Contains(upper, "PG"):
		return spec.SlotPostgres
	case strings.Contains(upper, "MYSQL"):
		return spec.SlotMySQL
	case strings.Contains(upper, "REDIS"):
		return spec.SlotRedis
	case strings.Contains(upper, "S3"), strings.Contains(upper, "BUCKET"):
		return spec.SlotS3
	case strings.Contains(upper, "SMTP"), strings.Contains(upper, "MAIL"):
		return spec.SlotSMTP
	default:
		return spec.SlotUnknown
	}
}

func truncate(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return append(append([]string(nil), items[:n]...), "…")
}

func joinInts(ns []int) string {
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ", ")
}

func displayDir(dir string) string {
	if dir == "." {
		return "the repository root"
	}
	return dir + "/"
}

func orUnknownImage(image string) string {
	if image == "" {
		return "an image Pando could not read"
	}
	return image
}

// --- Monorepo --------------------------------------------------------------

// MonorepoDetector recognizes a repository that holds many projects rather than
// one app.
//
// This bids StrategyUnknown, which is not a contradiction: being confident that
// a repository is not a single deployable app is a real detection result, and a
// more useful one than a low-confidence guess at which of its twenty packages
// was meant. R-021 is the rule — Pando fills what is declared and does not
// invent topology — and a workspace manifest is the repository declaring, in as
// many words, that it contains several things.
//
// It is a veto rather than a bid, so it stands down whenever the repository
// does say what to deploy: a Dockerfile or compose file at the root answers the
// question this detector would otherwise ask.
type MonorepoDetector struct{}

func (MonorepoDetector) Name() string { return "monorepo" }

// workspaceMarkers are files that declare a repository holds multiple packages.
var workspaceMarkers = []string{
	"pnpm-workspace.yaml", "pnpm-workspace.yml",
	"lerna.json", "nx.json", "rush.json", "turbo.json", "go.work",
}

// packageDirs are where multi-project repositories conventionally keep them.
var packageDirs = []string{"apps", "packages", "services", "cmd", "projects"}

func (d MonorepoDetector) Bid(_ context.Context, src api.SourceView) (Candidate, error) {
	// If the root says how to build the repository, there is nothing to veto.
	if info, err := src.Stat("Dockerfile"); err == nil && !info.IsDir {
		return Candidate{Confidence: 0}, nil
	}
	for _, name := range composeNames {
		if info, err := src.Stat(name); err == nil && !info.IsDir {
			return Candidate{Confidence: 0}, nil
		}
	}

	var markers []string
	for _, name := range workspaceMarkers {
		if info, err := src.Stat(name); err == nil && !info.IsDir {
			markers = append(markers, name)
		}
	}
	if hasWorkspacesField(src) {
		markers = append(markers, "package.json workspaces")
	}
	if hasCargoWorkspace(src) {
		markers = append(markers, "Cargo.toml [workspace]")
	}
	if len(markers) == 0 {
		return Candidate{Confidence: 0}, nil
	}

	// Name what was found. R-105 requires the question stand on its own, and
	// "which one do you want?" without a list is exactly the fragment it bans.
	var found []string
	for _, dir := range packageDirs {
		entries, err := src.Glob(dir + "/*")
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(path.Base(entry), ".") {
				continue
			}
			if info, err := src.Stat(entry); err == nil && info.IsDir {
				found = append(found, entry)
			}
		}
	}
	sort.Strings(found)

	evidence := []string{"workspace declared by " + strings.Join(markers, ", ")}
	if len(found) > 0 {
		evidence = append(evidence,
			fmt.Sprintf("%d projects under %s", len(found), strings.Join(packageDirsPresent(src), ", ")))
	}

	return Candidate{
		Strategy: StrategyUnknown,
		// High, because the evidence is a declaration rather than an inference.
		// The repository is not being read between the lines; it says so.
		Confidence: 0.8,
		Evidence:   evidence,
		Questions:  []Question{monorepoQuestion(markers, found)},
	}, nil
}

func monorepoQuestion(markers, found []string) Question {
	q := Question{
		Key:  "deployable_project",
		Kind: api.QuestionText,
		Why:  "Pando deploys one app at a time, and this repository contains several.",
	}

	if len(found) > 0 {
		q.Kind = api.QuestionChoice
		q.Options = found
		q.Prompt = fmt.Sprintf(
			"This repository holds several separate projects rather than one app — it declares a "+
				"workspace (%s) and contains %d projects: %s. Pando deploys one app at a time and "+
				"cannot tell which of these you mean. Valid answer: one of those paths.",
			strings.Join(markers, ", "), len(found), strings.Join(truncate(found, 12), ", "))
		return q
	}

	q.Prompt = fmt.Sprintf(
		"This repository declares a workspace (%s), which means it is meant to hold several "+
			"separate projects rather than one app. Pando deploys one app at a time and could not "+
			"find a single thing here to deploy. Valid answer: the path within the repository of "+
			"the project you want deployed, for example \"apps/web\".",
		strings.Join(markers, ", "))
	return q
}

func packageDirsPresent(src api.SourceView) []string {
	var present []string
	for _, dir := range packageDirs {
		if info, err := src.Stat(dir); err == nil && info.IsDir {
			present = append(present, dir+"/")
		}
	}
	return present
}

// hasBuildScript reports whether package.json declares a build command.
func hasBuildScript(src api.SourceView) bool {
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if !readJSON(src, "package.json", &pkg) {
		return false
	}
	return strings.TrimSpace(pkg.Scripts["build"]) != ""
}

// hasWorkspacesField reports whether package.json declares workspaces.
//
// The field is either an array of globs or an object with a "packages" array,
// so it is read as raw JSON and only tested for being present and non-empty.
func hasWorkspacesField(src api.SourceView) bool {
	var pkg struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if !readJSON(src, "package.json", &pkg) {
		return false
	}
	trimmed := strings.TrimSpace(string(pkg.Workspaces))
	return trimmed != "" && trimmed != "null" && trimmed != "[]" && trimmed != "{}"
}

// hasCargoWorkspace reports whether Cargo.toml declares a [workspace] table.
func hasCargoWorkspace(src api.SourceView) bool {
	f, err := src.Open("Cargo.toml")
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(io.LimitReader(f, 256<<10))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[workspace]" || strings.HasPrefix(line, "[workspace.") {
			return true
		}
	}
	return false
}

func readJSON(src api.SourceView, name string, into any) bool {
	f, err := src.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, into) == nil
}
