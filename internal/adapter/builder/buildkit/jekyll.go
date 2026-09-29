package buildkit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/trypando/pando/internal/errs"
)

// A Jekyll site.
//
// GitHub Pages builds a repository with Jekyll before it serves it, so a Pages
// site's repository is the source for a site: Markdown pages, layouts, and a
// _config.yml that may name a theme fetched at build time. Served as committed
// it deployed, reached running, and answered every page written in Markdown
// with a 403 or the raw Markdown (issue #67). R-094 counts a known static site
// generator's config as the static tier's evidence; what that evidence means
// is a build first.
//
// Planned here rather than by nixpacks, which reads the Gemfile as a Ruby app
// and asks what starts it. The site is built on the official Ruby image and
// served by the same nginx and config every other static site gets.

// jekyllGemfile is the Gemfile a site that has none is built with.
//
// GitHub Pages builds such a site with the github-pages gem, which pins Jekyll,
// its plugins and GitHub's defaults. Building with the same gem is building the
// site the way its author saw it, and it carries jekyll-remote-theme, which a
// Pages site naming a remote_theme needs.
const jekyllGemfile = `source "https://rubygems.org"
gem "github-pages", group: :jekyll_plugins
`

// jekyllGemfilePath is where that Gemfile is written: under .nixpacks/ so the
// plan carries it, and in a dot-directory so Jekyll leaves it out of the site.
const jekyllGemfilePath = ".nixpacks/jekyll/Gemfile"

// jekyllMarkers are the directories only a Jekyll site has.
var jekyllMarkers = []string{"_layouts", "_includes", "_posts", "_sass", "_data"}

var (
	jekyllTheme = regexp.MustCompile(`(?m)^\s*(remote_)?theme\s*:`)
	jekyllGem   = regexp.MustCompile(`(?m)^\s*gem\s+['"](jekyll|github-pages)['"]`)
)

// jekyllSite is what reading the repository said about a Jekyll site.
type jekyllSite struct {
	// OwnGemfile is true when the site's Gemfile names Jekyll. False means the
	// site has no Gemfile and is built with jekyllGemfile.
	OwnGemfile bool
	Ruby       string
}

// readJekyllSite reports whether the repository is a Jekyll site: a _config.yml,
// and either a Gemfile naming Jekyll or something else only a Jekyll site has —
// a theme, or one of its underscore directories. A _config.yml alone is not
// enough; other tools use the name. A Gemfile that does not name Jekyll is a
// Ruby app's, whatever else is beside it.
func readJekyllSite(contextDir string) (jekyllSite, bool) {
	var config []byte
	for _, name := range []string{"_config.yml", "_config.yaml"} {
		if body, err := os.ReadFile(filepath.Join(contextDir, name)); err == nil {
			config = body
			break
		}
	}
	if config == nil {
		return jekyllSite{}, false
	}

	if gemfile, err := os.ReadFile(filepath.Join(contextDir, "Gemfile")); err == nil {
		if !jekyllGem.Match(gemfile) {
			return jekyllSite{}, false
		}
		return jekyllSite{OwnGemfile: true, Ruby: jekyllRuby(contextDir)}, true
	}

	marked := jekyllTheme.Match(config)
	for _, dir := range jekyllMarkers {
		if info, err := os.Stat(filepath.Join(contextDir, dir)); err == nil && info.IsDir() {
			marked = true
		}
	}
	if !marked {
		return jekyllSite{}, false
	}
	return jekyllSite{Ruby: defaultRubyVersion}, true
}

// jekyllRuby is the Ruby a site with its own Gemfile is built on: what
// .ruby-version says, or what the Gemfile or its lockfile says, or the default.
func jekyllRuby(contextDir string) string {
	version := gemfileRubyVersion(contextDir)
	if body, err := os.ReadFile(filepath.Join(contextDir, ".ruby-version")); err == nil {
		version = strings.TrimPrefix(strings.TrimSpace(string(body)), "ruby-")
	}
	if version == "" || !safeVersion(version) {
		return defaultRubyVersion
	}
	return version
}

// writeJekyllPlan writes a plan that builds the site and serves what Jekyll
// writes.
//
// Into .nixpacks/, beside where a nixpacks plan goes, so detection collects it
// and the build replays it as it would any other plan (R-020).
func writeJekyllPlan(contextDir string, site jekyllSite) (string, error) {
	env := "ENV JEKYLL_ENV=production"
	if !site.OwnGemfile {
		full := filepath.Join(contextDir, filepath.FromSlash(jekyllGemfilePath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", errs.Wrap(errs.BuildFailed, "Could not prepare the build.", err)
		}
		// G306: a generated build input, read by the rootless builder as a
		// different user. It holds no secret.
		if err := os.WriteFile(full, []byte(jekyllGemfile), 0o644); err != nil { //nolint:gosec
			return "", errs.Wrap(errs.BuildFailed, "Could not prepare the build.", err)
		}
		env += " BUNDLE_GEMFILE=/site/" + jekyllGemfilePath
	}

	name := filepath.Join(".nixpacks", "Dockerfile")
	// The destination is outside the source, so nothing Jekyll writes can be
	// read back as a page on the next build.
	content := fmt.Sprintf(`# A Jekyll site, built with Jekyll and served by nginx.
FROM ruby:%s AS build
WORKDIR /site
COPY . .
%s
RUN bundle install
RUN bundle exec jekyll build --destination /tmp/_site

FROM %s
COPY --from=build /tmp/_site/ /usr/share/nginx/html/
RUN printf %s > /etc/nginx/conf.d/default.conf
EXPOSE 80
ENTRYPOINT ["/bin/sh", "-c"]
CMD ["exec nginx -g 'daemon off;'"]
`, site.Ruby, env, staticServerImage, printfFormat(staticConfig))

	full := filepath.Join(contextDir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", errs.Wrap(errs.BuildFailed, "Could not prepare the build.", err)
	}
	// G306: as above.
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil { //nolint:gosec
		return "", errs.Wrap(errs.BuildFailed, "Could not prepare the build.", err)
	}
	return name, nil
}
