# Pando documentation

Three kinds of document, and the distinction between the first two is load-bearing.

| | Path | Authority | Churn |
|---|---|---|---|
| **Requirements** | [`requirements.md`](requirements.md) | What Pando *is*. 207 requirements, IDs `R-###`. | Slowly |
| **Design** | [`design/`](design/) | How it is built. Ten documents, `00`–`09`. | Every sprint |
| **Plan** | [`plan/`](plan/) | What to build next, in what order, and what is still unresolved. | Continuously |
| **Traceability** | [`traceability/`](traceability/) | Generated. Which requirements are designed, planned, and proven. | On demand |
| **Reference** | [`api.md`](api.md), [`cli.md`](cli.md), [`mcp.md`](mcp.md) | Generated from the code by `make reference`. What the binary actually serves. | Every change to a surface |

**When design contradicts a requirement, the requirement wins** — or the requirement gets amended in
the same change. Never a silent divergence.

Tags mean the same thing in every document: **[D]** decided, **[P]** proposed, **[O]** open.
Requirement IDs are stable; section numbers are not. Cite `R-###`.

## Reading order

**New engineer:** requirements §1–3 (what it is, non-goals, invariants) → `design/01-spec-schema.md`
(the spec is the center of the system; everything else is downstream of getting it right) →
`design/07-sequences.md` (four end-to-end flows) → whatever you are building.

**Touching authorization or the proxy:** `design/06-authorization-and-proxy.md` in full, then
Sequence C in `07`. Do not skip.

**Writing an adapter:** `design/03-adapter-interfaces.md`, then the relevant section of `01`.

**Picking up work:** [`plan/README.md`](plan/README.md), then the phase file.

If you are an agent working in this repository, read [`../CLAUDE.md`](../CLAUDE.md) first — it carries
the invariants that must not be broken and the definition of done.
