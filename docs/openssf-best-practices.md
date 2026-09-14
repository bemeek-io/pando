# OpenSSF Best Practices — answer sheet

Every criterion on the [passing badge](https://www.bestpractices.dev/en/projects/14626), what Pando's
answer is, and the justification text to paste into the form. Keep this updated when the answers
change; the badge asks for a re-attestation periodically and this is the record of what was claimed.

**Status key**

| | Meaning |
|---|---|
| ✅ | Met. Evidence in the repository. Paste the justification and mark Met. |
| 👤 | Met, but it is your attestation to make — nobody else can answer it for you. |
| ⏳ | Blocked on the first tagged release. The machinery exists; the artifact does not yet. |

URLs below assume `https://github.com/bemeek-io/pando`, written as `<repo>`.

---

## Basics

| Criterion | Status | Justification to paste |
|---|---|---|
| `description_good` | ✅ | The README opens with "What is Pando?", describing it as a self-hosted deployment platform, what it runs on, and the problem it solves: `<repo>#what-is-pando` |
| `interact` | ✅ | The README has a "Reporting a problem" section linking issues, security advisories and discussions, and a "Contributing" section linking CONTRIBUTING.md: `<repo>#reporting-a-problem` |
| `contribution` | ✅ | Already met. `<repo>/blob/main/CONTRIBUTING.md` |
| `contribution_requirements` | ✅ | CONTRIBUTING.md states what a change needs before it merges: an acceptance test named for the requirement it satisfies, `make check` passing (build, vet, golangci-lint including gosec and the adapter import rule, unit tests), a CHANGELOG entry where an operator would notice, and design-system compliance for console changes. The same list is the pull request template. `<repo>/blob/main/CONTRIBUTING.md#pull-requests` |
| `floss_license` | ✅ | Already met. |
| `floss_license_osi` | ✅ | Already met. |
| `license_location` | ✅ | Already met. |
| `documentation_basics` | ✅ | Already met. |
| `documentation_interface` | ✅ | `docs/reference.md` documents the external interfaces: the HTTP API with its authentication, error envelope, ID and pagination conventions; what a deployed app receives (the signed assertion, headers, environment); every configuration variable; and the stability guarantees. It links the full API reference in `docs/design/04-api.md`. `<repo>/blob/main/docs/reference.md` |
| `sites_https` | ✅ | Already met. |
| `discussion` | ✅ | Already met. |
| `english` | ✅ | All documentation, code, issue templates and pull request templates are in English, and issues and pull requests are accepted and answered in English. |
| `maintained` | 👤 | Your call. The honest version: "The project is in active development toward a 1.0 release; see the commit history and `docs/plan/` for the phase schedule." |

---

## Change control

| Criterion | Status | Justification to paste |
|---|---|---|
| `repo_public` | ✅ | Already met. |
| `repo_track` | ✅ | Already met. |
| `repo_interim` | ✅ | Development happens in the open on `main`, with every interim commit public. CI runs on every pull request and every push to `main`. There are no release-only drops. `<repo>/commits/main` |
| `repo_distributed` | ✅ | Already met. |
| `version_unique` | ⏳ | `docs/releasing.md` defines the scheme: every release is a signed git tag `vMAJOR.MINOR.PATCH`, and `pando version` prints the version, commit and build date, stamped at build time. Container images are tagged by version and addressable by digest. A build that is not from a tag reports `dev` and never claims a version number. `<repo>/blob/main/docs/releasing.md#what-a-version-identifies` — **mark Met once `v0.1.0` is tagged.** |
| `version_semver` | ✅ | Pando follows Semantic Versioning 2.0.0. `docs/releasing.md` states what each part promises an operator — MAJOR for a breaking upgrade, MINOR for additions, PATCH for fixes — and that before 1.0 the MINOR position carries breaking changes. `<repo>/blob/main/docs/releasing.md#version-numbers` |
| `version_tags` | ⏳ | Every release is identified by an annotated, signed git tag (`git tag -s vX.Y.Z`), and pushing a `v*` tag is what triggers the release workflow. `<repo>/blob/main/docs/releasing.md#cutting-the-release` — **mark Met once the first tag exists.** |
| `release_notes` | ⏳ | `CHANGELOG.md` follows Keep a Changelog, written for the person deciding whether to upgrade rather than as a commit log. The release workflow extracts the section for the version being tagged and uses it as the GitHub release body, and **fails the release** if `CHANGELOG.md` has no section for that version. `<repo>/blob/main/CHANGELOG.md` — **mark Met with the first release's URL.** |
| `release_notes_vulns` | ✅ | Mark **N/A** today: there are no releases and no publicly known vulnerabilities in Pando. The policy is in place for when there are — every release section's **Security** subsection must name each publicly known vulnerability fixed in it with its CVE or GHSA identifier, and `docs/releasing.md` makes that a precondition for tagging. `<repo>/blob/main/docs/releasing.md#security-releases` |

---

## Reporting

| Criterion | Status | Justification to paste |
|---|---|---|
| `report_process` | ✅ | GitHub Issues, with templates for bug reports and enhancements that ask for the version, install method, reproduction and logs. The chooser routes security reports to the private advisory form instead. `<repo>/issues/new/choose` |
| `report_tracker` | ✅ | GitHub Issues. `<repo>/issues` |
| `report_responses` | 👤 | Yours to attest, based on the actual issue history. |
| `enhancement_responses` | 👤 | Yours to attest. |
| `report_archive` | ✅ | Every issue and its responses are publicly readable, searchable and addressable by URL. `<repo>/issues?q=is%3Aissue` |
| `vulnerability_report_process` | ✅ | `SECURITY.md` publishes the process: report privately through GitHub's advisory form, with an email fallback. It states what to include, the scope, and the response targets. GitHub surfaces it at the URL below. `<repo>/security/policy` |
| `vulnerability_report_private` | ✅ | Private vulnerability reporting is through GitHub Security Advisories, which keeps the report private between reporter and maintainers until an advisory is published. It is the only reporting channel, which `SECURITY.md` states plainly. `<repo>/security/advisories/new` — **enable "Private vulnerability reporting" in Settings → Code security first; see the checklist below.** |
| `vulnerability_report_response` | ✅ | Mark **N/A** if no vulnerability report has been received in the last six months. The published commitment is acknowledgment within 3 business days and an initial assessment within 14 days. `<repo>/blob/main/SECURITY.md#what-to-expect` |

---

## Quality

| Criterion | Status | Justification to paste |
|---|---|---|
| `build` | ✅ | Already met. |
| `build_common_tools` | ✅ | Already met. |
| `build_floss_tools` | ✅ | The build uses only FLOSS tools: the Go toolchain (BSD-3), GNU Make, Node and npm for the console, and Docker/BuildKit (Apache-2.0) for the container image. No proprietary tool is required at any step. `<repo>/blob/main/Makefile` |
| `test` | ✅ | Go's standard testing package, run by `make test` (unit, under the race detector) and `make test-integration` (against real Postgres and real Docker via testcontainers-go). Both are documented in CONTRIBUTING.md and both run in CI. `<repo>/blob/main/.github/workflows/ci.yml` |
| `test_invocation` | ✅ | `go test ./...`, the standard invocation for Go, wrapped as `make test`. |
| `test_most` | 👤 | Coverage is measured on every push and reported to Codecov (`<repo>` badge in the README). `make requirements-coverage` additionally reports which of the 211 numbered requirements have a named acceptance test — currently 90. Answer honestly: "Unmet" or "Met" depending on where you set the bar; this is a SUGGESTED criterion and an honest Unmet does not block the badge. |
| `test_continuous_integration` | ✅ | GitHub Actions runs build, vet, lint, unit tests, console checks and the integration suite on every pull request and every push to `main`. `<repo>/blob/main/.github/workflows/ci.yml` |
| `test_policy` | ✅ | CONTRIBUTING.md and CLAUDE.md both require an acceptance test named for the requirement it satisfies, in the form `TestR132_UnfilledRequiredSlotBlocksDeploy`. `make requirements-coverage` reports which requirement IDs have one, and CI runs it. `<repo>/blob/main/CONTRIBUTING.md#pull-requests` |
| `tests_are_added` | ✅ | Recent changes carry their tests. Example from this change set: a fuzz target found a panic in argon2id hash decoding, the fix landed with `TestR042_AMalformedStoredHashDeniesRatherThanPanics`. `<repo>/blob/main/internal/hash/hash_test.go` |
| `tests_documented_added` | ✅ | It is the first item in CONTRIBUTING.md's "Pull requests" section and the first checkbox in the pull request template. `<repo>/blob/main/.github/PULL_REQUEST_TEMPLATE.md` |
| `warnings` | ✅ | `go vet` plus `golangci-lint` with errcheck, govet, ineffassign, staticcheck, unused, bodyclose, contextcheck, depguard, errorlint, gosec, nilerr, rowserrcheck and sqlclosecheck. The console runs oxlint, eslint and `tsc --noEmit`. All fail the build. `<repo>/blob/main/.golangci.yml` |
| `warnings_fixed` | ✅ | The build fails on any warning, so the tree is warning-free by construction. Suppressions are per-site `//nolint` comments naming the rule and the reason, never blanket disables. `issues.max-issues-per-linter` and `max-same-issues` are set to 0 so nothing is hidden by a reporting cap. |
| `warnings_strict` | ✅ | Linting goes beyond the defaults — eight linters past the golangci-lint v2 baseline, plus `gofmt` as a formatter — and includes a project-specific `depguard` rule enforcing an architectural boundary (adapters may not import the authorization, audit, state or policy packages). Reporting caps are disabled so every finding is shown. |

---

## Security

| Criterion | Status | Justification to paste |
|---|---|---|
| `know_secure_design` | 👤 | Yours to attest, and there is evidence to point at: `docs/design/06-authorization-and-proxy.md` and `SECURITY.md` document a design built on least privilege (two independent permission planes and two independent scopes), a single enforcement point that cannot be routed around, defense in depth (per-app network isolation, rootless builds with no runtime socket), fail-safe defaults (apps private by default) and an append-only audit log enforced by database grant rather than by code. `<repo>/blob/main/SECURITY.md#security-model` |
| `know_common_errors` | 👤 | Yours to attest. Evidence: the codebase addresses injection (parameterized queries via sqlc), XSS, CSRF (SameSite cookies), path traversal (zip-slip guards in archive extraction), header forgery (unconditional inbound `X-Pando-*` stripping), timing attacks (constant-time comparison and a decoy hash on unknown usernames), and decompression and upload bombs (bounded reads). Each has a mechanism and, in most cases, a test. |
| `crypto_published` | ✅ | Only published, reviewed algorithms: argon2id (RFC 9106) for password and token hashing, Ed25519 (RFC 8032) for identity assertions, AES-256-GCM for secrets at rest, ChaCha20-Poly1305 (RFC 8439) for backup bundles, SHA-256 for digests. Full inventory: `<repo>/blob/main/SECURITY.md#cryptography` |
| `crypto_call` | ✅ | Pando implements no cryptographic primitives. Everything comes from the Go standard library (`crypto/ed25519`, `crypto/aes`, `crypto/sha256`, `crypto/rand`, `crypto/subtle`) or `golang.org/x/crypto` (`argon2`, `chacha20poly1305`). |
| `crypto_floss` | ✅ | All cryptography is the Go standard library (BSD-3) and `golang.org/x/crypto` (BSD-3). Both are FLOSS, and there is no alternative code path requiring anything else. |
| `crypto_keylength` | ✅ | Ed25519 (≈128-bit security), AES-256, ChaCha20-Poly1305 (256-bit key), SHA-256, argon2id with a 32-byte key and 16-byte salt. All meet or exceed NIST's recommendations through 2030. No key length is configurable, so there is nothing to disable — smaller lengths cannot be selected. |
| `crypto_working` | ✅ | No broken algorithm is used anywhere. MD4, MD5, SHA-1, single DES, RC4 and Dual_EC_DRBG do not appear. Both AEAD modes in use (GCM, ChaCha20-Poly1305) are authenticated; there is no unauthenticated cipher mode in the codebase. |
| `crypto_weaknesses` | ✅ | No algorithm with a known serious weakness is used. SHA-1 appears only transitively inside `go-git`, where it is the git object format and not a security mechanism Pando relies on. |
| `crypto_pfs` | ✅ | Mark **N/A**. Pando implements no key agreement protocol and terminates no TLS — TLS is the responsibility of the reverse proxy in front of it, and modern TLS ciphersuites provide forward secrecy. Pando holds no long-term key that could decrypt a recorded session: assertion keys sign, they do not encrypt. `<repo>/blob/main/SECURITY.md#what-pando-does-not-claim` |
| `crypto_password_storage` | ✅ | Passwords are stored as argon2id hashes with a per-user 16-byte random salt: 64 MiB memory, 3 iterations, parallelism 2, 32-byte key. Parameters are encoded in each hash so they can be raised without invalidating existing credentials. API token secrets use the same function. `<repo>/blob/main/internal/hash/hash.go` |
| `crypto_random` | ✅ | Every key, salt, nonce, session ID, API token and generated password comes from `crypto/rand`. `math/rand` does not appear anywhere in the non-test source. |
| `delivery_mitm` | ✅ | Already met. |
| `delivery_unsigned` | ⏳ | Every release publishes a `SHA256SUMS` file signed with cosign in keyless mode, plus the signing certificate, and container images are signed by digest. `docs/releasing.md` documents the `cosign verify-blob` invocation with the expected workflow identity, and says explicitly that checking the checksum without checking the signature is not sufficient. SLSA build provenance is attached to both binaries and images. `<repo>/blob/main/docs/releasing.md#verifying-a-download` — **mark Met once the first signed release exists.** |
| `vulnerabilities_fixed_60_days` | ✅ | No publicly known vulnerability in Pando itself. Dependencies are scanned daily with `govulncheck`, on top of Dependabot. Two Moby advisories (GO-2026-4887, GO-2026-4883) are reported against the Docker client library Pando imports; both are daemon-side vulnerabilities in code the client does not contain, and neither has a fixed release. They are recorded with reasoning in `.github/govulncheck-allowlist.txt`; anything not listed there fails the build. |
| `vulnerabilities_critical_fixed` | ✅ | `SECURITY.md` commits to releasing a fix for a critical or high severity issue within 30 days of confirmation, on its own rather than bundled with unrelated work. `<repo>/blob/main/SECURITY.md#what-to-expect` |
| `no_leaked_credentials` | ✅ | `gitleaks` scans the full history daily and on every push, in addition to GitHub's push protection. There are two allowlist entries in `.gitleaks.toml`, each a test fixture named individually with the reason — test files are not excluded wholesale, because a leaked credential lands in a fixture as often as in production code. |

---

## Analysis

| Criterion | Status | Justification to paste |
|---|---|---|
| `static_analysis` | ✅ | Two tools, both in CI. CodeQL with the `security-extended` query set analyzes Go and TypeScript on every push, every pull request and weekly. `gosec` runs inside `make lint` on every pull request. `<repo>/blob/main/.github/workflows/codeql.yml` |
| `static_analysis_common_vulnerabilities` | ✅ | Both tools target common vulnerability classes specifically. CodeQL's `security-extended` covers injection, path traversal, unsafe deserialization and hardcoded credentials with interprocedural dataflow; `gosec` covers the Go-specific set — weak randomness, unsafe file permissions, integer overflow conversions, decompression bombs, TLS misconfiguration, slowloris exposure. |
| `static_analysis_fixed` | ✅ | Every finding is fixed or suppressed with a comment naming the rule and the reason it does not apply. Enabling `gosec` produced 25 findings. One was a real bug and is fixed: a missing `ReadHeaderTimeout` on the API server, a slowloris exposure that the proxy's own listeners already guarded against. Two more flag a single real weakness that needs a product decision rather than a correction, recorded as open decision O-18, with the suppression pointing at it rather than claiming it is a false positive. The rest are false positives, each annotated at the call site with why the rule does not apply. `<repo>/blob/main/docs/plan/open-decisions.md` `<repo>/blob/main/docs/plan/open-decisions.md` |
| `static_analysis_often` | ✅ | CodeQL runs on every push and every pull request, plus weekly so that new queries are applied to unchanged code. `gosec` runs on every pull request as part of `make lint`. |
| `dynamic_analysis` | ✅ | The entire test suite runs under the Go race detector (`go test -race`) in CI. Native Go fuzzing covers the parsers that read untrusted input — the identity assertion verifier, the argon2id credential decoder, the compose-file importer and the backup envelope decryptor — running five minutes per target daily, with every seed corpus running on every pull request. `<repo>/blob/main/.github/workflows/security.yml` |
| `dynamic_analysis_unsafe` | ✅ | Mark **N/A**. Pando is written in Go and TypeScript, both memory-safe. There is no cgo, no `unsafe` import anywhere in the source, and the binary is built with `CGO_ENABLED=0`. |
| `dynamic_analysis_enable_assertions` | ✅ | Tests run under the Go race detector, which instruments every memory access and fails on a detected race. Fuzz targets assert security properties rather than only checking for panics: the assertion fuzzer fails if any token the minter did not sign verifies, and the credential fuzzer fails if any invented hash matches an unrelated password. Integration tests run against real Postgres and real Docker rather than fakes. |
| `dynamic_analysis_fixed` | ✅ | Findings are fixed rather than suppressed. Fuzzing found a panic in argon2id hash decoding — `argon2.IDKey` panics rather than errors on a zero time cost or zero parallelism, and an empty key field compared equal to an empty candidate, so a malformed row could crash the sign-in path or accept any password. Both are fixed with bounds checks and covered by `TestR042_AMalformedStoredHashDeniesRatherThanPanics`. `<repo>/blob/main/internal/hash/hash.go` |

---

## Before submitting: repository settings

Four of the answers above depend on GitHub settings that are not in the repository and cannot be set
by a commit. Do these first, or the justification will not match what a reviewer sees.

1. **Settings → Code security → Private vulnerability reporting: Enable.** Without it,
   `<repo>/security/advisories/new` does not accept reports from anyone outside the org, and
   `vulnerability_report_private` is not actually met.
2. **Settings → General → Features → Discussions: Enable.** `SECURITY.md`, the README and the issue
   chooser all link it.
3. **Settings → Code security → Secret scanning and push protection: Enable.** Defense in depth
   alongside the `gitleaks` job, and it is what Scorecard looks for.
4. **Settings → Branches → Branch protection on `main`:** require pull requests and require the CI
   checks to pass. `repo_interim` and the Scorecard branch-protection check both read this.

Optional but worth it: add the badge to the README once it is awarded, and expect the Scorecard badge
already in the README to resolve after the first scheduled run.

## Still your decision

Two things in this change set are deliberately left open.

- **O-18 — when the session cookie is marked `Secure`.** Found by turning `gosec` on. The cookie uses
  `Secure: r.TLS != nil`, which is correct for the documented localhost install and wrong behind a
  TLS-terminating reverse proxy, which is the other documented topology. The options and their costs
  are in `docs/plan/open-decisions.md`; the `[P]` favorite is an explicit `PANDO_EXTERNAL_URL`. This
  needs answering before an installation is exposed to the internet.
- **The first release.** Four criteria are blocked on a tag existing rather than on any missing work.
  Tagging `v0.1.0` clears `version_unique`, `version_tags`, `release_notes` and `delivery_unsigned`
  in one step — move the `Unreleased` section of `CHANGELOG.md` into a dated version section first,
  because the release workflow fails without one.
