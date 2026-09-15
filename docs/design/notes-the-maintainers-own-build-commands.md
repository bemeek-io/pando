# R-094 tier 3: a Makefile is the maintainer's own build commands

Adding [macscout](https://github.com/bemeek-io/macscout) produced a plan that
builds, starts, reports healthy, and serves an empty page.

It is a Go server with a React client. The client is compiled into
`cmd/server/dist` and embedded:

```go
//go:embed all:dist
```

and its Makefile says so:

```make
build:
	cd web && npm ci && npm run build
	touch cmd/server/dist/.gitkeep
	go build -o macscout ./cmd/server

run: build
	./macscout
```

Detection saw `go.mod` at the root, bid tier 4, and nixpacks planned:

```
install: go mod download
build:   go build -o out ./cmd/server
start:   ./out
```

No npm step. `web/package.json` is one directory down, and `BuildpackDetector`
stats `package.json` at the root; `StaticDetector` wants a root `index.html`,
also one directory down. Nothing bid on the client, and there were no
runners-up.

The build then **succeeds**, which is the part that matters. macscout commits
`cmd/server/dist/.gitkeep` precisely so `go:embed all:dist` compiles on a fresh
clone before anyone has run a client build — its Makefile restores the file
after every build for that reason. So the embed finds a directory containing one
empty file, and ships it.

Both images were built to check, rather than argued about:

| plan | `GET /` |
|---|---|
| `go build ./cmd/server` | `404 page not found`, 19 bytes |
| `make build` | the client, `assets/index-DOMOFn3-.js` |

## What was already decided

R-094's ladder had this, ranked above the tier that guessed:

> 3. **CI workflows** — `.github/workflows` contains the maintainer's own build commands.

The tier was right and unimplemented. What it named was too narrow: a Makefile
`build` target is the same artifact as a CI workflow — the build written down by
the person who wrote the app — and macscout has no `.github/workflows` at all.
R-094 tier 3 is amended to say "the maintainer's own build commands" and to name
both. The rank and the meaning are unchanged.

This is not the inference R-021 forbids. R-021's sentence is *"if the repo
declares what it needs, Pando's job is to satisfy that declaration"*, and a
Makefile target is a declaration. What R-021 rules out is going hunting through
imports, which is still not done: the client is found because `web/package.json`
exists, not because a recipe line contains the word `npm`.

## Nixpacks still does the work

R-095 says to wrap a buildpack implementation rather than reimplement
convention-matching, and that did not stop being true. Nixpacks already takes
the flags:

```
nixpacks build <dir> --out <dir> \
  --build-cmd "make build" \
  --start-cmd "./macscout" \
  --pkgs gnumake --pkgs nodejs
```

```
╔════════ Nixpacks v1.41.0 ════════╗
║ setup      │ nodejs, gnumake, go ║
║ install    │ go mod download     ║
║ build      │ make build          ║
║ start      │ ./macscout          ║
╚══════════════════════════════════╝
```

So nothing synthesizes a Dockerfile, nothing chooses a base image, and nothing
learns a toolchain matrix. The plan is still nixpacks', still readable, still
editable before it runs. What changed is which commands it was told to run.

`gnumake` is added because no nixpacks provider installs make — the build
command would otherwise be the first thing to fail. `nodejs` is added when a
`package.json` exists anywhere in the tree, because nixpacks picks one provider
from the root and a Go module's provider has no reason to bring Node.

## Where the parsing lives, and why it is not shared

The Makefile is read in the builder adapter. R-027 forbids an adapter importing
`internal/detect`, and the depguard rule names that package explicitly, so the
two cannot share a parser.

Detection therefore does not parse the Makefile at all. It stats it for a name,
and then reads the **generated plan** to see whether the build step invokes
make — which is what raises the bid from 0.45 to 0.72 and adds the evidence
line. Two Makefile parsers that have to agree is how they stop agreeing; the one
that decides is the builder's, and detection reports what it decided.

## What is deliberately not done

- **Variables are not expanded and `include` is not followed.** A Makefile that
  hides its build behind either is not recognized, and the repository is planned
  by convention exactly as before.
- **A start command is taken only when it is unambiguous.** `run: build` then
  `./macscout` is taken. A recipe that only calls `make` is delegating; one with
  a shell operator — `cd web && npm run dev` — needs a guess about which half is
  the app. Both are left for nixpacks to answer.
- **`start` outranks `run`.** A Makefile with both usually means `run` for
  development, often a file watcher. Deploying somebody's dev server is a bad
  way to discover the preference order was backwards.
