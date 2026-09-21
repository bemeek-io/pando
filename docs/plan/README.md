# Build plan

One file per phase, in build order. Sequenced so each phase produces something runnable and the
riskiest work happens while it is still cheap to change.

| Phase | File | Produces | Blocked by |
|---|---|---|---|
| 0 | `phase-00-skeleton.md` | A server that starts and an audit event that provably cannot be modified | — |
| 1 | `phase-01-identity-and-authz.md` | Login, tokens, roles, the authorizer | 0 |
| 2 | `phase-02-spec-and-state.md` | Apps and specs that validate, version, and diff | 1 |
| 3 | `phase-03-adapters-and-planner.md` | Every plan-time error path, on real adapters | 2 |
| 4 | `phase-04-build-and-deploy.md` | Sequence B — a real app deployed | 3 |
| 5 | `phase-05-proxy.md` | Sequence C — a real request enforced | 4 |
| 6 | `phase-06-detection.md` | Sequence A — an app onboarded from a bare repo | 5 |
| 7 | `phase-07-reconciler.md` | Drift correction, and a failed app that stays failed | 4 |
| 8 | `phase-08-console.md` | The launcher and the four weight-bearing screens | 5 |
| 9 | `phase-09-backup-and-dr.md` | Sequence D — verify-then-restore | 7 |
| 10 | `phase-10-cli-mcp-traefik.md` | The remaining surfaces, and the abstraction test | 8 |
| 11 | `phase-11-ai-assistance.md` | AI screening of detection proposals (R-106, §7.4) | 6 |

## How to pick up a phase

1. Read the phase file. It lists prerequisites, tasks, the requirements in scope, and a *Done when*
   condition that is not negotiable.
2. Read the design sections it cites, plus `../../CLAUDE.md` §2 (invariants) if you have not.
3. Check `open-decisions.md` for anything unresolved that your phase depends on. **If a phase depends
   on an open decision, raise it — do not resolve it silently.**
4. Work the tasks. Write the `TestR###_…` acceptance tests as you go, not at the end.
5. Report against the *Done when* condition explicitly, including any part you did not meet.

## Before you start a phase

**Nothing is blocking; phase 0 can start.** Check [`open-decisions.md`](open-decisions.md) for
unresolved questions your phase depends on, and
[`design-gaps.md`](design-gaps.md) for requirements the design does not yet address. Two of the gaps
there want answers before phases 1 and 8 respectively.

## What "done" is not

A phase is not done because its tasks are checked off. It is done when its *Done when* condition holds
and `make check` passes. Phases 4, 5, 6, and 9 each end in one of the four end-to-end sequences from
`../design/07-sequences.md`, and those sequences are the acceptance criteria for v1 as a whole.

## Ordering notes

- **Phase 7 depends on phase 4, not on 5 or 6.** The reconciler needs something deployed to reconcile,
  not a proxy or a detector. It can run in parallel with 5 and 6.
- **Phase 10 is last deliberately.** Traefik is the second implementation of the routing interface,
  and building it is the test of whether that abstraction holds. If Traefik requires changing the
  interface, the interface was wrong — better to learn that in phase 10 than to assume it in phase 3.
  Sketch the Cloudflare adapter on paper during phase 3, before the interface is fixed.
