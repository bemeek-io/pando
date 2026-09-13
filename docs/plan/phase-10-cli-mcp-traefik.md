# Phase 10 — CLI, MCP, Traefik

**Goal:** the remaining surfaces, and the test of whether the routing abstraction actually holds.

**Prerequisites:** phase 8.

**Design:** `../design/04-api.md` §3, §4; `../design/03-adapter-interfaces.md` §4.

## Tasks

- [x] CLI (cobra) per design 04 §4. `internal/cli` imports no core package, so a command needing
      something the API lacks fails to compile — R-261 as a dependency rule rather than a review note
- [x] `pando deploy ./` from a local path. Packs the directory, uploads it, runs detection, accepts
      and deploys; detection questions print **verbatim** (R-105) because they are written to be
      pasted into whatever wrote the app. Needed a source type (`upload`), an endpoint, and token
      endpoints that had never been wired
- [x] MCP server over the same API (R-262), JSON-RPC 2.0 on stdio. Ten tools, one per endpoint
- [x] Idempotency keys on `POST /apps` and `POST /apps/{id}/deployments`, by header or body field.
      Scoped to the principal as well as the endpoint
- [x] Traefik routing adapter: subdomain and path, TLS when a resolver is configured. **The routing
      interface did not change**
- [x] Notify adapter exercised by the console-only v1 implementation (R-231)
- [x] O-12 resolved as designed: host policy now sees the principal, so agent exclusions are a policy
      scoped to token principals rather than a list the MCP server keeps
- [x] `pando exec`. Raw mode restored on every exit path including a panic, SIGWINCH resize sent as
      the control frame the protocol defines, and the container's exit status carried out through the
      close reason so `pando exec app -- false` behaves like `false` in a script

## Requirements in scope

R-231, R-232, R-251, R-260–R-262.

## Done when

Every capability in the API is reachable from the CLI and (excepting the exclusions below) from MCP,
and Traefik routes an app end to end **without any change to the routing interface**.

**Met.** Traefik was run for real — `docker compose --profile traefik up` — and a
request to an app's hostname on Traefik's port is served by the app, an anonymous one is redirected
to login, and Pando's own console still answers on its own hostname. `RoutingAdapter` and its types
are untouched, which is the only real evidence that the abstraction held.

Running it found three bugs that reading it would not have:

- **The proxy resolved one way only**, switching on an install-wide mode. Design 03 §4.1 says an
  install can mix addressing modes; it could not. A subdomain app on a path-default install fell
  through to the console, looking to its owner like the app did not exist. The proxy's test double
  had been returning the same app for either lookup, so it agreed with the proxy no matter what the
  proxy did.
- **The console shadowed every subdomain app's root**, because it owns `/` and answered there on
  every hostname.
- **Every upgrade cut Pando off from every existing app.** The Docker adapter joined Pando's
  container to an app's private network only when *creating* that network, and a replaced container
  is a different container. Nothing detected it because a fresh install has no existing networks and
  a test suite recreates everything.

Two smaller ones worth recording: viper's `AutomaticEnv` resolves only keys it already knows, so
`PANDO_SERVER_BASE_DOMAIN` set nothing and said nothing; and adapter seeding was all-or-nothing on
first install, so a category added in a later version was never seeded on an install that already had
others.

## Traps

- **If Traefik requires changing the routing interface, the interface was wrong.** That is the entire
  reason this phase is last. Report it as an interface finding, do not quietly widen the interface to
  fit.
- **Traefik's config must point at Pando's proxy, never the workload.** State this in the adapter's
  contract, because an adapter author's instinct is to point it straight at the container.
- **Not exposed via MCP:** exec, secret value reads, grant mutation, policy mutation, user deletion.
  These are the highest-consequence actions in the system and an agent should not hold them by
  default. **[O-12] resolved:** policy-controlled and default-closed, expressed as **host policy per
  verb** rather than an MCP-specific list. An agent holding a token can call the REST API directly, so
  an MCP-layer block is a speed bump, not a boundary — enforcement lives where every surface passes
  through it. Do not build a second exclusion mechanism.
- An agent holds a token and is a principal like any other. **No MCP tool bypasses authorization**,
  and every action lands in the audit log under the token's owner (R-229).
- No surface may have a capability the API lacks (R-261). If the CLI can do something MCP cannot,
  check whether logic leaked into a handler.
