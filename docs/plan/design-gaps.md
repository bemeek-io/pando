# Design gaps

Found by reading `../traceability/requirements-index.md` against the design documents. These are
**distinct from `open-decisions.md`** — an open decision is a question the design deliberately left
unanswered and tracked; a gap is a requirement that nobody noticed was unaddressed.

Seventy-four requirements carry no design reference. Most are correctly uncovered — philosophy
(R-002), non-goals (R-010–R-016), `[LATER]` (R-290+), licensing (R-300+) — or are covered in substance
by a design section that simply does not cite the ID. The list below is what survived that filter.

## Needs an answer before the phase that builds it

**R-184 — the egress verb does not exist.** R-184 says defining an app-level egress allowlist is
gated by a verb so an admin can restrict it. The verb catalog in design 06 §5 has twelve verbs and
none of them is that one. Needs a name (`app.egress.override` would match the existing
`app.routing.override` / `app.resources.override` pattern) and a decision about which built-in roles
hold it. **Wanted before phase 1**, because built-in roles are seeded by migration and changing them
afterward is itself a migration (R-081).

**R-171 — the double-login experience has no console treatment.** An app with its own login page is
stacked behind Pando's auth, so the user sees two login screens. R-171 is tagged `[P]` and says this
is expected and not remediated, consistent with R-028 (Pando does not rewrite app behavior). But
nothing says whether the *console* mentions it — at share time, at deploy time, or not at all. For an
audience that may not know what a port is, two consecutive login screens reads as a broken app.
**Phase 8.** A warning code alongside the existing `WARN_*` set is the cheap answer.

## Worth a paragraph, not a decision

**R-164 / R-165 — the two topologies are described in requirements and nowhere in design.** Proxy mode
(one hostname, one certificate, one firewall rule, all apps behind a Pando login) versus per-hostname
(apps have their own hostnames, Pando invisible except at login). The *mechanism* exists —
`RoutingCapabilities.DefaultMode`, `Routing.Mode`, `ModeSource` — but no design document says which
topology an install should default to or how the console explains the choice. R-164 calls proxy mode
"the recommended enterprise topology because it is far easier to get approved than N public
hostnames," and that recommendation currently lives only in the requirements. **Phase 3 sets the
adapter defaults; phase 8 surfaces the choice.**

**R-116 — building inside a runtime-provided isolated environment.** Where a runtime adapter can
provision an isolated environment per app, building inside it is preferred, since build isolation
comes free from the same boundary. No design home. **Not v1-blocking** — it only becomes real when a
VM-class runtime adapter exists, and v1 ships Docker only. Worth a line in design 03 §3 so the next
person does not rediscover it.

## Already tracked elsewhere

These show as undesigned because the design *deliberately* did not settle them. They are in
`open-decisions.md` and need no separate action here: R-133 (O-4, required vs optional slots), R-217
(O-6, backup destination), R-257 (O-8, runtime adapter swap), R-266 (O-9, share notifications).

## How to keep this file honest

Regenerate the index (`make requirements-index`) after any design change, then re-read the orphan
list at the bottom of it. A requirement that moves out of the orphan list because someone cited its ID
in passing has not necessarily been designed — and one that stays in it has not necessarily been
missed. The index narrows where to look; it does not do the reading.
