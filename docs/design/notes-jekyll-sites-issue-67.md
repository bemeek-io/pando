# Jekyll sites and the directory redirect, issue #67

A GitHub Pages site (`ben-meeker/servicenow-solutions`) deployed, reached `running`, and its
Solutions page would not load in port mode. Two causes, one in the server every static site gets and
one in what was served. This note records the decisions, including each `[P]` default, so the
reasons are here and not only in code comments.

## The directory redirect dropped the port

A request for a directory without its trailing slash gets a 301 from nginx. With nginx's default
`absolute_redirect on` the `Location` is built from the `Host` header *without its port*, so
`/solutions` on `localhost:9001` redirected to `http://localhost/solutions/`, where nothing listens.
Every directory on every static site in port mode (R-160, loopback routing) was affected; subdomain
routing on :80/:443 hid it.

The static server's config now sets `absolute_redirect off`, so the redirect is relative
(`/solutions/`) and the browser keeps the address it used. The config is shared by committed static
sites, sites built to static files, and Jekyll sites, so all three are fixed together.
`TestR160_AStaticSitesDirectoryRedirectKeepsItsPort` runs the real image.

**Not changed: the proxy does not rewrite an upstream `Location`.** The issue asked whether it
should rewrite one naming the app's own host on a different port. It would only paper over a server
that builds redirects wrongly, and R-028 keeps the proxy from rewriting what an app sends. The
server Pando supplies is fixed at the source instead.

## A Jekyll site is built, not served as committed `[P]`

R-094's static tier counts "a known SSG config" as evidence. For Jekyll that evidence means a
build: served as committed, the Markdown pages were either the raw Markdown or, for a directory
holding only `index.md`, a 403. A deploy that looks healthy and serves source is worse than either a
built site or a refusal, and building is what GitHub Pages does with the same repository.

- **Recognized** when there is a `_config.yml` (or `.yaml`) and either a Gemfile that brings in
  Jekyll, or a `theme:`/`remote_theme:` line, or one of `_layouts`, `_includes`, `_posts`, `_sass`,
  `_data`. A Gemfile brings Jekyll in by naming `github-pages` or any `jekyll*` gem (a starter names
  only its theme gem, `jekyll-theme-chirpy`), through a gemspec that depends on `jekyll` (a theme's
  own repository), or by a `Gemfile.lock` that locks it. A `_config.yml` alone is not enough; a
  Gemfile that brings in none of these is a Ruby app's.
- **Detection** lowers the static bid to 0.2 when such a site is at the root, the same step aside a
  root `index.html` beside a package.json build script gets, so the buildpack bid (which plans the
  build) wins without a question.
- **Built** on `ruby:<version>` with `bundle exec jekyll build`, and served by the same nginx image
  and config as every other static site. The Ruby version is `.ruby-version`, then the Gemfile or
  its lockfile, then the default (`3.3.6`, the one Ruby projects already get).
- **A site with no Gemfile** is built with the `github-pages` gem, in a Gemfile the plan carries at
  `.nixpacks/jekyll/Gemfile`. That gem is what GitHub Pages builds such a site with: it pins Jekyll,
  the plugins Pages allows and Pages' defaults, and it includes `jekyll-remote-theme`, which a site
  naming a `remote_theme` needs. **A site whose Gemfile names `github-pages` is built the same
  way**, its Gemfile and lockfile replaced, because that is what GitHub does: it builds a Pages site
  with its current `github-pages` whatever the Gemfile says, and loads only the plugins it allows.
  Resolved from the Gemfile instead, `mmistakes/mm-github-pages-starter` (which also names
  `jekyll-algolia`) got `github-pages` 222 and Jekyll 3.9, which do not run on a current Ruby. Any
  other site with a Gemfile is built with its own.
  The build copies it to `./Gemfile` rather than pointing `BUNDLE_GEMFILE` at it: Jekyll loads the
  `:jekyll_plugins` group, and with it Pages' defaults, only from a `Gemfile` in the directory it
  runs in. Pointed at, the gems were installed and never loaded, and every Markdown page without
  front matter (all of this site's) was copied out as Markdown, because `jekyll-optional-front-matter`
  was off.
- **The repository is named in the plan** as `PAGES_REPO_NWO`, read from the checkout's GitHub
  `origin` when planning. `jekyll-github-metadata`, on for every Pages site, needs it as soon as a
  layout reads `site.github` (the Pages themes all do), and a production build reads it only from
  that variable or the site's config, never from the git remote. GitHub sets it for the sites it
  builds; without it the build stopped at "No repo name found". A source not cloned from GitHub gets
  none, and a site that reads `site.github` then fails with Jekyll's own message naming the fix.
- **`url` and `baseurl` are set to empty** by a second config file the plan carries
  (`.nixpacks/jekyll/_config.pando.yml`), read after the site's. In a production build the
  metadata plugin otherwise fills them with the site's GitHub Pages address, which built here was a
  guess at a project page (`/pages/<owner>/<repo>`); every stylesheet linked through it was answered
  by the `index.html` fallback. Pando serves every app at the root of its own address, so a
  `baseurl` the site sets for GitHub is overridden too.
- **`PAGES_DISABLE_NETWORK=1`** keeps the metadata plugin off the GitHub API. Unauthenticated, that
  is sixty requests an hour per address, and a build that got an answer and one that did not wrote
  different pages from the same commit. Fields only the API supplies (a repository's description,
  for one) are empty.
- **`BUNDLE_WITHOUT=development:test`.** Pando builds the site and does not test it (R-011), and a
  test group can need more than Ruby: `github/choosealicense.com`'s pulls in `rugged`, whose native
  extension needs CMake, and the build failed installing a gem the site never uses.
- **The build needs the network** — gems, and a remote theme — as a Node site's build does.

Checked against the repository in the issue: detection chooses the build with no question, and
`/`, `/solutions` and all thirteen solution pages load in port mode, rendered, with the theme's
stylesheet. Then against five more public sites, each detected, built with the plan detection
produced, and crawled (the home page, its internal links, and every local stylesheet and script)
through a port-mode address:

| Repository | Shape | Built with | Result |
|---|---|---|---|
| `ben-meeker/servicenow-solutions` | Pages, no Gemfile, remote theme | `github-pages` | 3 pages, 5 assets |
| `barryclark/jekyll-now` | Pages, no Gemfile | `github-pages` | 4 pages, 1 asset |
| `mmistakes/mm-github-pages-starter` | Gemfile naming `github-pages`, remote theme | `github-pages` | 12 pages, 5 assets |
| `github/choosealicense.com` | Jekyll 4 Gemfile, test group needing CMake | its Gemfile | 16 pages, 2 assets |
| `cotes2020/chirpy-starter` | Gemfile naming only a theme gem | its Gemfile | 7 pages, 5 assets |
| `daattali/beautiful-jekyll` | theme repository, `gemspec` | its Gemfile | 5 pages, 3 assets |

Each chose the build with no question. `academicpages/academicpages.github.io` was not built this
way: it commits a Dockerfile and a compose file, which R-094 ranks above a static site generator's
config, and detection asks which of the two to use.

Only the repository root is read. A Pages site kept in `docs/` beside a committed `index.html`
there is still served as committed.

## Not decided here

A directory with no `index.html` still answers 403: `try_files $uri $uri/ /index.html` matches the
directory before the fallback. Built Jekyll sites no longer produce such directories, and whether the
fallback should reach them for other sites is a separate question left open.
