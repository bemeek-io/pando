package buildkit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Each toolchain states its output directory somewhere, and each is read from a
// literal rather than by evaluating a config.
func TestEachToolchainsDeclaredOutputIsRead(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
		from  string
	}{
		"vite": {
			files: map[string]string{
				"web/package.json":   `{"scripts":{"build":"vite build"}}`,
				"web/vite.config.ts": `export default { build: { outDir: "../cmd/server/dist" } };`,
			},
			want: "cmd/server/dist", from: "vite.config.ts",
		},
		"astro": {
			files: map[string]string{
				"web/package.json":     `{"scripts":{"build":"astro build"}}`,
				"web/astro.config.mjs": `export default { outDir: "../public" };`,
			},
			want: "public", from: "astro.config.mjs",
		},
		"vue cli": {
			files: map[string]string{
				"web/package.json":  `{"scripts":{"build":"vue-cli-service build"}}`,
				"web/vue.config.js": `module.exports = { outputDir: "../server/static" };`,
			},
			want: "server/static", from: "vue.config.js",
		},
		"angular, the string form": {
			files: map[string]string{
				"web/package.json": `{"scripts":{"build":"ng build"}}`,
				"web/angular.json": `{"projects":{"app":{"architect":{"build":{"options":{"outputPath":"../dist/app"}}}}}}`,
			},
			want: "dist/app", from: "angular.json",
		},
		"angular 17, the object form": {
			files: map[string]string{
				"web/package.json": `{"scripts":{"build":"ng build"}}`,
				"web/angular.json": `{"projects":{"app":{"targets":{"build":{"options":` +
					`{"outputPath":{"base":"../dist/app","browser":"browser"}}}}}}}`,
			},
			want: "dist/app/browser", from: "angular.json",
		},
		"next, exported statically": {
			files: map[string]string{
				"web/package.json":    `{"scripts":{"build":"next build"}}`,
				"web/next.config.mjs": `export default { output: "export" };`,
			},
			want: "web/out", from: "next.config.mjs",
		},
		"next, with a named distDir": {
			files: map[string]string{
				"web/package.json":    `{"scripts":{"build":"next build"}}`,
				"web/next.config.mjs": `export default { output: "export", distDir: "../static" };`,
			},
			want: "static", from: "next.config.mjs",
		},
		"webpack, the resolve idiom": {
			files: map[string]string{
				"web/package.json":      `{"scripts":{"build":"webpack --mode production"}}`,
				"web/webpack.config.js": "module.exports = { output: { path: path.resolve(__dirname, '../cmd/app/assets') } };",
			},
			want: "cmd/app/assets", from: "webpack.config.js",
		},
		"sveltekit with adapter-static": {
			files: map[string]string{
				"web/package.json":     `{"scripts":{"build":"vite build"}}`,
				"web/svelte.config.js": "import adapter from '@sveltejs/adapter-static';\nexport default { kit: { adapter: adapter({ pages: '../build' }) } };",
			},
			want: "build", from: "svelte.config.js",
		},
		"a flag in the build script": {
			files: map[string]string{
				"web/package.json": `{"scripts":{"build":"esbuild src/app.ts --bundle --outdir=../cmd/app/dist"}}`,
			},
			want: "cmd/app/dist", from: "package.json",
		},
		"parcel's dist-dir flag": {
			files: map[string]string{
				"web/package.json": `{"scripts":{"build":"parcel build src/index.html --dist-dir ../public"}}`,
			},
			want: "public", from: "package.json",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := withFiles(t, tc.files)
			projects := clientProjects(root)
			require.Len(t, projects, 1)
			require.Equal(t, tc.want, projects[0].OutDir)
			require.Equal(t, tc.from, projects[0].ConfigFile)
		})
	}
}

// The build script wins, because it is the command that runs.
func TestAFlagInTheBuildScriptBeatsTheConfig(t *testing.T) {
	root := withFiles(t, map[string]string{
		"web/package.json":   `{"scripts":{"build":"vite build --outDir ../from-the-flag"}}`,
		"web/vite.config.ts": `export default { build: { outDir: "../from-the-config" } };`,
	})
	projects := clientProjects(root)
	require.Len(t, projects, 1)
	require.Equal(t, "from-the-flag", projects[0].OutDir)
}

// Each reader takes a literal, and declines a config that is a program.
func TestAComputedOutputDirectoryIsDeclined(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"vite with resolve": {
			"web/package.json":   `{"scripts":{"build":"vite build"}}`,
			"web/vite.config.ts": "import {resolve} from 'node:path';\nexport default { build: { outDir: resolve(__dirname, '../dist') } };",
		},
		"vite with a variable": {
			"web/package.json":   `{"scripts":{"build":"vite build"}}`,
			"web/vite.config.ts": "const out = process.env.OUT_DIR;\nexport default { build: { outDir: out } };",
		},
		"webpack with a variable": {
			"web/package.json":      `{"scripts":{"build":"webpack"}}`,
			"web/webpack.config.js": "module.exports = { output: { path: OUT } };",
		},
		"angular with no build target": {
			"web/package.json": `{"scripts":{"build":"ng build"}}`,
			"web/angular.json": `{"projects":{"app":{"architect":{"test":{"options":{"outputPath":"dist"}}}}}}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Empty(t, clientProjects(withFiles(t, files)))
		})
	}
}

// Next.js without a static export produces a server, and a directory of server
// bundles embedded into a Go binary is not a thing that runs.
func TestNextWithoutAStaticExportIsNotAClientBuild(t *testing.T) {
	root := withFiles(t, map[string]string{
		"web/package.json":    `{"scripts":{"build":"next build"}}`,
		"web/next.config.mjs": `export default { distDir: "../build" };`,
	})
	require.Empty(t, clientProjects(root))
}

// SvelteKit's other adapters produce a server too.
func TestSvelteKitWithoutAdapterStaticIsNotAClientBuild(t *testing.T) {
	root := withFiles(t, map[string]string{
		"web/package.json":     `{"scripts":{"build":"vite build"}}`,
		"web/svelte.config.js": "import adapter from '@sveltejs/adapter-node';\nexport default { kit: { adapter: adapter({ pages: '../build' }) } };",
	})
	require.Empty(t, clientProjects(root))
}

// A path that climbs out of the repository is not somewhere anything here can
// build into.
func TestAnOutputAboveTheRepositoryIsDeclined(t *testing.T) {
	root := withFiles(t, map[string]string{
		"web/package.json":   `{"scripts":{"build":"vite build"}}`,
		"web/vite.config.ts": `export default { build: { outDir: "../../elsewhere" } };`,
	})
	require.Empty(t, clientProjects(root))
}

// End to end: an Angular client embedded by a Go binary, which is the same
// pairing macscout makes with Vite.
func TestR094_AnyToolchainsOutputCanPairWithAnEmbed(t *testing.T) {
	root := withFiles(t, map[string]string{
		"go.mod":          "module example.com/app\n",
		"cmd/api/main.go": "package main\n\n//go:embed all:static\nvar ui embed.FS\n",
		"ui/package.json": `{"scripts":{"build":"ng build"}}`,
		"ui/angular.json": `{"projects":{"app":{"architect":{"build":{"options":{"outputPath":"../cmd/api/static"}}}}}}`,
	})

	d := readDeclaredBuild(root)
	require.Equal(t, rankEmbedded, d.Rank)
	require.Equal(t, "ui/angular.json", d.Source)
	require.Equal(t, "(cd ui && npm ci && npm run build)", d.Before)
	require.Contains(t, d.Why, "cmd/api/static")
}
