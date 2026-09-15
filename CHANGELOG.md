# Changelog

Notable changes to Pando, written for the person deciding whether to upgrade and what will change
when they do. This file is not a git log; a change that nobody operating an installation would notice
does not belong here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Pando follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). What a version number promises is in
[`docs/releasing.md`](docs/releasing.md); how a release is cut is in
[`CONTRIBUTING.md`](CONTRIBUTING.md#releasing).

Every release section names, in this order: **Security** (including every publicly known vulnerability
fixed in it, with its CVE or GHSA identifier), **Added**, **Changed**, **Deprecated**, **Removed**,
**Fixed**, and **Upgrade notes** for anything requiring an operator action.

<!-- Rename this heading to the version and date when the next release is cut — the release
workflow reads the section matching the tag and refuses to release without one — and open a fresh
Unreleased above it. -->

## [Unreleased]

## [0.2.0] - 2026-09-15

Detection stops asking for things the repository already told it, and starts reading the build
instructions an app carries rather than inferring them. If you deploy anything that builds a client
into a Go binary, or that keeps data on disk, this release changes what Pando proposes for it.

### Security

No new advisories. The six open against `github.com/docker/docker` are unchanged and remain accepted
with their reasoning in [`.github/govulncheck-allowlist.txt`](.github/govulncheck-allowlist.txt):
all six are Moby **daemon** vulnerabilities, and `go list -deps` on the runtime adapter resolves to
`api/…`, `client` and `pkg/stdcopy` with no `daemon/…` or `plugin/…` package in the binary. The
module has no fixed release and will not get one.

- An open redirect in the console's sign-in page. `returnTo` accepted a `next` parameter after
  checking it began with one `/` and not two — but `/\evil.example` passes that and the URL parser
  still reads it as `//evil.example`, because where an authority may begin a backslash and a slash
  mean the same thing. Following a crafted link, signing in on the real hostname with a real
  password, and landing on somebody else's site was a working attack. `next` is now resolved against
  the current document and accepted only when the origins match, which agrees with what the browser
  will do by construction and turns away `javascript:` and `data:` in the same breath.

### Added

- **Pando builds an app the way its repository says to.** Where a repository states its build — a
  `.github/workflows` build job, a `Makefile`, `Taskfile.yml` or `justfile` target, a `Procfile`'s
  web process — the plan runs those commands instead of inferring them from the language. R-094's
  confidence ladder always ranked "the maintainer's own build commands" above convention-matching;
  this implements it.
- **A client that builds into a directory the binary embeds is built first.** Where a bundler config
  names an output directory and a `//go:embed` directive names the same one, the ordering is stated
  by the repository rather than guessed, and the client build runs ahead of the binary's. Read from
  Vite, Astro, Vue CLI, Angular, Next.js (static exports), webpack and SvelteKit, or from an
  `--outDir`-style flag in the build script.
- `WARN_NO_PERSISTENT_VOLUME` now appears for apps built from source. It previously required a trial
  run, which does not happen before an image exists — so an app that kept data on disk and declared
  no volume got no warning at all.

### Changed

- **Adapter interface:** `BuildPlanner.Plan` returns an `*api.PlanDeclaration` alongside the
  generated files, naming what in the repository dictated the plan. Nil means convention-matching
  chose it. Only affects out-of-tree builder adapters, of which there are none; everything ships
  compiled in (R-253).
- Detection reports `ready` rather than `needs_answers` when it has nothing to ask. A bid below the
  confidence threshold used to force `needs_answers` on its own, which became visible — and wrong —
  once the questions below stopped being asked.
- The console builds with TypeScript 6.0.3, and its `tsconfig.json` no longer sets `baseUrl`, which
  TypeScript 6 rejects and 7 removes.

### Fixed

- **Deploying a repository with no Dockerfile asked two questions it could answer itself.** A plain
  Go module was asked for a start command that the generated build plan already contained, and for a
  port that the language's framework default already supplied. Both are gone; the port rides in the
  proposal as an editable default, marked as the guess it is.
- A variable named after a target — `build := ./out` — was read as declaring that target, so the plan
  ran `make build` against something that did not exist.
- Audit log paging stopped on any `limit` above the cap. The API applied the default page size but
  not the ceiling, compared a capped page of 500 against the requested 1000, decided the page was not
  full, and returned no cursor.
- The `cosign verify-blob` command in [`docs/releasing.md`](docs/releasing.md#verifying-a-download)
  rejected every release cut the normal way. It required a certificate identity ending
  `@refs/tags/v`, but a release dispatched from `main` lets the workflow create the tag, so the run —
  and the certificate — belongs to `refs/heads/main`. Anyone following the instructions on v0.1.1
  would have concluded a good release was forged. The documented regex now matches both paths.

### Upgrade notes

Nothing to do. Detection does not re-run on its own (R-022), so an app pinned before this release
keeps the spec it was pinned with. To pick up the new reading for an existing app, re-run detection
from its page and review the proposal as usual.

## [0.1.1] - 2026-09-14

### Security

- Base images, GitHub Actions and the two scanners CI installs are pinned by digest or exact version
  rather than by a mutable tag.
- `containerd/v2` to 2.3.5 (GHSA-7jxh-36q5-gcqv) and `moby/go-archive` to 0.3.0 (GO-2026-6253, a
  crafted tar writing outside the extraction directory).
- The console's `vite` to 8.3.0, with `@vitejs/plugin-react` 6.1.1 alongside it, clearing six
  high-severity dev-server advisories.
- A malformed stored credential hash no longer crashes the sign-in path or verifies against an
  arbitrary password. `argon2.IDKey` panics rather than returning an error on a zero time cost or
  zero parallelism, and an empty key field compared equal to an empty candidate, so a corrupted or
  hand-edited row could take the process down or accept anything. Both are now rejected during
  decoding. Found by fuzzing; covered by `TestR042_AMalformedStoredHashDeniesRatherThanPanics`.
- Session cookies are marked `Secure` behind a TLS-terminating reverse proxy. Pando sees plain HTTP
  in that topology, so it could not tell an encrypted browser connection from an unencrypted one and
  sent the cookie without the attribute; one plaintext request to the hostname put a live session on
  the wire. Set `PANDO_SERVER_EXTERNAL_URL` to the address browsers use. **Upgrade note:** an
  installation behind a proxy should set it — unset keeps the previous behavior, which is correct
  only when Pando serves TLS itself or runs on localhost. (O-19)
- The API server sets `ReadHeaderTimeout` and `IdleTimeout`. Without them a client dribbling header
  bytes held a connection open indefinitely. The proxy's per-app listeners already did this; the API
  server was the one that did not. Found by `gosec`.

### Added

- Security policy, coordinated disclosure process and documented security model
  ([`SECURITY.md`](SECURITY.md)).
- Checksums signed with cosign on every release, and the verification procedure that goes with them
  ([`docs/releasing.md`](docs/releasing.md#verifying-a-download)).
- CodeQL, `gosec`, `govulncheck`, `gitleaks` and OpenSSF Scorecard in continuous integration.
- Fuzz targets over the parsers that see untrusted input, run in continuous integration.
- Issue and pull request templates, a code of conduct, Dependabot, and a reference index of the
  external interfaces ([`docs/reference.md`](docs/reference.md)).

### Fixed

- The Docker image ships with the console in it. `docker compose up -d` built an image whose binary
  had no UI embedded, so it served the API and returned 404 for every console route.


## [0.1.0] - 2026-09-14

The first release. Its notes were generated from the commit log, which is what this file now exists
to replace; see the release page for the artifact list.

[Unreleased]: https://github.com/bemeek-io/pando/compare/v0.2.0...main
[0.2.0]: https://github.com/bemeek-io/pando/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/bemeek-io/pando/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/bemeek-io/pando/releases/tag/v0.1.0
