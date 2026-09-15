package buildkit

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Where a client build says it writes to.
//
// Each reader here answers one question — "does this project state its output
// directory, and where" — for one toolchain, and every one of them reads a
// literal. None evaluates a config, because a bundler config is a JavaScript
// program and running somebody's repository to find out where it builds is not
// a thing this gets to do.
//
// A reader that cannot find a literal declines. The cost of declining is that
// the repository is planned by convention, which is where it already was; the
// cost of guessing is an image that builds and ships the wrong bundle.
//
// Ordered: the build script first, because a flag in the command that actually
// runs beats a config file that may not be the one it reads.

// outDirReader finds a declared output directory for a client project.
//
// dir is relative to the repository root. The returned path is relative to dir,
// as the tool itself would read it, and the file is where it was found.
type outDirReader func(root, dir string, scripts map[string]string) (file string, outDir string)

// Ordered. The build script comes first because a flag in the command that runs
// beats a config file that may not be the one it reads.
var outDirReaders = []outDirReader{
	readScriptOutDir,
	readViteOutDir,
	readAstroOutDir,
	readVueOutDir,
	readAngularOutDir,
	readNextOutDir,
	readWebpackOutDir,
	readSvelteKitOutDir,
}

// declaredOutDir asks each reader in turn.
func declaredOutDir(root, dir string, scripts map[string]string) (string, string) {
	for _, read := range outDirReaders {
		file, out := read(root, dir, scripts)
		if out == "" {
			continue
		}
		out = strings.TrimSpace(out)
		// A path that climbs out of the repository is not something anything
		// here can build into.
		resolved := path.Clean(path.Join(dir, out))
		if resolved == "" || resolved == "." || strings.HasPrefix(resolved, "..") {
			continue
		}
		return file, resolved
	}
	return "", ""
}

// readFileIn reads one of several candidate files from a client directory.
func readFileIn(root, dir string, names ...string) (string, []byte) {
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path.Join(dir, name))))
		if err == nil {
			return name, body
		}
	}
	return "", nil
}

// --- the build script -------------------------------------------------------

// outFlagPattern matches the flags a bundler takes for its output directory,
// in either `--flag value` or `--flag=value` form.
//
// This is the most literal declaration there is: it is in the command that
// runs, so it beats whatever a config file says — and it is the only thing that
// catches the tools whose output is a flag and nothing else, like esbuild and
// Parcel.
// Case-insensitive because the tools disagree: Vite spells it --outDir,
// esbuild --outdir, Parcel --dist-dir. Only the flag name folds; the captured
// path has no letters to fold.
var outFlagPattern = regexp.MustCompile(
	`(?i)--(?:out-?dir|dist-dir|output-path|out-file)(?:=|\s+)([^\s"';|&]+)`)

func readScriptOutDir(_ string, _ string, scripts map[string]string) (string, string) {
	m := outFlagPattern.FindStringSubmatch(scripts["build"])
	if m == nil {
		return "", ""
	}
	return "package.json", m[1]
}

// --- Vite, Astro, Vue: a literal key in a config ----------------------------

var viteConfigNames = []string{
	"vite.config.ts", "vite.config.js", "vite.config.mts", "vite.config.mjs",
	"vite.config.cts", "vite.config.cjs",
}

// viteOutDirPattern matches `outDir: "x"`.
//
// A literal only. `outDir: resolve(__dirname, '../x')` is a program, and this
// declines rather than pretending to evaluate it.
var viteOutDirPattern = regexp.MustCompile(`outDir\s*:\s*["']([^"']+)["']`)

func readViteOutDir(root, dir string, _ map[string]string) (string, string) {
	name, body := readFileIn(root, dir, viteConfigNames...)
	if body == nil {
		return "", ""
	}
	return name, submatch(viteOutDirPattern, body)
}

var astroConfigNames = []string{
	"astro.config.mjs", "astro.config.ts", "astro.config.js", "astro.config.cjs",
}

func readAstroOutDir(root, dir string, _ map[string]string) (string, string) {
	name, body := readFileIn(root, dir, astroConfigNames...)
	if body == nil {
		return "", ""
	}
	// Astro spells it the same way Vite does, which is not a coincidence.
	return name, submatch(viteOutDirPattern, body)
}

// vueOutputDirPattern matches Vue CLI's `outputDir: "x"`.
var vueOutputDirPattern = regexp.MustCompile(`outputDir\s*:\s*["']([^"']+)["']`)

func readVueOutDir(root, dir string, _ map[string]string) (string, string) {
	name, body := readFileIn(root, dir, "vue.config.js", "vue.config.ts", "vue.config.mjs")
	if body == nil {
		return "", ""
	}
	return name, submatch(vueOutputDirPattern, body)
}

// --- Angular: real JSON, and the easiest of the lot -------------------------

