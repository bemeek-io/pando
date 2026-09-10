# Design gaps

Found by reading `../traceability/requirements-index.md` against the design documents. These are
**distinct from `open-decisions.md`** — an open decision is a question the design deliberately left
unanswered and tracked; a gap is a requirement that nobody noticed was unaddressed.

Of the ~74 requirements carrying no design reference, most are correctly uncovered — philosophy
(R-002), non-goals (R-010–R-016), `[LATER]` (R-290+), licensing (R-300+) — or are covered in substance
by a design section that simply does not cite the ID. Four survived that filter. **All four are now
closed.**

## Closed

**R-184 — the egress verb did not exist.** R-184 requires that defining an app-level egress allowlist
be gated by a verb so an admin can restrict it. The verb catalog had twelve verbs and none was that
one, so the requirement had no mechanism.

*Resolved:* `app.egress.override` added to the catalog (design 06 §5) and to R-080's table. It joins
`app.routing.override` and `app.resources.override` as the third "deviate from a default the host
operator chose" verb, and like those two it sits with **Owner only** — not Operator. This matters
beyond tidiness: an app-level allowlist *replaces* the install-wide list rather than narrowing it
(R-182, R-183), so defining one is an escalation. Ungated, the install-wide list would be advisory.

**R-171 — stacked logins had no console treatment.** An app with its own login page sits behind
Pando's auth and the user sees two login screens.

*Resolved: no warning.* An app presenting its own login page is that app working correctly, and Pando
has no basis for calling a working app a problem. R-171 is promoted `[P]` → `[D]` with that rationale,
and design 08 §1.3 now states the general rule the whole warning set is held to: a warning describes
something that will bite the user later, not something that merely looks unusual. Warnings that fire
on correct behavior are how users learn to dismiss the ones that matter.

**R-164 / R-165 — the two topologies were described in requirements and nowhere in design.**

*Resolved:* design 03 §4.1. Proxy mode (one hostname, one certificate, one firewall rule) versus
per-hostname (apps have their own hostnames, Pando invisible except at login). Neither is a global
setting — topology is the aggregate of each app's `Routing.Mode`, and an install can mix them. What
makes an install feel like one or the other is the routing adapter's `DefaultMode` (R-162), which is
why that field exists and why deviating from it is gated. The choice is not surfaced during setup
(R-005, R-104); `ModeSource` records whether a mode was inherited or chosen.

**R-116 — building inside a runtime-provided isolated environment had no design home.**

*Resolved:* design 03 §3, tagged `[P]`. Not v1 work — v1 ships a Docker runtime whose isolation class
is `container`, so BuildKit supplies the build boundary independently. The note exists so that
whoever writes the first VM-class runtime adapter considers borrowing that boundary before building a
parallel one.

## Still open, tracked elsewhere

These show as undesigned because the design *deliberately* did not settle them. They live in
[`open-decisions.md`](open-decisions.md) and need no action here: R-133 (O-4, required vs optional
slots), R-217 (O-6, backup destination), R-257 (O-8, runtime adapter swap), R-266 (O-9, share
notifications). **O-11 is resolved and nothing is blocking** — see [`open-decisions.md`](open-decisions.md).

## How to keep this file honest

Regenerate the index (`make requirements-index`) after any design change, then re-read the orphan list
at the bottom of it. A requirement that leaves the orphan list because someone cited its ID in passing
has not necessarily been designed — and one that stays in it has not necessarily been missed. The
index narrows where to look; it does not do the reading.

One known limitation, now handled: the design docs cite adjacent requirements as `R-070/071`, and the
generator originally matched only the first half, reporting the second as undesigned. If a new compound
citation format appears, the generator needs teaching.
