# Open decisions

Seventeen questions. O-1 through O-10 come from requirements §23; O-11 through O-14 were added during
design; O-15 through O-17 were found while implementing phases 6, 7 and 8. **Twelve are resolved. Five
remain, none blocking.**

**These are not TODOs to resolve at your discretion.** An agent hitting an open one should raise it,
state which options the docs already identify, and stop — not pick quietly and move on. Record any
resolution both here and in the requirements or design doc that owns it.

## Still open

| ID | Question | Why it stays open | Needed by |
|---|---|---|---|
| **O-4** | Required vs optional slot detection — the forty-key `.env.example` problem | Has a `[P]` answer that needs measuring, not deciding | Phase 6 |
| **O-5** | TLS issuance — ACME, wildcards, self-signed local | Genuinely per-adapter; each routing adapter answers it for itself | Per adapter |
| **O-6** | Which backup destinations ship — local, S3, mounted share | Provider-shaped. The *design* half is settled: a destination is not an adapter category (design 03 §8.1) | Phase 9 |
| **O-15** | How a host port is chosen in port-mode routing | Nothing in the requirements says. Has a `[P]` answer in code | Phase 6 (shipped), revisit at phase 10 |
| **O-16** | How log retention is actually enforced (R-222–R-224) | The requirement is clear; no mechanism exists to carry it out | Phase 7 (deferred), needed before an install runs many apps |

**O-4** has a `[P]` fallback that preserves R-103: default `Required: false` for anything not typed to
a known service, and let the trial run settle it — a slot whose absence crashes the trial run is
promoted to required with the crash log as evidence. This turns an unanswerable question into an
observation. It is open because it needs a false-block rate measured against the detection corpus, not
because nobody has decided.

**O-15** was found by asking whether a detected app could actually deploy. It could not: the spec had
no routing, and filling that in surfaced the question nobody had answered.

R-166 prefers subdomain and falls back to path. The `loopback` adapter that ships as the laptop
default supports **neither** — it is port mode only (design 03 §4.1). So on a default install every
app needs a host port, and no requirement says where one comes from. R-005 rules out asking the user:
someone who may not know what a port is cannot pick a free one.

`[P]` implemented: the lowest free port in a configured range, default `9000-9999`, held in a
`port_allocations` row keyed `(adapter_ref, port)`. Exhausting the range returns
`CAPACITY_NO_FREE_PORT` naming the setting to widen.

**Half of this question is now answered, by being got wrong.** The first version derived the port —
"the lowest number no pinned spec is using" — and that is not an allocation, it is a guess about one.
It raced immediately, and in the ordinary case rather than an exotic one: adding five apps at once
runs five background detections, two computed the same answer before either had written anything
down, and both were handed port 9001 with nothing anywhere noticing. So a port **is** a durable
allocation with its own table, and the unique constraint is what makes a collision impossible rather
than unlikely.

Lowest-free rather than random so an app tends to keep its port across a rebuild and a bookmark keeps
working; reused rather than ever-increasing so a deleted app's port comes back. What needs deciding is
whether lowest-free is the rule and whether `9000-9999` is the right range. Traefik (phase 10) does subdomain and path, so an install
using it never reaches this path at all; that is the reason it is not blocking.

**O-16** was found implementing phase 7's garbage collection. R-222 bounds log retention by size,
R-223 sets 100 MB per app, and R-224 says the aggregate must respect total host disk. The GC job was
meant to enforce all three. It cannot, because **Pando does not hold app logs** — it streams them from
the runtime through `RuntimeAdapter.Logs`, and the bytes live wherever that runtime put them.

There is no `TrimLogs` on the adapter interface, and adding one is not obviously right either. On
Docker the honest mechanism is the log driver's own `max-size` / `max-file`, set when the container is
created — which makes the per-app cap (R-223) easy and the aggregate (R-224) hard, because scaling
every app's cap down proportionally when the total exceeds the disk budget would mean **recreating
every container**. The reconciler may not do that: it is destruction of something a person may have
wanted, on a schedule, triggered by an unrelated app being chatty.

Options, none free:

1. **Per-app cap at creation, aggregate as a warning only.** Honest and cheap; R-224 becomes a
   notification rather than a guarantee, which is a real weakening of a `[D]` requirement.
2. **A `LogRetention` capability on the runtime adapter**, applied without recreating where the
   runtime allows it. Docker does not allow it for an existing container; another runtime might.
3. **Pando collects logs itself** into storage it controls, which makes both requirements trivially
   enforceable and adds a durable, secret-bearing store R-225 currently reasons about not needing.

Recorded rather than decided. Spec revision pruning (R-152) is implemented — it is the part of GC
that operates on data Pando actually owns.

**O-5** is open the way `SessionPolicy` is open: deferring it to each adapter *is* the answer (R-047's
shape). A routing adapter that issues certificates declares how; one that cannot says so through
`RoutingCapabilities`.

## Resolved

