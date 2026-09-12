# Open decisions

Fifteen questions. O-1 through O-10 come from requirements §23; O-11 through O-14 were added during
design; O-15 was found while implementing phase 6. **Eleven are resolved. Four remain, none
blocking.**

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

**O-13 — one clock, not two.** The re-authorization interval is *exactly* the assertion lifetime
rather than an independently chosen value. Two clocks measuring the same thing drift apart the first
time someone tunes one. See design 06 §3.1.

## Adding one

If you hit a question the docs do not answer, add a row here rather than deciding in code. Include the
question, what depends on it, the options you can see, and which requirement or design section would
need to change for each. A question recorded with its options is most of the work of answering it.
