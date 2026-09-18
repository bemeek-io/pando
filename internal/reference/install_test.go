package reference_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/bemeek-io/pando/internal/reference"
)

// TestInstallInstructionsMatchWhatATagPublishes keeps the install page honest.
//
// The instructions are compiled into the binary, because the server serves this
// document at runtime and there is no release config beside a running
// container. So the release config is read here instead: renaming the tap,
// dropping a package format, adding an architecture or moving the module all
// fail this test rather than leaving a page telling people to install something
// that is no longer published.
//
// Install instructions are the first thing anybody reads and the last thing
// anybody checks — they are correct on the day they are written, and nobody
// notices when they stop being, because the person they are wrong for is not on
// the team.
func TestInstallInstructionsMatchWhatATagPublishes(t *testing.T) {
	doc := reference.Build(nil)
	got := doc.Install

	var release struct {
		Builds []struct {
			Main   string   `yaml:"main"`
			Goos   []string `yaml:"goos"`
			Goarch []string `yaml:"goarch"`
		} `yaml:"builds"`
		Archives []struct {
			Formats      []string `yaml:"formats"`
			NameTemplate string   `yaml:"name_template"`
		} `yaml:"archives"`
		HomebrewCasks []struct {
			Name       string `yaml:"name"`
			Repository struct {
				Owner string `yaml:"owner"`
				Name  string `yaml:"name"`
			} `yaml:"repository"`
		} `yaml:"homebrew_casks"`
		NFPMs []struct {
			Formats []string `yaml:"formats"`
		} `yaml:"nfpms"`
		Release struct {
			GitHub struct {
				Owner string `yaml:"owner"`
				Name  string `yaml:"name"`
			} `yaml:"github"`
		} `yaml:"release"`
	}

	body, err := os.ReadFile("../../.goreleaser.yaml")
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(body, &release))

	// Where releases are published.
	require.NotEmpty(t, release.Release.GitHub.Owner)
	require.Equal(t,
		"https://github.com/"+release.Release.GitHub.Owner+"/"+release.Release.GitHub.Name,
		got.Repo)

	// The `go install` path: the module from go.mod, plus the command the
	// release builds.
	gomod, err := os.ReadFile("../../go.mod")
	require.NoError(t, err)
	module := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(string(gomod), "\n", 2)[0], "module "))
	require.Equal(t, module, strings.TrimSuffix(got.Module, "/cmd/pando"))

	require.Len(t, release.Builds, 1)
	require.Equal(t, "./cmd/pando", release.Builds[0].Main,
		"the documented `go install` path is this build's main package")

	// The tap.
	require.Len(t, release.HomebrewCasks, 1)
	cask := release.HomebrewCasks[0]
	require.Equal(t, cask.Repository.Owner+"/"+strings.TrimPrefix(cask.Repository.Name, "homebrew-")+"/"+cask.Name,
		got.Homebrew)

	// The package formats.
	require.Len(t, release.NFPMs, 1)
	require.ElementsMatch(t, release.NFPMs[0].Formats, got.Packages)

	// Every platform built is a platform offered.
	var platforms []string
	for _, os := range release.Builds[0].Goos {
		for _, arch := range release.Builds[0].Goarch {
			platforms = append(platforms, os+"/"+arch)
		}
	}
	require.ElementsMatch(t, platforms, got.Platforms)

	// The archive's shape, with the template's fields standing in for what a
	// person substitutes.
	require.Len(t, release.Archives, 1)
	require.ElementsMatch(t, []string{"tar.gz"}, release.Archives[0].Formats,
		"the documented archive is a tarball")
	expanded := release.Archives[0].NameTemplate
	for from, to := range map[string]string{
		"{{ .ProjectName }}": "pando",
		"{{ .Version }}":     "<version>",
		"{{ .Os }}":          "<os>",
		"{{ .Arch }}":        "<arch>",
	} {
		expanded = strings.ReplaceAll(expanded, from, to)
	}
	require.Equal(t, expanded+".tar.gz", got.Archive)
}

// The README installs the CLI too, for somebody who is reading the repository
// rather than a running installation. It is prose and stays prose — but the
// three strings in it that a release can invalidate are checked here, so a
// renamed tap or a moved module breaks the build rather than the first thing a
// new user types.
func TestTheREADMEInstallsTheSameCLI(t *testing.T) {
	got := reference.Build(nil).Install

	readme, err := os.ReadFile("../../README.md")
	require.NoError(t, err)
	text := string(readme)

	require.Contains(t, text, "brew install "+got.Homebrew)
	require.Contains(t, text, "go install "+got.Module+"@latest")
	require.Contains(t, text, got.Repo+"/releases")
	require.Contains(t, text, got.InContainer)
}