| ID | Question | Resolution | Where |
|---|---|---|---|
| **O-1** | Identity linking across adapters | Not in v1. When it lands, linking **aliases and never merges** — `users.id` is never retired | design 02 §2.1 |
| **O-2** | Per-adapter session lifetime and revocation | Deferring to each adapter's `SessionPolicy` *is* the answer (R-047) | design 03 §5 |
| **O-3** | Private repo credential ownership | App-owned, attributed to the supplier in audit; offboarding flags rather than breaks | design 01 §2.1 |
| **O-7** | Exec command recording | Command recorded at open; PTY stream not captured | design 03 §2.3 |
| **O-8** | Runtime adapter swap under a running app | Neither migration nor plain redeploy — a `destructive` spec change | design 01 §4, R-257 |
| **O-9** | Share notifications | No message; the launcher tile is the notification | design 08 §1.1, R-266 |
| **O-10** | Retroactive policy application | Running apps untouched; next deploy fails at plan time | design 05 §3 |
| **O-11** | How Postgres is supplied | The install topology supplies it — Compose, with an external-database override | design 00 §1.1 |
| **O-12** | MCP exclusion list hard or policy-controlled | Policy-controlled, default-closed, expressed as host policy — not a second mechanism | design 04 §3 |
| **O-13** | Session revocation mid-websocket | Re-authorize on the assertion lifetime; close on failure | design 06 §4.2 |
| **O-14** | DR restore bootstrap ordering | Largely dissolved by O-11; confirm sequencing in phase 9 | design 07 D |
| **O-17** | What an "administrative verb" is (R-265) | Install-scoped verbs, held as a grant with no app; a fourth built-in role | design 06 §2.1, R-080/R-081 |

### O-17 in full, because it was a live escalation

**O-17 — resolved: option 1, install-level verbs as grants with no app.**

It was not only a console gap. Six endpoints were gated by "are you signed in" and nothing else,
because there was no install-level authority to gate them with, and three of those mutated. It was
demonstrated end to end on the shipped stack: create an ordinary user, sign in as them, `PATCH` the
administrator to `suspended`, and the administrator's next login returns 401. Two calls, no grants
needed. `GET /apps` correctly returned nothing for that user the whole time — the per-app
authorization worked exactly as designed, which is what made the gap so easy to miss.

What shipped:

| Piece | Where |
|---|---|
| Six install verbs — `install.view`, `install.users.manage`, `install.policy.manage`, `install.adapters.manage`, `install.audit.read`, `app.create` | R-080, `internal/core/authz/verbs.go` |
| A fourth built-in role, **Administrator**, install-scoped, holding all six and no app verb | R-081, migration `000009` |
| `grants.app_id` nullable, with the scope correspondence enforced structurally | design 06 §2.1, migration `000009` |
| `CheckInstall`, a third check function beside `CheckControl` and `CheckData` | design 06 §2.1 |
| Bootstrap grants the first account the Administrator role | R-046 |
| The six endpoints gated; `/users/{id}` **self or verb** | design 04 §2.7, §2.8 |
| `GET /me` returns the caller's install verbs; the console reads them | design 04 §2.9, R-265 |

Two things about the resolution are worth keeping in mind:

**The `app_id NOT NULL` guarantee was replaced, not dropped.** That column was doing real work — it is
why an app-scoped grant could not accidentally become global. In its place: `roles.scope`,
`grants.role_scope`, a composite foreign key between them, and a CHECK tying `role_scope` to whether
`app_id` is null. So "no app" and "carries install verbs" cannot come apart, whatever the application
does. A third CHECK says a data grant always names an app, because R-070's binary use has no
install-wide form and that was previously implied by the NOT NULL.

**`PATCH /users/{id}` is self-or-verb, not verb-only.** Your own account is self-service; anyone
else's needs `install.users.manage`. Both halves are load-bearing, and the first is what keeps an
install with one administrator from being an install where nobody can manage their own account.

What is still not built is install-level **screens** — users, host policy, adapters, the audit log.
They have verbs and gated endpoints now; they have no UI. That is phase-9 work and it is recorded as
such in `phase-08-console.md` rather than as an open decision, because nothing is undecided about it.

Related and now unblocked: design 05 §3's promise that the console lists policy-violating apps before
a policy is saved, and R-085's install-wide exec disable, both of which needed this concept.

### The ones worth understanding before you touch that area

**O-1 — linking aliases, it never merges.** Merging two `users` rows is the obvious implementation and
it breaks R-054: `users.id` is the assertion `sub` claim, apps key their data on it, and Pando cannot
reach into an app to rewrite rows stored under the losing ID. A merge silently orphans a person's data
inside every app they ever used. Decided now because getting it wrong later is unrecoverable.

**O-7 — the command, not the stream.** A captured PTY stream is a durable, searchable store of every
secret an operator ever typed, sitting in the one table deliberately readable by anyone with audit
access. It cannot be redacted, because `secret.Value` protects values Pando *handles* and a stream is
bytes Pando never parses. Recording the command answers what an investigation asks first; the
documentation must not imply exec is fully audited (R-086).

**O-12 — an MCP-layer block is a speed bump, not a boundary.** An agent holding a token can call the
REST API directly, so enforcement has to live where every surface passes through it. That is host
policy, which is already evaluated before grants for every principal.

**O-17 — an administrator is a principal with a grant.** Not a flag on a user, which was the cheap
option and the one that would have to be decided again the first time somebody asked for "manages
users but not policy". The cost is that `grants.app_id` is nullable, and the whole design of the
migration is about paying that cost in the schema rather than in `CheckControl`'s branching: a role
carries its scope, a grant carries the scope it was made at, and a composite foreign key makes the two
agree. Two check functions that each refuse the other's verbs keep the call sites honest — and the
asymmetry matters, because an app verb evaluated install-wide looks for a grant that *can* exist.

**O-13 — one clock, not two.** The re-authorization interval is *exactly* the assertion lifetime
rather than an independently chosen value. Two clocks measuring the same thing drift apart the first
time someone tunes one. See design 06 §3.1.

## Adding one

If you hit a question the docs do not answer, add a row here rather than deciding in code. Include the
question, what depends on it, the options you can see, and which requirement or design section would
need to change for each. A question recorded with its options is most of the work of answering it.
