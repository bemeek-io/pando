# Phase 7 — Reconciler

**Goal:** drift correction that never destroys anything a human may have wanted, and a failed app that
stays failed.

**Prerequisites:** phase 4. **Not** 5 or 6 — this can run in parallel with them.

**Design:** `../design/05-reconciler-and-lifecycle.md` §1, §2, §4–6.

## Tasks

- [x] The state machine: `draft` → `proposed` → `deploying` → `running` / `degraded` / `stopped` /
      `failed` / `archived`, with `desired_state` kept separate from observed `state`
- [x] The loop: 15s interval [P], 8 concurrent [P], per-app advisory lock so two ticks cannot overlap, plus a panic guard — a panic in one app's reconciliation used to take the process down
- [x] Drift classification — reconcilable vs report-only (design 05 §2.1)
- [x] Stale-environment drift via `apps.applied_env_fingerprint` — this is the only mechanism R-193
      has, because `Observe` returns no environment and deliberately should not. Hash `(key, version)`
      pairs and literal values, **never secret values**
- [x] `markUnobservable`: an adapter being down is a **platform** problem, not app failure. It stamps
      `apps.unobservable_since`, a third field beside `state` and `desired_state` — not a state value,
      and not an `unknown` state; clears on the first successful `Observe`
- [x] Backoff (R-149), capped at 5 minutes
- [x] Give-up threshold: 10 failures in 30 minutes [P] → `failed`, notify, audit, **stop** (R-150) — the window had to become an idle timeout or the threshold was arithmetically unreachable ([note](../design/notes-give-up-threshold-was-unreachable.md))
- [x] Auto-deploy as a **separate scheduled job** (R-141): creates a spec revision and enqueues a
      deployment; skipped, not queued, if a deployment is in flight
- [~] Hourly GC: spec revision pruning that skips ever-pinned revisions — **done**. Log trimming
      (R-223, R-224) is **not**: Pando does not hold app logs and no mechanism exists to trim them,
      recorded as O-16 rather than guessed at. Backup expiry arrives with backups in phase 9

## Requirements in scope

R-140, R-141, R-146–R-152, R-203, R-211, R-221–R-224.

## Done when

Killing a container by hand restores it; **killing it repeatedly reaches `failed` and stays there.**

`test/acceptance/reconciler_test.go` does both against the shipped stack with the real loop running:
`docker rm -f` on a live container and the reconciler brings back a *new* one; a crash-looping app
walks the backoff to the threshold, reaches `failed`, and is still there six minutes later — past
every retry interval the loop has.

Getting there took three bugs that only running it could find: a give-up threshold that was
arithmetically unreachable, a failure counter that measured the wrong thing, and a crash-looping app
being read as recovered in the moment between crashes. All three are written up in
[notes](../design/notes-give-up-threshold-was-unreachable.md).

**Not done:** log retention (O-16), and the aggregate disk budget it implies.

## Traps

- **Pando stopping does not stop apps** (design 05 §2.1.1). On startup the loop finds apps that kept
  running while Pando was away and converges to them — it does not restart them for tidiness, and it
  does not treat "I did not observe this for a while" as drift. An app that was running and is still
  running needs nothing done to it.

- **`failed` is the only state the reconciler refuses to touch,** and R-151 is true because there is
  *no code path* for it — not because a flag is checked. If you add a "retry failed apps after an
  hour" path, you have broken the requirement.
- **The dividing line:** the reconciler may create and start things; it may not destroy anything a
  human may have wanted. Hold that rule when new cases come up.
- A **missing volume that previously existed is not reconcilable.** Recreating it silently produces an
  empty volume and an app that looks healthy while having lost everything (R-203). Report.
- An adapter being unreachable must not increment the app's restart counter. Otherwise a Docker daemon
  restart marks every app on the host as failed.
- `nil` health means **no signal**, which is not unhealthy. An app with no health check is `running`,
  not perpetually `degraded`.
- The failure counter resets only on `running` with health passing. A flapping app that recovers
  between failures still accumulates — flapping is a failure mode.
- **Aggregate log budget wins over per-app caps** (R-224). Per-app retention alone still fills a disk
  with twenty apps.
