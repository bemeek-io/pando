# Phase 10 — CLI, MCP, Traefik

**Goal:** the remaining surfaces, and the test of whether the routing abstraction actually holds.

**Prerequisites:** phase 8.

**Design:** `../design/04-api.md` §3, §4; `../design/03-adapter-interfaces.md` §4.

## Tasks

- [ ] CLI (cobra) per design 04 §4
- [ ] `pando deploy ./` from a local path — R-262's agent workflow depends on it, because a generated
      app cannot drop a config file but an agent can invoke a command
- [ ] MCP server over the **same service layer** as `httpapi` (R-262)
- [ ] Idempotency keys on infrastructure-creating POSTs — required for MCP, where an agent retry must
      not deploy twice
- [ ] Traefik routing adapter: subdomain and path modes, TLS
- [ ] Notify adapter interface exercised by the console-only v1 implementation (R-231)

## Requirements in scope

R-231, R-232, R-251, R-260–R-262.

## Done when

Every capability in the API is reachable from the CLI and (excepting the exclusions below) from MCP,
and Traefik routes an app end to end **without any change to the routing interface**.

## Traps

- **If Traefik requires changing the routing interface, the interface was wrong.** That is the entire
  reason this phase is last. Report it as an interface finding, do not quietly widen the interface to
  fit.
- **Traefik's config must point at Pando's proxy, never the workload.** State this in the adapter's
  contract, because an adapter author's instinct is to point it straight at the container.
- **Not exposed via MCP:** exec, secret value reads, grant mutation, policy mutation, user deletion.
  These are the highest-consequence actions in the system and an agent should not hold them by
  default. **[O-12] unresolved:** whether this is a hard exclusion or a policy-controlled default.
- An agent holds a token and is a principal like any other. **No MCP tool bypasses authorization**,
  and every action lands in the audit log under the token's owner (R-229).
- No surface may have a capability the API lacks (R-261). If the CLI can do something MCP cannot,
  check whether logic leaked into a handler.
