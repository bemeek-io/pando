# Security policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub's advisory form:

**<https://github.com/bemeek-io/pando/security/advisories/new>**

That form is private between you and the maintainers until an advisory is published. Do not open a
public issue for a vulnerability, and do not report one in a pull request — a pull request that fixes
a security bug describes the bug in public before anyone can upgrade.

The advisory form is the only reporting channel. It requires a GitHub account, which is a real cost
for a reporter who does not have one, and it is the trade we are making: one channel that is
definitely monitored beats a second one that might not be.

### What to include

- The version or commit you tested, and how Pando was installed.
- Which component is affected — the proxy, the API, an adapter, the console, the CLI, the MCP server.
- What an attacker gains, and what access they need to start. "An authenticated user with `app.view`
  on one app can read another app's secrets" is actionable. "Secrets can be read" is not.
- Steps to reproduce, and a proof of concept if you have one.

### What to expect

| Stage | Target |
|---|---|
| Acknowledgment that the report arrived | 3 business days |
| An initial assessment — is it a vulnerability, and how severe | 14 days |
| Fix released for a critical or high severity issue | 30 days from confirmation |
| Fix released for a medium or low severity issue | The next release |

Severity is assigned with [CVSS v3.1](https://www.first.org/cvss/v3.1/specification-document). If a
fix will take longer than the target above, we will say so in the advisory thread rather than let it
go quiet.

Fixes are released together with a GitHub Security Advisory and a CVE where one applies, and are
listed in [`CHANGELOG.md`](CHANGELOG.md). Reporters are credited in the advisory unless they ask not
to be.

### Scope

In scope: anything in this repository — the server, proxy, adapters, console, CLI, MCP server,
migrations, and the shipped `docker-compose.yml` and `Dockerfile`.

Out of scope:

- Vulnerabilities in applications a user deploys *onto* Pando. Pando authenticates the caller and
  authorizes the request; it does not audit the app behind it.
- Vulnerabilities in a third-party dependency with no reachable path from Pando's own code. Report
  those upstream. If there is a reachable path, it is in scope — say which call reaches it.
- A finding that requires host root, physical access, or a host already under an attacker's control.
  Pando trusts the host it runs on.
- Missing hardening that has no exploit behind it. Send that as an issue or a pull request.

## Supported versions

Before 1.0, only the most recent release receives security fixes. Once 1.0 ships, this table will
name the supported minor versions and the date each stops receiving fixes.

| Version | Supported |
|---|---|
| Most recent release | Yes |
| Anything older | No — upgrade |

v0.1.0 is the current release. Security fixes land on `main` and go out in the next release.

## Security model

The design is in [`docs/design/06-authorization-and-proxy.md`](docs/design/06-authorization-and-proxy.md);
this is the short version of what Pando relies on and what it does not claim.

**One enforcement point.** Every request to every app goes through Pando's proxy, which
authenticates the caller and makes the authorization decision (R-023). There is no bypass for public
apps or for websockets. A routing adapter puts traffic *in front of* that proxy and never points at a
workload directly.

**Two independent planes.** Being able to use an app and being able to administer it are separate
grants (R-029, R-070, R-071). Holding an administrative role does not make someone a user of every
app, and using an app daily grants nothing administrative.

**Two independent scopes.** A grant is either app-scoped or installation-scoped, never both, and each
check refuses the other's verbs rather than evaluating them (R-080). An administrator holds no
`app.*` verb and is not an implicit owner of every app (R-087).

**Headers are not trust.** Inbound `X-Pando-*` headers are stripped unconditionally before a request
reaches an app (R-053), so a client cannot forge one. What an app can trust is the signed assertion
in `X-Pando-Assertion`, verified against the JWKS Pando publishes. Outbound `pando_*` cookies are
stripped before reaching an app (R-173), so Pando's session cookie is never exposed to a deployed
application.

**The audit log cannot be rewritten.** The database role Pando runs as holds no `UPDATE` or `DELETE`
on `audit_events` (R-027). Neither Pando nor an adapter can alter the record after the fact.

**Builds get no container runtime socket.** BuildKit runs rootless in its own container, and an
integration test asserts on that container's actual mount list (R-112). A build that could reach the
runtime socket would be a container escape by design.

**Secrets do not reach logs.** Secret values are carried in a `secret.Value` type that renders
`[redacted]` in every marshaler it implements — `String`, `GoString`, `Format`, `MarshalJSON`,
`MarshalText`, and zap's `MarshalLogObject` (R-194).

**Apps are isolated from each other.** Each app runs on its own private network and publishes nothing
to the host beyond the port Pando allocates it.

### What Pando does not claim

- It trusts the host it runs on and the container runtime it drives. A compromised host is a
  compromised installation.
- It is not multi-tenant (R-015). One installation serves one organization, and the isolation between
  apps is container-level, not a hostile-tenant boundary.
- It does not terminate TLS itself. TLS is the job of whatever sits in front of it — Traefik, a
  reverse proxy, or a load balancer. On a public installation, running Pando without TLS in front
  exposes session cookies and bearer tokens.
- Accounts are local to the installation in this version. There is no external identity provider yet.

### Cryptography

Pando does not implement cryptographic primitives. Everything below comes from the Go standard
library or `golang.org/x/crypto`.

| Use | Algorithm | Where |
|---|---|---|
| Password and API token hashing | argon2id — 64 MiB, t=3, p=2, 16-byte salt, 32-byte key; parameters encoded in each hash so they can be raised without invalidating credentials | [`internal/hash`](internal/hash/hash.go) |
| Identity assertions to apps | Ed25519 (JWT `EdDSA`), 120-second lifetime, key ID from SHA-256 of the public key, rotation by JWKS overlap | [`internal/core/assertion`](internal/core/assertion/assertion.go) |
| Secrets at rest | AES-256-GCM, per-value nonce, adapter reference as additional authenticated data | [`internal/adapter/secrets/local`](internal/adapter/secrets/local/local.go) |
| Backup and DR bundles | ChaCha20-Poly1305, key derived from the passphrase with argon2id, parameters written into the envelope | [`internal/core/backup`](internal/core/backup/crypt.go) |
| Content fingerprints and digests | SHA-256 | `internal/core/deploy`, `internal/core/backup` |
| All keys, salts, nonces, tokens, and generated passwords | `crypto/rand` | throughout |

Credential verification compares derived keys with `crypto/subtle.ConstantTimeCompare`. When the
account does not exist, sign-in verifies against a decoy argon2id hash rather than returning early,
so a failed attempt costs the same whether or not the username is real.

`math/rand` does not appear anywhere in the non-test source. MD5, SHA-1, DES and RC4 are not used.
Key lengths meet the NIST recommendations through 2030 and beyond: Ed25519 (≈128-bit security),
AES-256, ChaCha20-Poly1305 (256-bit). There is no configuration that lowers them, because no
algorithm or key length is configurable.

Forward secrecy is a property of the TLS terminator in front of Pando, not of Pando itself. Pando
holds no long-term key that a recorded session could be decrypted with later: assertion signing keys
sign, they do not encrypt.

## Secure development

- **Memory safety.** Go and TypeScript, both memory-safe. There is no cgo and no `unsafe` in the
  source, and the binary is built with `CGO_ENABLED=0`.
- **Static analysis.** `golangci-lint` including `gosec`, plus CodeQL for Go and TypeScript on every
  push and weekly. See [`.github/workflows`](.github/workflows).
- **Dependency vulnerabilities.** `govulncheck` on every push and daily, Dependabot for Go modules,
  npm, GitHub Actions and Docker base images. Findings that are understood and accepted are listed,
  with the reasoning, in [`.github/govulncheck-allowlist.txt`](.github/govulncheck-allowlist.txt);
  anything not in that file fails the build. The open advisories are enumerated below.
- **Secret scanning.** `gitleaks` over the full history on every push, in addition to GitHub's own
  push protection.
- **Dynamic analysis.** The whole test suite runs under the Go race detector. Native Go fuzzing
  covers the parsers that see untrusted input — credential hashes, identity assertions, app specs,
  compose files, and backup envelopes.
- **Structural invariants.** The rules in §2 of [`CLAUDE.md`](CLAUDE.md) are enforced by database
  constraints, triggers, grants and lint rules rather than by review, because review eventually
  misses one.

### Open advisories

Two tools look at dependencies and disagree, for a reason worth understanding before reading either.

**`govulncheck` matches at the call level.** It reports an advisory only when a vulnerable function is
reachable from Pando's own code. It is what gates CI, because a finding it reports is a call path
rather than a row in a dependency tree.

**OpenSSF Scorecard matches at the module level**, through OSV. If a module Pando depends on has any
advisory against it, Scorecard counts it — whatever package the advisory is about, and whether or not
that package is linked into the binary. Its count is therefore always the higher of the two, and the
difference is not a disagreement about facts.

Six advisories are open against the dependency tree. None has a fix available today.

| Advisory | Module | Why it is open |
|---|---|---|
| GO-2026-4883 | `github.com/docker/docker` | Off-by-one in plugin privilege validation |
| GO-2026-4887 | `github.com/docker/docker` | AuthZ plugin bypass on oversized request bodies |
| GO-2026-5617 | `github.com/docker/docker` | `docker cp` race allowing bind-mount replacement |
| GO-2026-5668 | `github.com/docker/docker` | `docker cp` race allowing arbitrary file creation |
| GO-2026-5746 | `github.com/docker/docker` | `PUT /containers/{id}/archive` executes a container binary on the host |
| GO-2026-5932 | `golang.org/x/crypto` | `openpgp` is unmaintained and unsafe by design |

**The five Docker advisories are daemon vulnerabilities.** Pando imports
`github.com/docker/docker` as a *client* library — it speaks to a daemon over a socket, it does not
contain one — and none of the affected code paths exist in the client. `v28.5.2` is the newest
release of that module path and none of the five is fixed in it; the fixes are in
`github.com/moby/moby/v2`, which is a different module and currently at `v2.0.0-beta.23`. Moving a
deployment platform's runtime adapter onto a beta module to silence advisories about code it does not
execute is the wrong trade. The daemon these describe is the host's own Docker installation, and it
is patched by patching Docker.

Two of the five are reachable in `govulncheck`'s sense, because the advisories name the whole module
rather than specific symbols, so every trace resolves to something like `docker.init calls
filters.init`. Those two are in the allowlist with that reasoning; the rest are not reported by
`govulncheck` at all.

**The `x/crypto` advisory is about a package Pando does not build.** `golang.org/x/crypto/openpgp`
does not appear anywhere in the build graph — `go list -deps ./...` does not name it. Pando uses
`x/crypto` for argon2, ChaCha20-Poly1305 and SSH; the OpenPGP code reached through `go-git` is
`github.com/ProtonMail/go-crypto/openpgp`, the maintained fork that exists because the `x/crypto` one
was deprecated. There is no version of `x/crypto` that resolves this, because the resolution is not
to use the package, which Pando already does not.

Re-check this list whenever a dependency is upgraded, and remove an entry the moment a fixed release
exists. An entry stays here because there is no fix, never because the fix is inconvenient.
