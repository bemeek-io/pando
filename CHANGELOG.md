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

Pando has not had a release yet. The sections below accumulate until the first tag.

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

[Unreleased]: https://github.com/bemeek-io/pando/commits/main
