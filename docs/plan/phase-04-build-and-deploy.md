# Phase 4 — Build and deploy

**Goal:** an app actually runs. This phase ends in Sequence B, the first end-to-end acceptance test.

**Prerequisites:** phase 3.

**Design:** `../design/05-reconciler-and-lifecycle.md` §3; `../design/07-sequences.md` B;
`../design/03-adapter-interfaces.md` §3.

## Tasks

- [x] BuildKit builder adapter: rootless, containerized, **no runtime socket** (R-111, R-112)
- [x] Per-app build cache namespace (R-117)
- [x] Build egress control per policy (R-118) and build timeout (R-119)
- [x] Build logs streamed live to SSE
- [x] `deployments` table and the deployment pipeline, steps 8–16
- [x] Source clone; resolve ref → commit SHA and **write it into the spec** (R-120)
- [x] Secret materialization into `WorkloadPlan.Env` — fully resolved before the adapter sees it
- [x] Volume creation
- [x] Recreate strategy (R-144)
- [x] Route `Ensure` — pointing at **Pando's proxy**, never the workload
- [x] Health wait, with the source precedence in R-221
- [x] Deployment endpoints, including the SSE log stream

## Requirements in scope

R-111, R-112, R-117–R-120, R-140, R-144, R-146, R-190–R-194, R-221, R-243.

## Done when

**Met.** Shipped in an earlier phase; the named tests exist and the full acceptance
suite is green. The boxes below went unticked at the time — corrected here rather than
left to imply the work is outstanding.

**Sequence B passes** with a hand-written spec — including the assertion that the build container has
no runtime socket mounted.

## Traps

- **The socket test is non-negotiable.** Assert on the build container's actual mount list. This is a
  named risk in the register precisely because it is easy to reintroduce accidentally.
- **A failed build leaves the running app untouched** (R-146). The *deployment* is failed; the *app*
  is not, and its state does not change. This is why `deployments` is its own table.
- **Traefik's generated config must point at Pando's proxy address**, not the workload's — even
  though the workload address is right there and would work.
- Secrets must not appear in build logs, deployment records, or audit detail. Test it.
- Start-then-swap is opt-in only (R-145) and requires the runtime capability. Its constraint goes in
  body text at the point of enabling, not a tooltip.
