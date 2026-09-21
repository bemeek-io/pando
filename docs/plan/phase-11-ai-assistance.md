# Phase 11 — AI assistance

**Goal:** an install that configures an AI adapter deploys more repositories on the first try, and one
that does not is exactly as good as it was. R-106's first function: screening the proposal detection
produced, and amending what it missed.

**Prerequisites:** phase 6. Screening runs at the end of Sequence A and has nothing to screen before it.

**Design:** `../design/09-ai-assistance.md`, then `../design/07-sequences.md` A and
`../design/01-spec-schema.md` §2.3–2.5.

## Tasks

- [x] The ninth adapter category, `ai`, with `AIAdapter`, `AICapabilities` and the function list as
      data (R-258, R-259)
- [x] The closed amendment set in `adapter/api` — nothing in it says policy, isolation, adapters,
      routing, resources, egress, grants or a secret (R-312)
- [x] `core/screening.Apply`: evidence checked to exist in the source, observations not overwritten,
      a person's values not overwritten, slots and secrets not written over, refusals recorded
      (R-313, R-314)
- [x] Screened provenance on env, ports and volumes, so the review shows it and `spec.Carry` does not
      preserve it as a person's choice (R-314)
- [x] Screener-added slots are `Required` only on a crashed trial run — O-4's fallback, unchanged
- [x] Answers to detection's questions through detection's own answer machinery, with screened
      provenance (R-318)
- [x] Screening wired into `core/detection.Runner` after the install's defaults; every failure a
      skipped outcome, never an error (R-315)
- [x] `detection.screen` audit event naming adapter, model and files read (R-317)
- [x] `DisableAIScreening` in host policy, checked before the adapter is reached (R-316)
- [x] The Anthropic adapter: official Go SDK, `claude-opus-5` default, `list_files`/`read_file`
      against a budgeted reader, `submit_findings` with `strict: true` (R-319)
- [x] Outcome stored on the proposal; console types regenerated
- [x] The review section in the console (`console/src/admin/Screening.tsx`): the model, each change
      with its reason and the files it cites, the files read, notes, and what Pando refused. Silent when
      no AI adapter is configured; one line naming the reason when screening was skipped for any other
- [x] Configurable through the existing `POST /api/v1/adapters` with `category: "ai"`, applied at
      restart like every other adapter; `api_key_env` keeps the key out of the database
- [x] Credentials encrypted at rest (O-20, resolved): write-only `credentials` sealed by the secrets
      adapter into `adapter_credentials`; a check constraint refuses them in plain configuration
- [ ] The corpus measurement below

## Requirements in scope

R-106, R-190 (for adapter credentials), R-258, R-259, R-310–R-319. O-20 is resolved here.

## Done when

**The detection corpus reports first-deploy success with and without screening, and screening does
not lower it on any repository.** R-103's questions-per-deploy is the second number, and it must not
rise.

**Not done.** Every requirement in scope has a named acceptance test and `make check` passes, but the
measurement that says whether screening does what it is for has not been taken. It needs a real
credential and the corpus run (`make detection-corpus`) extended to deploy each proposal rather than
stop at detection. Until then the claim is that screening is safe — every rule in design 09 §3 is
enforced and tested — and not yet that it helps.

## Traps

- **The whole-spec return.** Somebody will propose that `ScreenPlan` return an amended `AppSpec`
  because it is simpler. It is simpler, and it lets a model's output express an isolation floor. The
  closed set is the mechanism; a denylist in core is not (design 09 §3).
- **A screener marking slots required.** Turns "might not start" into "cannot be deployed". O-4's
  fallback already has the right answer; do not relax it for screening.
- **Screening in `internal/detect`.** It would make the auction non-deterministic and put an audit
  write where R-027 cannot see it. It belongs in `core/detection`.
- **Treating a refusal as a model error.** Refusals are the feedback loop for the prompt. Read them.
