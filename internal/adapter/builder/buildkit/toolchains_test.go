package buildkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The toolchain settings Pando supplies when the repository is silent, each of
// which a deploy QA app failed without (issue #55).
func TestR095_ToolchainsTheRepositoryDoesNotNameAreSupplied(t *testing.T) {
	poetry := writeFiles(t, map[string]string{"pyproject.toml": "[tool.poetry]\nname = \"x\"\npackage-mode = false\n"})
	require.Contains(t, toolchainDefaults(poetry), "NIXPACKS_POETRY_VERSION="+defaultPoetryVersion)

	slim := writeFiles(t, map[string]string{"composer.json": "{}"})
	require.NoError(t, os.MkdirAll(filepath.Join(slim, "public"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(slim, "public", "index.php"), []byte("<?php"), 0o644))
	require.Contains(t, toolchainDefaults(slim), "NIXPACKS_PHP_ROOT_DIR=/app/public")

	root := writeFiles(t, map[string]string{"composer.json": "{}", "index.php": "<?php"})
	require.NotContains(t, toolchainDefaults(root), "NIXPACKS_PHP_ROOT_DIR=/app/public",
		"a front controller at the root is served from the root")
}

func TestR095_ARubyProjectsVersionComesFromItsGemfile(t *testing.T) {
	dir := writeFiles(t, map[string]string{"Gemfile": "source 'https://rubygems.org'\nruby '3.2.4'\ngem 'sinatra'\n"})
	ensureRubyVersion(dir)
	body, err := os.ReadFile(filepath.Join(dir, ".ruby-version"))
	require.NoError(t, err)
	require.Equal(t, "3.2.4\n", string(body))

	silent := writeFiles(t, map[string]string{"Gemfile": "gem 'sinatra'\n"})
	ensureRubyVersion(silent)
	body, err = os.ReadFile(filepath.Join(silent, ".ruby-version"))
	require.NoError(t, err)
	require.Equal(t, defaultRubyVersion+"\n", string(body))

	kept := writeFiles(t, map[string]string{"Gemfile": "ruby '3.1.0'\n", ".ruby-version": "3.3.0\n"})
	ensureRubyVersion(kept)
	body, err = os.ReadFile(filepath.Join(kept, ".ruby-version"))
	require.NoError(t, err)
	require.Equal(t, "3.3.0\n", string(body), "one the repository has is left alone")
}

func TestR095_ADotnetProjectIsBuiltOnTheSDKItTargets(t *testing.T) {
	b, ok := readDotnetBuild(writeFiles(t, map[string]string{
		"DotnetMinimal.csproj": `<Project Sdk="Microsoft.NET.Sdk.Web"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`,
	}))
	require.True(t, ok)
	require.Equal(t, dotnetBuild{Project: "DotnetMinimal.csproj", Assembly: "DotnetMinimal", Version: "8.0"}, b)

	_, ok = readDotnetBuild(writeFiles(t, map[string]string{
		"A.csproj": "<TargetFramework>net8.0</TargetFramework>", "B.csproj": "<TargetFramework>net8.0</TargetFramework>",
	}))
	require.False(t, ok, "two projects are a solution whose entry point is not guessed")
}

// A Mix project that is not Phoenix has no phx.server task (issue #55).
func TestR104_AnElixirAppThatIsNotPhoenixIsStartedWithMixRun(t *testing.T) {
	plug := writeFiles(t, map[string]string{"mix.exs": "defp deps, do: [{:bandit, \"~> 1.5\"}]"})
	require.Equal(t, "mix run --no-halt", elixirAppStart(plug))

	phoenix := writeFiles(t, map[string]string{"mix.exs": "defp deps, do: [{:phoenix, \"~> 1.7\"}]"})
	require.Empty(t, elixirAppStart(phoenix), "nixpacks' phx.server is right for Phoenix")
}

// rackup binds 127.0.0.1 unless told otherwise (issue #55).
func TestR104_ARackAppListensWhereItCanBeReached(t *testing.T) {
	require.Equal(t, "bundle exec rackup -o 0.0.0.0 -p ${PORT:-3000}",
		rackAppStart(writeFiles(t, map[string]string{"config.ru": "run App", "Gemfile": ""})))
	require.Empty(t, rackAppStart(writeFiles(t, map[string]string{"Gemfile": ""})))
}

// A Procfile beside a committed program is run as it is; nixpacks had nothing
// to recognize and refused it (issue #55).
func TestR094_ACommittedProgramIsRunAsItIs(t *testing.T) {
	dir := writeFiles(t, map[string]string{"server": "\x7fELF", "Procfile": "web: ./server --port $PORT\n"})
	require.Equal(t, "./server --port $PORT", committedProgram(dir, "./server --port $PORT"))
	require.Empty(t, committedProgram(dir, "./missing"), "only a program that is there")
	require.Empty(t, committedProgram(dir, "python app.py"), "only a program in the repository")
	require.Empty(t, committedProgram(dir, "./../etc/passwd"))

	name, err := writeCommittedProgramPlan(dir, "./server --port $PORT")
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	require.Contains(t, string(body), `CMD ["./server --port $PORT"]`)
}

// A FastAPI or Flask module leaves serving the app to a server, and `python
// main.py` imported it and exited 0 (issue #55).
func TestR104_APythonWebAppIsStartedUnderItsServer(t *testing.T) {
	fastapi := writeFiles(t, map[string]string{
		"main.py":          "from fastapi import FastAPI\napp = FastAPI()\n",
		"requirements.txt": "fastapi\nuvicorn\n",
	})
	require.Equal(t, "uvicorn main:app --host 0.0.0.0 --port ${PORT:-8000}", pythonAppStart(fastapi))

	selfStarting := writeFiles(t, map[string]string{
		"app.py":           "from flask import Flask\napp = Flask(__name__)\nif __name__ == '__main__':\n    app.run()\n",
		"requirements.txt": "flask\ngunicorn\n",
	})
	require.Empty(t, pythonAppStart(selfStarting), "an app that starts itself is left to")

	noServer := writeFiles(t, map[string]string{
		"main.py": "from fastapi import FastAPI\napi = FastAPI()\n", "requirements.txt": "fastapi\n",
	})
	require.Empty(t, pythonAppStart(noServer), "a server the app does not install is not named")
}
