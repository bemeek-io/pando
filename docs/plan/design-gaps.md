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

## Second pass — fragilities, not omissions

A later review looked for choices with the same shape as O-11: several local decisions, each
defensible, that together produce an unstated global property. Three turned up. All are closed.

**Four revocation windows that nobody added up.** Session validity was checked per request, group
membership cached 60s, assertions lived 120s, long-lived connections were never re-checked, and each
component's documentation implied its own delay was the answer. The effective window is the *largest*
of them, and nothing computed it.

*Resolved:* design 06 §3.1. One number — 120 seconds — with the long-lived-connection interval set to
exactly the assertion lifetime rather than an independently chosen value, because two clocks measuring
the same thing drift apart the first time someone tunes one. The console states the window wherever
access is revoked; implying revocation is instant was the real failure mode. **This resolved O-13 as a
consequence.**

**`markUnobservable` had nowhere to live.** The reconciler was specified to flag an app whose adapter
is unreachable — correctly treating it as a platform problem rather than app failure — but no such
field existed in the schema, and `apps.state` had no value for it.

*Resolved:* `apps.unobservable_since`, a third field alongside `state` and `desired_state`, mirroring a
split the design already uses. Folding it into `state` would mean either reporting `running` for an app
nobody can see, or inventing an `unknown` state every consumer then has to handle. The console renders
it as a banner over the last known state.

**R-193 had no mechanism.** "Rotation implies a restart" — but the reconciler detects drift by
comparing desired against *observed*, and `ObservedWorkload` carries no environment. Nothing could see
that a running workload held a stale secret.

*Resolved:* `apps.applied_env_fingerprint`, compared state-side, listed in the reconcilable-drift set
in design 05 §2.1. The fingerprint hashes `(key, version)` pairs and literal values, **never secret
values** — hashing those would put a verifier for every secret in the state store, which is worse than
not having the feature. Adding environment to `ObservedWorkload` was the alternative and would have
required every runtime adapter to read back resolved environment, which is exactly the secret-bearing
data the adapter interface works to keep out of adapters' hands.

## Still open, tracked elsewhere

Three questions remain, none blocking: **O-4** (slot detection — has a `[P]` answer awaiting
measurement), **O-5** (TLS issuance — genuinely per-adapter), and **O-6** (which backup destinations
ship — provider-shaped; the design half, that a destination is *not* an adapter category, is settled in
design 03 §8.1). See [`open-decisions.md`](open-decisions.md).

## How to keep this file honest

Regenerate the index (`make requirements-index`) after any design change, then re-read the orphan list
at the bottom of it. A requirement that leaves the orphan list because someone cited its ID in passing
has not necessarily been designed — and one that stays in it has not necessarily been missed. The
index narrows where to look; it does not do the reading.

One known limitation, now handled: the design docs cite adjacent requirements as `R-070/071`, and the
generator originally matched only the first half, reporting the second as undesigned. If a new compound
citation format appears, the generator needs teaching.
