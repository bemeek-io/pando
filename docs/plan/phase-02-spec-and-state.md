# Phase 2 — Spec and state

**Goal:** the central object. Detection produces it, humans edit it, the planner consumes it, adapters
translate it, revisions store it, export serializes it. Everything else is downstream of getting this
right.

**Prerequisites:** phase 1.

**Design:** `../design/01-spec-schema.md` in full; `../design/02-data-model.md` §2.3;
`../design/04-api.md` §2.1, §2.3.

## Tasks

- [ ] `AppSpec` types per design 01 §2, with `schema_version`
- [ ] Validation, every rule in design 01 §3, each with its `VALID_*` / `PLAN_*` code
- [ ] The classified differ: `benign` / `restart` / `rebuild` / `destructive` (design 01 §4).
      A changed runtime adapter is `destructive` (R-257, O-8) — not a migration path
- [ ] `apps` and `spec_revisions` migrations, with the append-only trigger
- [ ] Grants wired to app creation — two rows (R-073)
- [ ] App CRUD endpoints, including the `409 STATE_BACKUP_DECISION_REQUIRED` delete shape (R-204/205)
- [ ] Spec revision endpoints: list, get, create, diff
- [ ] Export: spec plus resolved non-secret configuration; secrets named, never valued (R-020)
- [ ] Import lands as a proposal requiring review, `OriginImported`, never a live deployment

## Requirements in scope

R-020, R-026, R-100, R-101, R-120, R-131 (declaration only), R-132 (validation), R-152, R-201–R-205,
R-241, R-261.

## Done when

An app can be created and a spec hand-written, validated, and pinned via the API. **Nothing runs yet.**

## Traps

- **The spec is the sole record** (R-020). Nothing is read from the repo at deploy time.
- **The spec contains no policy, no grants, no secret values.** Policy is evaluated live at plan time
  so R-274 works; grants live separately so sharing doesn't create a revision; a literal slot value is
  stored as a secret so "export is safe to hand to someone" stays true without a special case.
- **Adapters are named by reference, never by vocabulary** (R-251): `runtime: {ref: "rt_docker_local"}`,
  never Docker arguments.
- Volumes are top-level and referenced by mounts, **not nested in workloads** — a volume outlives any
  single workload definition and must survive a revision that renames the workload.
- Exactly one workload has `Primary: true`.
- `Port.Source` is retained because the review UI needs it. "We watched your app bind 3000" reads
  differently from "we guessed 3000 because it's a Next.js app."
- `Egress.Mode == allowlist` **replaces** the install-wide list rather than intersecting it (R-182).
  The field documentation must say so — "allowlist" reads like narrowing, and it is not.
- Warnings live in the spec and survive revisions until dismissed. **A warning is never a blocker.**
