# Phase 0 — Skeleton

**Goal:** the scaffolding every later phase assumes, plus the two mechanisms that are far cheaper to
build now than to retrofit — the audit log's immutability and the adapter import rule.

**Not blocked.** O-11 is resolved (design 00 §1.1): Postgres is supplied by the install topology — a
Compose file with a `pando` service and a `postgres` service that start together — with an
external-database override in configuration. Pando does not start Postgres, so phase 0 needs nothing
from phase 3.

**Design:** `../design/00-stack-and-conventions.md` in full, `../design/02-data-model.md` §1, §2.6.

## Tasks

- [ ] `go.mod`, repo layout per design 00 §2 (already scaffolded — fill it in)
- [ ] Config loading: viper, YAML + env + flags (R-271)
- [ ] zap logger, threaded through `context.Context` alongside request ID, principal, deadline
- [ ] `internal/id` — prefixed ULID generation for every object kind
- [ ] `internal/errs` — the error envelope and the full code taxonomy from design 00 §3.2, including
      every named code the requirements promise
- [ ] `internal/secret` — `secret.Value` whose `String()`, `MarshalJSON()`, and `MarshalLogObject()`
      all return `[redacted]` (R-194)
- [ ] `Clock` interface in core, so backoff is testable without sleeping
- [ ] Postgres connection via pgx, migrations via golang-migrate, embedded in the binary
- [ ] Compose file: `pando` and `postgres` services, starting together (design 00 §1.1)
- [ ] Bounded connect retry at startup — both services start at once, so Postgres will not be ready
      when Pando first dials. This is the standard Compose race; handle it rather than assuming
      readiness
- [ ] External-database support: `PANDO_DATABASE_URL` config path
- [ ] **Privilege preflight for the external path** — verify at startup that Pando can create and
      constrain the restricted application role, and **fail loudly** if not. A degraded mode that runs
      with a rewritable audit log is not acceptable; the value of the grant is that it holds without
      anyone checking. Error text is held to R-105
- [ ] `audit_events` table, and `REVOKE UPDATE, DELETE` from the application role
- [ ] `apps.unobservable_since` and `apps.applied_env_fingerprint` when the apps table lands in phase
      2 — noted here so the columns are not discovered late (design 02 §2.3)
- [ ] sqlc wired up and generating
- [ ] chi router, `/healthz`
- [ ] The adapter import-lint rule in CI (`.golangci.yml` depguard) — design 03 §9
- [ ] `make check` green in CI

## Requirements in scope

R-027 (import rule + audit grant), R-194 (`secret.Value`), R-253 (single binary), R-271 (config).

## Done when

The server starts, `/healthz` responds, **and an audit event can be written and provably not
modified** — a test that attempts `UPDATE` and `DELETE` on `audit_events` as the application role and
asserts both fail.

Plus: `docker compose up` brings up a working install from nothing, and pointing
`PANDO_DATABASE_URL` at a database where Pando lacks the privileges to constrain the application role
fails at startup with a readable error rather than starting.

## Traps

- **Two database identities, not one.** The role that owns the schema and runs migrations is not the
  role the application connects as. The application role holds `INSERT` on `audit_events` and nothing
  else on it. Conflating them makes the grant meaningless, and it is the easiest thing in this phase
  to get subtly wrong.
- The audit grant is a database-level `REVOKE`, not application-level care. R-027 says no *adapter*
  can rewrite the audit log; doing it at the DB makes it true of core as well, which is stronger and
  costs nothing.
- `secret.Value` must refuse to render in **all three** marshalers. A type that redacts in `String()`
  but not in `MarshalJSON()` will leak through an error envelope.
