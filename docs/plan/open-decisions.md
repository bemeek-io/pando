# Open decisions

Seventeen questions. O-1 through O-10 come from requirements §23; O-11 through O-14 were added during
design; O-15 through O-17 were found while implementing phases 6, 7 and 8. **Eleven are resolved. Six
remain, none blocking, though O-17 blocks half of R-265.**

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
| **O-17** | What an "administrative verb" is (R-265) | The concept appears in a requirement and exists nowhere in the model — and six endpoints are already exposed without one | **Blocking.** A signed-in user with no grants can suspend the administrator |

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

**O-17 is not only a console gap — it is a live privilege escalation.** Six endpoints are gated by
"are you signed in" and nothing else, because there is no install-level authority to gate them with.
Three of them mutate:

| Endpoint | What a user with no grants can do |
|---|---|
| `PATCH /users/{id}` | **Suspend any user, including the administrator.** Sessions are revoked immediately (R-048), so the install is locked out |
| `POST /users` | Create accounts |
| `POST /apps` | Create apps and consume host capacity — Sequence A step 1 says this checks install-level `app.create`, which does not exist |
| `GET /users/{id}` | Read any user's record |
| `GET /capacity` | Read host sizing |
| `GET /adapters` | Read which adapters are configured and what they support |

Demonstrated end to end on the shipped stack: create an ordinary user, sign in as them, `PATCH` the
administrator to `suspended`, and the administrator's next login returns 401. Two calls, no grants
needed. `GET /apps` correctly returns nothing for that user the whole time — the per-app authorization
works exactly as designed, which is what makes the gap so easy to miss.

**O-17** was found building the launcher. R-265 says "users holding any administrative verb see an
**Admin** entry point from the launcher, exposing the console scoped to whatever privileges they
hold." There is no administrative verb: R-080's catalog is thirteen `app.*` verbs and nothing
install-level, `grants.app_id` is `NOT NULL` so an install-level grant cannot be written, users carry
no admin flag, and the first-run administrator is an ordinary local user. `app.create` — which
Sequence A step 1 calls install-level — is not in the catalog and is checked nowhere.

The half of R-265 that *is* implementable is shipped: the management console opens for someone holding
a control-plane grant on at least one app, scoped by the server to those apps. What needs a decision
is install-level administration — users, hosts, host policy, the audit log — none of which has a verb
and none of which has a screen. Related: design 05 §3 promises the console lists policy-violating apps
before a policy is saved, and R-085 lets host policy disable exec install-wide; both need the same
missing concept.

### The three options

**1. Install-level verbs, as grants with no app.** Add `install.*` verbs to R-080's catalog —
`install.users.manage`, `install.policy.manage`, `install.adapters.manage`, `install.audit.read`,
plus the `app.create` that Sequence A already assumes — and relax `grants.app_id` to nullable so a
grant can be install-scoped. `CheckControl` grows an install-scoped path beside its app-scoped one.

*For:* one authorization model, one table, one function. Custom roles (R-082) compose from the same
catalog, so "can manage users but not policy" costs nothing extra. The audit log already records
grants, so who made someone an admin is answerable.

*Against:* `grants.app_id NOT NULL` is currently doing real work — it is why an app-scoped grant
cannot accidentally become global. Making it nullable moves that guarantee from the schema into a
`CHECK` constraint and into `CheckControl`'s branching. R-070/071's two planes also need saying
explicitly: an install grant is control-plane only, and `grants_role_is_control_plane_only` already
half-says it.

**2. A flag on the user.** `users.is_admin boolean`, set by bootstrap for the first account.

*For:* smallest possible change, and it makes the lockout above impossible today.

*Against:* it is a second authorization mechanism beside grants and roles, which is exactly the shape
R-080 rejected when it chose individual verbs over a role bit — "there is no implication graph"
because graphs are where authorization bugs live, and a boolean is the most implicit graph there is.
It cannot express "manages users but not policy", so R-265's "scoped to whatever privileges they
hold" becomes untrue the moment anyone asks for it. It is a decision that has to be taken again later.

**3. A separate install role.** A distinct role table and grant table for install scope, parallel to
R-081's per-app roles.

*For:* leaves the per-app model completely untouched.

*Against:* two of everything — two role catalogs, two grant tables, two check functions — and the
console has to explain the difference to a person who does not care. It is the option most likely to
drift, because a new verb has to be added in the right one of two places.

### Recommendation

**Option 1.** It is the only one that answers R-265 as written — "scoped to whatever privileges they
hold" needs privileges, plural, and separable. It also costs the least *conceptually*: an
administrator becomes someone with a grant, which is already how everything else in the system
works, and the console's Admin entry becomes "do you hold any `install.*` verb" — one line, in the
one place the question is already asked.

The schema cost is real but contained: `app_id` becomes nullable with a `CHECK` that an install-scoped
grant has no app and an app-scoped one does, which is the same shape as the two constraints already on
that table.

What needs deciding is not really which option — it is **the verb list**. That is a product question
about how finely install administration should divide, and it belongs to whoever owns R-080.

### Containment, independent of the decision

The escalation above should not wait for the verb list. The narrow fix is to refuse the three mutating
endpoints unless the caller is acting on themselves, which is correct under every option:

- `PATCH /users/{id}` — deny unless `id` is the caller's own, until there is a verb for it. Nobody
  loses a capability they legitimately had, because nobody legitimately had this one.
- `POST /users` and `POST /apps` — these genuinely need install authority, so they need a decision or
  an interim gate.

Say the word and I will land the containment on its own, with a test that Bob cannot suspend the
administrator.

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
