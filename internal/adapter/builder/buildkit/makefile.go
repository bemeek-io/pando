package buildkit

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A Makefile is the maintainer saying how their app is built.
//
// R-094's confidence ladder puts "the maintainer's own build commands" above
// ecosystem manifests, and this is why: convention-matching reads a repository
// and guesses, while a Makefile target is the answer written down by the person
// who wrote the app. macscout is the case that made it concrete — a Go module
// at the root whose Makefile builds a React client into `cmd/server/dist` and
// then embeds it with `go:embed`. Convention-matching sees go.mod, plans
// `go build`, and produces a binary that starts, reports healthy and serves an
// empty page, because the placeholder that makes `go:embed` compile on a fresh
// clone is all that ends up inside it.
//
// Nothing here synthesizes a Dockerfile. R-095 says to wrap a buildpack
// implementation rather than reimplement convention-matching, and that still
// holds: nixpacks decides the environment, and what this adds is the commands
// it should run inside it. `--build-cmd`, `--start-cmd` and `--pkgs` are its
// own flags.

// makefileNames are the names GNU make itself looks for, in its order.
var makefileNames = []string{"GNUmakefile", "makefile", "Makefile"}

// targetPattern matches a target line: a name at the start of a line, then a
// colon. Anything indented is a recipe line and anything starting with a dot is
// a directive such as .PHONY.
var targetPattern = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.\-]*)\s*:[^=]?`)

// declaration is what a Makefile says about building and running an app.
type declaration struct {
	// File is the Makefile's name, for evidence. Empty when there is none.
	File string

	// BuildTarget is the target that builds the app, or empty.
	BuildTarget string

	// StartCommand is the command a run-style target executes, or empty. It is
	// the last thing that target does that is not another make invocation:
	// `run: build` then `./macscout` means the app starts with ./macscout.
	StartCommand string
}

// declares reports whether this Makefile says enough to build from.
func (d declaration) declares() bool { return d.BuildTarget != "" }

// buildTargets are the target names taken to mean "build this app", in
// preference order.
var buildTargets = []string{"build", "all", "compile", "dist"}

// startTargets are the target names taken to mean "run this app".
//
// `start` before `run` because a Makefile that has both usually means `run` for
// development — often a watcher, or two processes — and `start` for the real
// thing. Getting that backwards would deploy somebody's file-watching dev
// server.
var startTargets = []string{"start", "serve", "run"}

// readMakefile parses the Makefile in dir, if there is one.
func readMakefile(dir string) declaration {
	for _, name := range makefileNames {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		d := parseMakefile(f)
		_ = f.Close()
		d.File = name
		return d
	}
	return declaration{}
}

// parseMakefile pulls the targets out of a Makefile.
//
// Deliberately shallow, the same way readDockerfile is: it reads which targets
// exist and what their recipes say, and it does not try to be make. Variables
// are not expanded and includes are not followed, so a Makefile that hides its
// build behind either is simply not recognized — which leaves the repository
// exactly where it was, planned by convention-matching.
func parseMakefile(r io.Reader) declaration {
	recipes := map[string][]string{}
	var current string

	scanner := bufio.NewScanner(io.LimitReader(r, 256<<10))
	for scanner.Scan() {
		line := scanner.Text()

		// A recipe line is indented with a tab. That is make's own rule, and
		// it is what separates "what this target does" from the next target.
		if strings.HasPrefix(line, "\t") {
			if current != "" {
				if step := strings.TrimSpace(line); step != "" && !strings.HasPrefix(step, "#") {
					recipes[current] = append(recipes[current], step)
				}
			}
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := targetPattern.FindStringSubmatch(trimmed); m != nil {
			current = m[1]
			if _, seen := recipes[current]; !seen {
				recipes[current] = nil
			}
			continue
		}
		// A variable assignment, a directive, a conditional — none of which
		// name a target, and all of which end the recipe that came before.
		current = ""
	}

	var d declaration
	for _, name := range buildTargets {
		if _, ok := recipes[name]; ok {
			d.BuildTarget = name
			break
		}
	}
	for _, name := range startTargets {
		steps, ok := recipes[name]
		if !ok {
			continue
		}
		if cmd := startCommandFrom(steps); cmd != "" {
			d.StartCommand = cmd
			break
		}
	}
	return d
}

// startCommandFrom picks the command a run target actually runs.
//
// The last step that is not another make invocation. `run: build` followed by
// `./macscout` is the shape this is for: the dependency does the building and
// the recipe line is the app. A target whose every line is `make something` is
// delegating, and delegating to a target this cannot see is not an answer.
func startCommandFrom(steps []string) string {
	for i := len(steps) - 1; i >= 0; i-- {
		step := strings.TrimLeft(steps[i], "@-+")
		if step == "" || strings.HasPrefix(step, "make ") || step == "make" {
			continue
		}
		// Multi-command lines, `cd web && npm run dev`, and anything with a
		// shell operator are left alone. Running one of them as the container's
		// command would mean guessing which half is the app.
		//
		// Quotes, backslashes and `$` go the same way for a duller reason: this
		// string is handed to nixpacks, which writes it into a Dockerfile CMD,
		// and a value that has to be escaped to survive that round trip is one
		// where a mistake is silent.
		if strings.ContainsAny(step, "&|;><\"'`$\\") {
			continue
		}
		if strings.HasPrefix(step, "echo ") {
			continue
		}
		return step
	}
	return ""
}

