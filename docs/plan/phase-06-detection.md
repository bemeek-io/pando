# Phase 6 — Detection

**Goal:** a non-technical user points Pando at a repo and gets a working proposal. This is where R-005
(the deployer may not know what a port is) is honored or lost.

**Prerequisites:** phase 5.

**Design:** `../design/07-sequences.md` A; `../design/03-adapter-interfaces.md` §3;
`../design/01-spec-schema.md` §2.5; `../design/04-api.md` §2.2.

## Tasks

- [ ] Read-only `SourceView` — no write methods, structurally (R-020)
- [ ] Source allowlist check **before clone** (R-092); a blocked source produces zero disk writes
- [ ] Registry check tier (R-094)
- [ ] The detector auction: every builder adapter `Bid()`, ranked (R-093)
- [ ] Confidence ladder; `runners_up` returned so the user can see the auction, not just a verdict
- [ ] Compose import, with rejected constructs raising `PLAN_COMPOSE_CONSTRUCT_REJECTED` (R-099) and
      rewritten ones raising `WARN_COMPOSE_CONSTRUCT_REWRITTEN`
- [ ] Trial run in throwaway isolation (R-097): observe bound ports, observe writes outside declared
      volumes, capture crash logs
- [ ] Slot promotion from the trial run — the O-4 fallback (design 01 §2.5)
- [ ] Question generation held to R-105
- [ ] Warnings: `WARN_NO_PERSISTENT_VOLUME` with the observed directory (R-201, R-202),
      `WARN_PATH_ROUTING_INCOMPATIBLE` (R-168)
- [ ] Detection endpoints: get, rerun, diff, answers, accept

## Requirements in scope

R-020, R-021, R-092–R-099, R-101–R-107, R-131, R-132, R-168, R-201, R-202.

## Done when

**Sequence A passes against a set of real public repos** — a Dockerfile app, a compose stack, a static
site, a Node app with no deployment artifacts, and a monorepo.

## Traps

- **Build the corpus of ~30 real repos early**, not at the end. Detection quality below the R-103 bar
  is the top risk in the register, and "questions per deploy" is a tracked metric, not a vibe.
- **R-105 is a hard content requirement, not a style note.** A question must be answerable by a model
  that cannot see the repo, because the intended workflow is pasting it into the assistant that wrote
  the app. "Which port?" fails review. Enforce this on every generated question.
- A blocked source must produce **zero disk writes** — `git clone` is never invoked. Test it.
- Ports discovered by observation carry `Source: "observed"`, not `"framework"`. The distinction is
  what the review UI shows the user.
- **Accepting a proposal does not deploy.** It pins revision 1 and writes two grants.
- Pando still does not act on suspected undeclared dependencies (R-021, R-107) —
  `WARN_UNDECLARED_DEPENDENCY_SUSPECTED` is informational only.
- **O-4 (required vs optional slots) is unresolved.** The trial-run promotion fallback is the [P]
  answer; measure its false-block rate against the corpus rather than assuming it works.
