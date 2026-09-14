# Releasing

How a Pando version is numbered, tagged, built, signed and announced.

## Version numbers

Pando follows [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html). A version is
`MAJOR.MINOR.PATCH`, and the tag is that string with a `v` prefix: `v0.3.1`.

What each part promises, for an operator running an installation:

| Part | Changes when |
|---|---|
| **MAJOR** | Something breaks on upgrade: an HTTP API endpoint or response shape is removed or changed incompatibly, an app spec field is removed, a configuration variable is removed, a migration cannot be applied to the previous version's database, or an adapter interface changes. |
| **MINOR** | Something is added: a new endpoint, spec field, configuration variable, adapter, console capability or CLI command. Existing behavior is unchanged. |
| **PATCH** | A bug or a vulnerability is fixed, with no interface change. |

Before `1.0.0`, MINOR carries the breaking changes — `0.4.0` may break what `0.3.0` did. Patch
releases within a `0.x` line never break.

Pre-releases use a SemVer pre-release suffix: `v1.0.0-rc.1`. They are marked as pre-releases on
GitHub and are not the `latest` Docker tag.

The API is versioned separately and independently, at `/api/v1`. A Pando MAJOR bump does not imply an
API version bump, and the API version changes only when `/api/v1` itself stops being supported.

## What a version identifies

Every release is a git tag, and the tag is the single identifier for it:

- **The tag** — `v0.3.1`, annotated and signed, on the commit the release was built from.
- **The GitHub release** — its notes are the `CHANGELOG.md` section for that version, not a commit log.
- **The binaries** — `pando version` prints the version, the commit and the build date, stamped in at
  build time with `-ldflags`. A build from a working tree that is not a tagged commit reports `dev`.
- **The container image** — `ghcr.io/bemeek-io/pando:0.3.1`, plus `:0.3` and `:latest`. A bare major
  tag is published only from 1.0 onward: `:0` would otherwise move across `0.x` minors that are
  allowed to break. Every image is also addressable by digest, and the digest is what an install that
  needs to be reproducible should pin.

There is no unversioned artifact. A binary that says `dev` came from a working tree, and that is the
only thing it can mean.

## Before tagging

1. `make check` and `make test-integration` are green on the commit being tagged.
2. `CHANGELOG.md` has a section for the version, dated, with every user-visible change in it. The
   **Security** subsection names every publicly known vulnerability fixed in this release that
   already had a CVE or GHSA identifier when the release was cut — that is a requirement, not a
   courtesy, because it is the only thing telling an operator whether the upgrade is urgent.
3. Any open advisory that this release fixes is ready to publish at the same moment as the release.
   The fix and the disclosure go out together; a fix pushed ahead of its advisory tells an attacker
   about a bug before it tells an operator.
4. Migrations apply cleanly from the previous release's schema.

## Cutting the release

```bash
# Move the Unreleased section into a dated version section first, and commit it.
git tag -s v0.3.1 -m "v0.3.1"
git push origin v0.3.1
```

The tag is signed. `git tag -v v0.3.1` verifies it against the signer's public key.

Pushing a `v*` tag runs [`.github/workflows/release.yml`](../.github/workflows/release.yml), which:

1. Rebuilds from the tagged commit with `-trimpath` and a stamped version.
2. Cross-compiles for linux and darwin, amd64 and arm64.
3. Writes `SHA256SUMS` over every artifact.
4. Signs `SHA256SUMS` with [cosign](https://docs.sigstore.dev/) in keyless mode, producing
   `SHA256SUMS.sig` and `SHA256SUMS.pem`. There is no long-lived signing key to leak; the signature
   is bound to the workflow identity through Sigstore's transparency log.
5. Generates SLSA build provenance for the artifacts.
6. Builds and pushes the multi-architecture container image, with provenance attached.
7. Creates the GitHub release with the `CHANGELOG.md` section as its notes.

Everything is served over HTTPS: GitHub releases, the container registry, and the Go module proxy.

## Verifying a download

A checksum retrieved over HTTPS and used without checking a signature only proves the file matches
what the same server said it should be. Check the signature:

```bash
# Download the binary, SHA256SUMS, SHA256SUMS.sig and SHA256SUMS.pem from the release.

cosign verify-blob SHA256SUMS \
  --signature       SHA256SUMS.sig \
  --certificate     SHA256SUMS.pem \
  --certificate-identity-regexp '^https://github\.com/bemeek-io/pando/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'

sha256sum --check --ignore-missing SHA256SUMS
```

The first command establishes that `SHA256SUMS` was produced by Pando's release workflow from a
tagged commit. The second establishes that the binary you have is the one it describes. Running the
second without the first is the case this section exists to warn about.

For the container image:

```bash
cosign verify ghcr.io/bemeek-io/pando:0.3.1 \
  --certificate-identity-regexp '^https://github\.com/bemeek-io/pando/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

## Security releases

A release that fixes a vulnerability follows the process above, plus:

- The GitHub Security Advisory is published in the same window as the release.
- The `CHANGELOG.md` **Security** subsection names the identifier, the severity, the affected version
  range and what an operator should do beyond upgrading, if anything.
- A critical or high severity fix is released on its own rather than waiting for unrelated work, so
  the upgrade carries the smallest possible change.

See [`SECURITY.md`](../SECURITY.md) for how a vulnerability reaches this process in the first place.
