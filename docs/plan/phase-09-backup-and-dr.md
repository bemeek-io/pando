# Phase 9 — Backup and disaster recovery

**Goal:** the flow nobody tests until they need it. That is why it is one of the four sequences.

**Prerequisites:** phase 7.

**Design:** `../design/07-sequences.md` D; `../design/02-data-model.md` §2.8.

## Tasks

- [ ] Rolling backups per app, retained per `Retention.BackupDaily` (R-211)
- [ ] On-delete backups, `retain_until IS NULL`, kept until explicitly discarded (R-204)
- [ ] DR bundle: `pg_dump`, the local secrets encryption key, adapter configs, host policy, app
      volumes, provisioned services (R-212)
- [ ] Manifest: object counts, checksums, versions — this is what restore verifies against (R-215)
- [ ] Encryption under an operator-supplied passphrase, **never a key stored on the host** (R-213)
- [ ] Restore: decrypt → **verify against manifest** → confirm destructive intent → restore → let the
      reconciler converge
- [ ] `POST /backups/{id}:verify` (R-216)
- [ ] GC integration: aggregate disk budget (R-224)

## Requirements in scope

R-204, R-205, R-211–R-216, R-224.

## Done when

**Sequence D passes**, including rejection of a tampered bundle **with the target untouched.**

## Traps

- **Verify before touching anything.** A truncated or tampered bundle is rejected at the verify step
  with the target install completely untouched. Not "mostly untouched."
- A wrong passphrase fails at decrypt and **reveals nothing about bundle contents.**
- **The passphrase is never persisted.** If the operator loses it, the bundle is unusable (R-214) —
  an accepted cost, and the console must say so **at creation**, not in documentation.
- After restore, the assertion signing key is the original, so apps that cached JWKS still verify.
- After restore, every app returns to its pinned spec **without human intervention** beyond the
  restore itself.
- **[O-14] is unresolved:** if Pando's own Postgres runs as a container on the runtime adapter,
  restore must start Postgres before it has a state store telling it how. The restore path needs a
  bootstrap mode reading adapter configuration from the bundle itself. This is coupled to O-11 —
  resolve them together.
