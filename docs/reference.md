# Reference

Pando's external interfaces — everything that goes in and everything that comes out. Start here when
you need to know what a surface accepts rather than how it is built.

R-261 makes one of these authoritative: **the HTTP API is the product.** The console, the CLI and the
MCP server are clients of it, and none has a capability the API lacks. When two documents here
disagree, the API reference is the one that is right.

| Interface | Reference |
|---|---|
| HTTP API | [`design/04-api.md`](design/04-api.md) |
| App spec — the object almost everything operates on | [`design/01-spec-schema.md`](design/01-spec-schema.md) |
| CLI | [`../README.md#cli`](../README.md#cli), and `pando <command> --help` |
| MCP server | [`../README.md#mcp-server`](../README.md#mcp-server) |
| Adapter interfaces — for contributing one | [`design/03-adapter-interfaces.md`](design/03-adapter-interfaces.md) |
| Authorization verbs and the proxy's request contract | [`design/06-authorization-and-proxy.md`](design/06-authorization-and-proxy.md) |

---

## HTTP API

Base path `/api/v1`. JSON in and out. Every response carries an `X-Request-Id`.

Authentication, in precedence order:

1. `Authorization: Bearer tok_…` — a token principal, from `pando token`.
2. `Cookie: pando_session=ses_…` — a browser session, from `POST /api/v1/sessions`.

Resources, in full, are in [`design/04-api.md`](design/04-api.md) §2: apps, deploys, specs, slots,
secrets, environment, volumes, grants, users, groups, roles, host policy, backups, tokens and the
audit log.

**Errors** are one envelope everywhere. The `code` is stable and machine-readable; the `message` and
`remedy` are written to be pasted into an assistant and acted on without further context (R-105).

```json
{
  "code": "PLAN_SLOT_UNFILLED",
  "message": "This app needs a PostgreSQL database, and one hasn't been chosen yet.",
  "remedy": "Choose how to fill the database slot: provision one inside this app, connect to an existing one, or paste a connection string.",
  "details": { "slots": [{ "key": "database", "type": "postgres" }] },
  "request_id": "req_01HQ8…"
}
```

Code prefixes and what they mean: `AUTH_*` the caller is not who they need to be, `PERM_*` they are
but may not do this, `POLICY_*` host policy forbids it for everyone, `PLAN_*` the app cannot be
deployed as configured, `VALID_*` the request is malformed, `STATE_*` the object is in the wrong
state for this, `CAPACITY_*` a resource is exhausted, `BACKUP_*` and `ADAPTER_*` name their subsystem.
A `WARN_*` is never a blocker — anything that blocks is a `PLAN_*` error.

**IDs** are prefixed, sortable and opaque: `app_`, `spec_`, `usr_`, `tok_`, `vol_`, `gr_`, `ses_`,
`req_`, with a ULID body. Treat them as strings; the prefix is for reading, not parsing.

**Time** is RFC 3339, UTC, everywhere on the wire.

**Pagination** is cursor-based: `?limit=50&cursor=…`, and the response carries `next_cursor`.

**Idempotency**: `POST` endpoints that create infrastructure accept an `Idempotency-Key` header, and
a retry replays rather than repeats.

## What an app receives

An app deployed on Pando is handed three things, and the distinction between the first and the second
is the whole security model.

**`X-Pando-Assertion`** — a signed Ed25519 JWT describing the caller. This is the only statement about
identity an app should trust. Verify it against the JWKS Pando publishes at `/.well-known/jwks.json`.
Claims: `sub` (stable per user, independent of email and of the identity adapter), `email`, `name`,
`groups`, `aud` (the app's ID — this is what stops an assertion minted for one app being replayed
against another), `iat`, `exp`, `iss`. Valid for 120 seconds.

**Other `X-Pando-*` headers** — the same information, unsigned, for convenience. Explicitly not
trustworthy on their own (R-053). Inbound `X-Pando-*` headers from a client are stripped
unconditionally before a request reaches an app, so they cannot be forged; but an app that reads them
instead of verifying the assertion is trusting Pando's proxy rather than a signature, and there is no
way for it to tell the difference if the proxy is ever bypassed.

**Environment variables** — the app's own configuration, plus anything injected by a filled slot
(database credentials and connection strings) and any secret bound to it. Nothing is read from the
repository at deploy time (R-020): the spec is the sole record of how an app runs.

An app never receives a `pando_*` cookie. Those are stripped on the way out (R-173).

## Configuration

Set as environment variables, or in a config file. The environment prefix is `PANDO_`, and a nested
setting is joined with an underscore: `server.base_domain` is `PANDO_SERVER_BASE_DOMAIN`.

| Variable | Default | Purpose |
|---|---|---|
| `PANDO_DATABASE_URL` | the bundled Postgres | Point Pando at an existing database. |
| `PANDO_SERVER_ADDR` | `:8080` | Address the console and API listen on. |
| `PANDO_SERVER_BASE_DOMAIN` | `localtest.me` | Domain per-app subdomains are taken from, under hostname routing. |
| `PANDO_SERVER_ROUTING_MODE` | port | How apps are addressed: port, subdomain or path. |
| `PANDO_SERVER_ISSUER` | derived | The `iss` claim in identity assertions. |
| `PANDO_SERVER_PROXY_UPSTREAM` | — | Where the proxy sends traffic it has authorized. |
| `PANDO_SERVER_WORK_DIR` | `/var/lib/pando` | Build contexts, uploads and adapter state. |
| `PANDO_APP_PORT_START` / `_END` | `9000` / `9019` | Range of host ports apps are allocated. |
| `PANDO_ADMIN_PASSWORD` | generated | Initial admin password. Read on first run only. |
| `PANDO_LOG_LEVEL` | `info` | Log verbosity. |
| `PANDO_RECONCILER_BACKOFF` | see R-149 | Retry schedule. Compressing it is for tests; `pando` warns when it is set faster than the shipped default. |

`PANDO_PORT` is not read by Pando. It is a variable in the shipped `docker-compose.yml`, which uses
it to choose the host port published in front of the container's fixed `8080`.

## Guarantees worth relying on

These are requirements, not implementation details, and they will not be changed without a major
version:

- A user's `sub` is stable across email changes and across a change of identity adapter (R-054). An
  app may key its own data on it.
- The audit log is append-only, at the database grant level (R-027). Nothing that happened stops
  having happened.
- Spec revisions are append-only, enforced by a database trigger, so a rollback target cannot be
  rewritten under you. The last ten pinned specs are retained (R-152).
- Deleting an app decides what happens to its data rather than assuming. Interactively Pando asks
  whether to keep a final backup (R-204); non-interactively, through the CLI, API or MCP, it backs up
  by default and `--force` is what skips it (R-205).
- A failed app stays failed until a person acts (R-151). Nothing retries it back into existence.
- Warnings never block. Anything that blocks is a `PLAN_*` error, and that split is deliberate: a
  warning shaped like an error teaches people to ignore both.

## Other documents

- [`../SECURITY.md`](../SECURITY.md) — the security model, the cryptography in use, and how to report
  a vulnerability.
- [`releasing.md`](releasing.md) — version numbering, and how to verify a download's signature.
- [`../CONTRIBUTING.md`](../CONTRIBUTING.md) — building it, and what a change needs before it merges.
- [`requirements.md`](requirements.md) — what Pando is, as 210 numbered requirements. The authority
  behind everything above.
- [`traceability/requirements-index.md`](traceability/requirements-index.md) — generated: which
  requirement is implemented where, and which have acceptance tests.