// makefileArgs turns a Makefile's declaration into nixpacks flags.
//
// Empty when there is no Makefile, or when the one there declares no build
// target — in which case nixpacks plans exactly as it did before, and a
// repository carrying a Makefile full of lint and release helpers is not
// treated as though it had said something about deployment.
func makefileArgs(contextDir string) []string {
	d := readMakefile(contextDir)
	if !d.declares() {
		return nil
	}

	args := []string{"--build-cmd", "make " + d.BuildTarget}
	if d.StartCommand != "" {
		args = append(args, "--start-cmd", d.StartCommand)
	}

	// make itself is not in the environment nixpacks builds. Its providers
	// install a language toolchain, and none of them has a reason to include
	// GNU make — so the build command would be the first thing to fail.
	pkgs := []string{"gnumake"}

	// And the toolchains the build needs that the chosen provider will not
	// bring. nixpacks picks one provider from what it finds at the root, so a
	// Go module whose Makefile builds a client with npm gets Go and not Node.
	//
	// This reads the repository rather than the Makefile's text on purpose. A
	// package.json is the Node project declaring itself; parsing the recipe for
	// the word "npm" would be reading a shell script and hoping.
	if hasFileNamed(contextDir, "package.json") {
		pkgs = append(pkgs, "nodejs")
	}
	for _, p := range pkgs {
		args = append(args, "--pkgs", p)
	}
	return args
}

// hasFileNamed reports whether name exists anywhere in the tree.
//
// Bounded, because this walks a checkout of somebody else's repository: it
// stops at the first match, skips the directories that hold other people's
// code, and does not descend past maxDepth.
func hasFileNamed(root, name string) bool {
	const maxDepth = 4
	found := false

	// An unreadable directory is not a reason to fail a build, so a walk error
	// skips that subtree and the search carries on.
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if found {
			return filepath.SkipAll
		}
		if err != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil //nolint:nilerr // see above: a file we cannot stat is not a build failure.
		}
		if !entry.IsDir() {
			if entry.Name() == name {
				found = true
			}
			return nil
		}
		if path == root {
			return nil
		}
		// Depth is measured from root, which every path here is under because
		// the walk started there.
		depth := strings.Count(strings.TrimPrefix(path, root), string(filepath.Separator))
		base := entry.Name()
		if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" ||
			base == "testdata" || depth >= maxDepth {
			return filepath.SkipDir
		}
		return nil
	})
	return found
}
