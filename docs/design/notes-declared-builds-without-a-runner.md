# When nothing names the commands

[The Makefile work](notes-the-maintainers-own-build-commands.md) fixed macscout
by reading the file that said how it builds. The obvious next question is what
happens to the same repository with that file deleted, and the answer turned out
not to be "nothing".

## macscout still says it, twice

```
web/vite.config.ts     build: { outDir: "../cmd/server/dist" }
cmd/server/main.go     //go:embed all:dist
```

The first is the client saying where it builds to. The second is the Go build
saying it needs that directory populated. Neither file mentions the other, and
neither was written with deployment in mind — but together they say the client
has to be built before the binary that carries it.

That is a fact read out of two files. It is not the inference R-021 forbids,
which is going hunting through imports to guess at a topology nobody stated.
Both halves here are statements, in files whose whole purpose is to state them.

It earns its place the same way R-203's persistence warning does: by how it
fails. A Go module that embeds a directory compiles whether or not anything
filled it, because `all:dist` matches the committed placeholder that exists so a
fresh clone builds at all. So the image builds, the container starts, the health
check passes, and the app serves an empty page.

## The ladder now

R-094 ranks evidence, and everything the builder reads sits on it:

| rung | source | what it knows | bid |
|---|---|---|---|
| tier 3 | `.github/workflows` | the commands that run on every push | 0.80 |
| tier 3 | `Makefile`, `Taskfile.yml`, `justfile` | the commands somebody wrote down | 0.72 |
| tier 2 | `Procfile` | how to run it, nothing about building | 0.62 |
| tier 3b | an embed and a client output naming one directory | an ordering, and no commands | 0.58 |
| tier 4 | ecosystem manifests | nixpacks reads the repository and infers | 0.45 |

A workflow outranks a Makefile because it is load-bearing: a Makefile target is
what somebody wrote down, while a workflow step is what has been observed to
work by every green check on the repository. When the two disagree, the workflow
is the one that is true.

The embed pairing ranks below everything that names a command because it says
less. It knows what must happen first and not what it happens before — which is
why it needs a second planning pass.

## Two passes, and why

Nixpacks plans the Go build correctly on its own. The missing part is running
npm first, and "first" is only expressible once you know what it precedes.

So: plan once, read the chosen build command back out of the generated
Dockerfile, plan again with the client build in front of it.

```
--build-cmd (cd web && npm ci && npm run build) && go build -o out ./cmd/server
```

Two subprocesses instead of one. That is the honest cost of not reimplementing
the thing R-095 says to wrap — the alternative to asking nixpacks what it would
have chosen is being nixpacks.

The parentheses are load-bearing and were learned the hard way. Without them the
`cd` outlives the client build, `go build ./cmd/server` runs from `web/`, and the
image fails with `stat /app/web/cmd/server: directory not found`. The unit tests
could not have caught it; building the image did, on the first try.

## What declining looks like

Every source refuses rather than guesses, and a refusal costs nothing: the
repository falls through to the next rung, which is where it already was.

- **Variables and quotes.** A command that needs escaping to survive the trip
  into a Dockerfile is one where a mistake is silent, so `go build -ldflags "-X
  main.v=$VERSION"` is declined by every source. A Procfile whose only web
  process is `./app --port $PORT` declares nothing usable.
- **Matrices and reusable workflows.** A `strategy.matrix` builds several things
  and a job-level `uses:` is another file's worth of steps. Neither is a list of
  commands.
- **Assumed output directories.** A client whose bundler config does not say
  where it writes is not paired with an embed, even when the conventional path
  would have matched. A coincidence is not a declaration.
- **Wildcard embeds.** `//go:embed templates/*.html` names a set of files, not a
  directory something builds into.
- **Delegation.** A run target whose every line is `make something` is pointing
  at a target this cannot see.

## The interface changed

`BuildPlanner.Plan` now returns an `*api.PlanDeclaration` alongside the files:
which source dictated the plan, why, and what a detector should bid.

The builder knows this and detection cannot derive it — R-027's depguard rule
forbids an adapter importing `internal/detect`, which is what stops the two
growing separate copies of the same reading that then disagree. Before this,
detection re-derived one case by looking for `make` in the plan's RUN lines, and
[got it wrong](notes-the-maintainers-own-build-commands.md) by answering from the
first RUN, which in a nixpacks plan is always `nix-env`.

## Still not done

`.github/workflows` is read only when one job is unambiguously the build. A
repository that builds in a matrix, or through a composite action, or with the
version in a `$VERSION`, is planned by convention as it was before — which is a
large fraction of real workflows.

Only Vite's `outDir` is read for a client's output. Next.js, Create React App,
webpack and Angular all declare theirs somewhere, and none of them is read yet.