// readAngularOutDir reads outputPath from angular.json.
//
// The one config here that is data rather than a program, so it is parsed
// rather than pattern-matched. Angular 17 and later allow an object form
// (`{ base, browser }`); both are handled, because a workspace that upgraded is
// not a workspace that stopped declaring.
func readAngularOutDir(root, dir string, _ map[string]string) (string, string) {
	_, body := readFileIn(root, dir, "angular.json")
	if body == nil {
		return "", ""
	}

	var workspace struct {
		Projects map[string]struct {
			Architect map[string]struct {
				Options struct {
					OutputPath json.RawMessage `json:"outputPath"`
				} `json:"options"`
			} `json:"architect"`
			// Angular 17 renamed architect to targets and kept both readable.
			Targets map[string]struct {
				Options struct {
					OutputPath json.RawMessage `json:"outputPath"`
				} `json:"options"`
			} `json:"targets"`
		} `json:"projects"`
	}
	if json.Unmarshal(body, &workspace) != nil {
		return "", ""
	}

	// Sorted, so a workspace with several projects reads the same way twice.
	for _, project := range sortedKeys(workspace.Projects) {
		p := workspace.Projects[project]
		for _, targets := range []map[string]struct {
			Options struct {
				OutputPath json.RawMessage `json:"outputPath"`
			} `json:"options"`
		}{p.Architect, p.Targets} {
			raw := targets["build"].Options.OutputPath
			if len(raw) == 0 {
				continue
			}
			if out := angularOutputPath(raw); out != "" {
				return "angular.json", out
			}
		}
	}
	return "", ""
}

// angularOutputPath reads outputPath in either the string or the object form.
func angularOutputPath(raw json.RawMessage) string {
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return asString
	}
	var asObject struct {
		Base    string `json:"base"`
		Browser string `json:"browser"`
	}
	if json.Unmarshal(raw, &asObject) == nil && asObject.Base != "" {
		// `browser` is a subdirectory of `base`, and is where the files a
		// server would serve actually land.
		return path.Join(asObject.Base, asObject.Browser)
	}
	return ""
}

// --- Next.js ----------------------------------------------------------------

var nextConfigNames = []string{
	"next.config.js", "next.config.mjs", "next.config.ts", "next.config.cjs",
}

var nextDistDirPattern = regexp.MustCompile(`distDir\s*:\s*["']([^"']+)["']`)

// nextExportPattern matches `output: "export"`, which is the setting that makes
// Next.js emit something a Go binary could embed at all.
var nextExportPattern = regexp.MustCompile(`output\s*:\s*["']export["']`)

// readNextOutDir reads where Next.js writes a static export.
//
// Only a static export counts. Next's ordinary build produces a server, and a
// directory of server bundles embedded into a Go binary is not a thing that
// runs — so `output: 'export'` is what turns this from a guess into a
// declaration, and `out` is where that export lands.
func readNextOutDir(root, dir string, _ map[string]string) (string, string) {
	name, body := readFileIn(root, dir, nextConfigNames...)
	if body == nil {
		return "", ""
	}
	if !nextExportPattern.Match(body) {
		return "", ""
	}
	if out := submatch(nextDistDirPattern, body); out != "" {
		return name, out
	}
	// `output: 'export'` writes to `out`, and distDir names the build
	// directory rather than the export. Stated by the config either way.
	return name, "out"
}

// --- webpack ----------------------------------------------------------------

// webpackOutputPattern matches the two shapes a webpack `output.path` takes:
// `path.resolve(__dirname, 'x')` and a bare literal.
//
// The resolve form is a program, and this reads exactly one idiom of it — the
// one every webpack config in existence uses. Anything else, including a
// variable or a second argument that is not a literal, declines.
var webpackOutputPattern = regexp.MustCompile(
	`path\s*:\s*(?:path\.(?:resolve|join)\(\s*__dirname\s*,\s*)?["']([^"']+)["']`)

func readWebpackOutDir(root, dir string, _ map[string]string) (string, string) {
	name, body := readFileIn(root, dir,
		"webpack.config.js", "webpack.config.mjs", "webpack.config.cjs", "webpack.config.ts",
		"webpack.prod.js", "webpack.prod.config.js")
	if body == nil {
		return "", ""
	}
	return name, submatch(webpackOutputPattern, body)
}

// --- SvelteKit --------------------------------------------------------------

// sveltePagesPattern matches adapter-static's `pages: 'x'`.
//
// adapter-static is what makes a SvelteKit app a directory of files; the other
// adapters produce a server, which is not something to embed.
var sveltePagesPattern = regexp.MustCompile(`pages\s*:\s*["']([^"']+)["']`)

func readSvelteKitOutDir(root, dir string, _ map[string]string) (string, string) {
	name, body := readFileIn(root, dir, "svelte.config.js", "svelte.config.ts", "svelte.config.mjs")
	if body == nil {
		return "", ""
	}
	if !strings.Contains(string(body), "adapter-static") {
		return "", ""
	}
	return name, submatch(sveltePagesPattern, body)
}

// --- shared -----------------------------------------------------------------

func submatch(re *regexp.Regexp, body []byte) string {
	m := re.FindSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}
