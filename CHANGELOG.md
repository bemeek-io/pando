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

## [Unreleased]

Changes since v0.1.0. Rename this heading to the version and date when the next release is cut — the
release workflow reads the section matching the tag and refuses to release without one.

### Fixed

- The Docker image ships with the console in it. `docker compose up -d` built an image whose binary
  had no UI embedded, so it served the API and returned 404 for every console route.

### Security

- Base images, GitHub Actions and the two scanners CI installs are pinned by digest or exact version
  rather than by a mutable tag.
- `containerd/v2` to 2.3.5 (GHSA-7jxh-36q5-gcqv) and `moby/go-archive` to 0.3.0 (GO-2026-6253, a
  crafted tar writing outside the extraction directory).
- The console's `vite` to 8.3.0, with `@vitejs/plugin-react` 6.1.1 alongside it, clearing six
  high-severity dev-server advisories.

### Security

- A malformed stored credential hash no longer crashes the sign-in path or verifies against an
  arbitrary password. `argon2.IDKey` panics rather than returning an error on a zero time cost or
  zero parallelism, and an empty key field compared equal to an empty candidate, so a corrupted or
  hand-edited row could take the process down or accept anything. Both are now rejected during
  decoding. Found by fuzzing; covered by `TestR042_AMalformedStoredHashDeniesRatherThanPanics`.
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

### Open

- **O-19**: the session cookie is marked `Secure` only when Pando terminates TLS itself, so behind a
  TLS-terminating reverse proxy — the topology `SECURITY.md` describes — it is sent without the
  attribute. Needs a decision about which forwarded-protocol signal Pando trusts. See
  [`docs/plan/open-decisions.md`](docs/plan/open-decisions.md).

## [0.1.0] - 2026-09-14

The first release. Its notes were generated from the commit log, which is what this file now exists
to replace; see the release page for the artifact list.

[Unreleased]: https://github.com/bemeek-io/pando/compare/v0.1.0...main
[0.1.0]: https://github.com/bemeek-io/pando/releases/tag/v0.1.0
