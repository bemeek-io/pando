# Phase 3 — Adapters and the planner

**Goal:** the seven interfaces, three real implementations, and every plan-time error path. The plan
boundary established here is what makes plan-time failure meaningful rather than a label on a
mid-deploy crash.

**Prerequisites:** phase 2.

**Design:** `../design/03-adapter-interfaces.md` in full; `../design/05-reconciler-and-lifecycle.md`
§3 steps 1–7. Also read `../../internal/adapter/CLAUDE.md`.

## Tasks

- [ ] `internal/adapter/api` — all seven interfaces, definitions only
- [ ] Capability structs; **returned as data, never type assertions** (R-254)
- [ ] `IsolationClass` as an ordered integer with gaps of 10
- [ ] The registry: `Register`, `Get`, `ByCategory`, `Default`; registration in `main`
- [ ] `adapter_configs` migration and endpoints; `GET /adapters` returns **live** capabilities
- [ ] Docker runtime adapter: apply, observe, stop, destroy, volumes, logs, exec, capacity
- [ ] Loopback routing adapter (port mode, no TLS — the laptop default)
- [ ] Local secrets adapter, encrypted at rest, key on disk (R-190)
- [ ] Host policy evaluation and the `host_policy` singleton
- [ ] The planner: steps 1–7 of the deployment pipeline, side-effect-free
- [ ] `POST /apps/{id}:plan`

## Requirements in scope

R-024, R-114, R-132, R-182, R-190, R-242, R-243, R-250, R-251, R-253, R-254, R-255, R-274.

## Done when

`POST /apps/{id}:plan` returns each of `PLAN_SLOT_UNFILLED`, `PLAN_CAPABILITY_UNSUPPORTED`,
`CAPACITY_WOULD_OVERSUBSCRIBE`, and `PLAN_NO_ADAPTER_MEETS_POLICY` **on the right inputs** — four
tests, four distinct error paths.

## Traps

- **Sketch the Cloudflare adapter on paper before fixing the routing interface.** The known risk here
  is a routing abstraction that is secretly Docker- and Traefik-shaped. Ten minutes on paper now is
  cheaper than phase 10 discovering it.
- **Capacity is adapter-reported** (R-243). Core does not read `/proc`. The local Docker adapter
  reports its own machine; a clustered adapter would report its cluster.
- **The plan boundary is absolute.** Steps 1–7 create nothing. If a check needs a side effect to run,
  it belongs after the boundary and it is not a plan-time check.
- The planner refuses to plan against an unhealthy adapter — `ADAPTER_UNAVAILABLE`, not a mid-deploy
  failure.
- Error messages here are user-facing and held to R-105. `PLAN_SLOT_UNFILLED` reads "This app needs a
  Redis, and one hasn't been chosen yet," with a remedy naming the three ways to fill it.
