# Phase 0 — Skeleton

**Goal:** the scaffolding every later phase assumes, plus the two mechanisms that are far cheaper to
build now than to retrofit — the audit log's immutability and the adapter import rule.

**Blocked by: O-11.** How Postgres is supplied — bundled container, bring-your-own, or embedded
binary — must be decided before migrations are written. Everything downstream assumes Postgres
regardless; only the install path changes. See `open-decisions.md`.

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
- [ ] `audit_events` table, and `REVOKE UPDATE, DELETE` from the application role
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

## Traps

- The audit grant is a database-level `REVOKE`, not application-level care. R-027 says no *adapter*
  can rewrite the audit log; doing it at the DB makes it true of core as well, which is stronger and
  costs nothing.
- `secret.Value` must refuse to render in **all three** marshalers. A type that redacts in `String()`
  but not in `MarshalJSON()` will leak through an error envelope.
