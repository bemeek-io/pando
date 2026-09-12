# Phase 9 — Backup and disaster recovery

**Goal:** the flow nobody tests until they need it. That is why it is one of the four sequences.

**Prerequisites:** phase 7.

**Design:** `../design/07-sequences.md` D; `../design/02-data-model.md` §2.8.

## Tasks

- [x] DR bundle: `pg_dump`, the local secrets encryption key, adapter configs, host policy, app
      volumes (R-212). `SnapshotVolume`/`RestoreVolume` were stubs returning "not available yet" —
      implemented on the Docker adapter as a throwaway helper container with the volume mounted and
      tar on stdio, no network, all capabilities dropped
- [x] Manifest: object counts, checksums, versions — what restore verifies against (R-215)
- [x] Encryption under an operator-supplied passphrase, **never a key stored on the host** (R-213).
      argon2id at 256 MiB into chunked ChaCha20-Poly1305. The last-chunk marker in the nonce is what
      makes truncation detectable: without it a bundle cut at a chunk boundary decrypts perfectly
- [x] Restore: decrypt → **verify against manifest** → confirm destructive intent → restore
- [x] `POST /backups/{id}/verify` (R-216) — its own route, not a flag on restore
- [x] **Backup is the eighth adapter category** (R-252, R-217). This **reverses** design 03 §8.1,
      which is rewritten with the reversal and what the old argument got right. O-6 resolved: `local`
      ships, anything else is an ordinary adapter
- [x] Console screen, with R-214 stated in the dialog above the passphrase field — not a tooltip,
      not documentation
- [ ] Rolling backups per app, retained per `Retention.BackupDaily` (R-211). The schema, the
      retention column and the `Expired` query are here; nothing schedules them yet, so every backup
      today is taken by hand
- [x] On-delete backups (R-204, R-205). Deleting an app with storage takes a final backup, kept
      until explicitly discarded. A failed backup refuses the delete and says how to proceed anyway.
      Encrypted under the install's own secrets key rather than a passphrase — R-213 governs the DR
      bundle and its reasoning (a fresh machine cannot unwrap the dead machine's keys) does not apply
      to an in-place app restore (R-206)
- [ ] **Restoring** a per-app backup. Creation works and the bundle holds everything a restore needs —
      spec and volume data — but nothing reads it back yet. R-206 says in-place only, matched by
      Pando's own identity for the app, and that matching is the work
- [ ] Provisioned services in the bundle (R-212). Volumes are in; services are not
- [ ] **A deleted app's Docker volume is never reclaimed.** With `backup=true` the data is safely in
      a bundle and the row is removed, but the volume itself stays on disk with nothing referencing
      it. Leaving data is the safe direction and disk is the lesser evil, so this is recorded rather
      than guessed at — but it is a leak, and it is the same shape as the bundle-teardown one
- [ ] GC integration: aggregate disk budget (R-224). Blocked on the same gap as O-16

## Requirements in scope

R-204, R-205, R-211–R-216, R-224.

## Done when

**Sequence D passes**, including rejection of a tampered bundle **with the target untouched.**

**Met.** Nine acceptance tests against the shipped stack (`sequence_d_backup_test.go`): a bundle
carrying all four parts with checksums; a wrong passphrase that reveals nothing and reports a
*decrypt* failure rather than a damaged bundle; an unconfirmed restore that changes nothing; a
tampered bundle — damaged at the destination, mid-file, so its length is still right — rejected with
a witness account created after the backup still present; a restore that returns the install to its
backed-up state; and the restore recorded in the install it produced.

Plus 14 unit tests on the format itself, where a mistake is worst: truncation at a chunk boundary,
tampering anywhere, reordered chunks, trailing bytes, a downgraded KDF header, and every manifest
failure.

Three things running it found that reading it would not:

- **`adapter_configs` has a CHECK enumerating the categories.** R-252 gaining an eighth had to reach
  the schema, or backup is a category no adapter can be configured in.
- **Restoring erased the record of the bundle being restored from.** A backup cannot contain the row
  describing itself, so that row vanishes with the restore — one use and the bundle became
  unlistable and unrestorable. `Record` is idempotent now and the handler puts the row back.
- **The DR bundle had no app data in it.** Volumes are in the bundle format, `SnapshotVolume` works
  and was tested at the adapter level — but the query that lists volumes to snapshot read a table
  nothing ever wrote to. Marked done in this phase on the strength of the plumbing, and false in
  practice until phase 10's debt pass. A test now asserts a real bundle contains a real volume.
- **pg_dump refuses a server newer than itself.** Alpine 3.20 stops at `postgresql16-client` and the
  server is 17, so the runtime base moved to 3.21. Bump it with the postgres service, never
  separately.

## Traps

- **Verify before touching anything.** A truncated or tampered bundle is rejected at the verify step
  with the target install completely untouched. Not "mostly untouched."
- A wrong passphrase fails at decrypt and **reveals nothing about bundle contents.**
- **The passphrase is never persisted.** If the operator loses it, the bundle is unusable (R-214) —
  an accepted cost, and the console must say so **at creation**, not in documentation.
- After restore, the assertion signing key is the original, so apps that cached JWKS still verify.
- After restore, every app returns to its pinned spec **without human intervention** beyond the
  restore itself.
- **Confirmed for O-14:** nothing in the restore path needs adapter configuration before the database
  is available. The destination adapter is resolved from the *current* install's configuration to
  read the bundle, and everything the bundle configures is applied after the database is in place.
  Ordinary sequencing, as predicted.

- **[O-14] is largely dissolved.** It existed only under the bundled-container option, where restore
  would have had to start Postgres before having a state store to say how. O-11 resolved to Compose
  supplying the database, so restore writes into a Postgres the topology already brought up. Confirm
  during this phase that nothing else in the restore path needs adapter configuration before the
  database is available; if something does, that is ordinary sequencing, not a paradox.
