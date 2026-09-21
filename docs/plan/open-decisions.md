# Open decisions

Twenty questions. O-1 through O-10 come from requirements §23; O-11 through O-14 were added during
design; O-15 through O-17 were found while implementing phases 6, 7 and 8; O-18 was found while
setting up the release build; O-19 was found by turning `gosec` on; O-20 was found while building the
first AI adapter. **Eighteen are resolved. Two remain, and neither is a design decision** — O-4 needs
a measurement and O-18 needs somebody to pick a host and pay for it.

O-5 was the other long-standing one and is now resolved: "per-adapter" answered it until R-174 made
Pando run the edge and write its configuration, at which point Pando became the thing choosing.

**These are not TODOs to resolve at your discretion.** An agent hitting an open one should raise it,
state which options the docs already identify, and stop — not pick quietly and move on. Record any
resolution both here and in the requirements or design doc that owns it.

## Still open

| ID | Question | Why it stays open | Needed by |
|---|---|---|---|
| **O-4** | Required vs optional slot detection — the forty-key `.env.example` problem | Has a `[P]` answer that needs measuring, not deciding | Phase 6 |
| **O-18** | Where a signed apt repository is hosted, so `apt install pando` works without downloading a file first | Costs money or custody of a signing key; neither is an engineering call | Not blocking — the `.deb` is already published |

**O-4** has a `[P]` fallback that preserves R-103: default `Required: false` for anything not typed to
a known service, and let the trial run settle it — a slot whose absence crashes the trial run is
promoted to required with the crash log as evidence. This turns an unanswerable question into an
observation. It is open because it needs a false-block rate measured against the detection corpus, not
because nobody has decided.

**O-18** exists because a `.deb` attached to a release and an apt repository are different products.
The release build publishes `.deb`, `.rpm` and `.apk` packages, which install with
`sudo apt install ./pando_<version>_linux_amd64.deb` and never upgrade themselves. `apt install pando`
and `apt upgrade` need a repository that apt trusts, and the options differ in who holds the signing
key:

- **A hosted repository** — Cloudsmith has an open-source tier and Gemfury hosts public packages for
  free. Both sign the repository and give users a key to install. GoReleaser publishes to either. The
  cost is a dependency on a vendor for the install path, and an account somebody has to own.
- **Self-hosted on GitHub Pages**, generated with `aptly` or `apt-ftparchive` and signed in CI. Free,
  and the key is ours — which is also the problem, because the key then has to live somewhere, be
  rotated, and survive the person who made it.
- **Neither**, and the `.deb` on the release page stays the answer. Debian and Ubuntu users download
  a file and upgrade by downloading another one.

The choice matters more than it looks: an unsigned repository, or one added with `[trusted=yes]`,
tells every user of a product that argues for provenance to skip checking ours.

**O-20 — where an adapter's credential lives. Resolved:** encrypted by the install's secrets adapter,
in its own table. The Anthropic adapter (design 09 §6) was the first adapter to hold a credential, and
`adapter_configs.config` is plain JSON, stored unencrypted and exported readably into every DR bundle.
Keeping a key there is inconsistent with R-190, so it is not allowed.

- `POST /adapters` takes a separate, write-only `credentials` object. Each value is sealed by the
  install's secrets adapter and stored in `adapter_credentials` as ciphertext, bound to the adapter's
  ID as authenticated data, exactly as an app secret is stored in `secrets`. There is no plaintext
  column.
- The database refuses a `credentials` key in `adapter_configs.config` (a check constraint), and the
  handler refuses the field names credentials usually go by, with a message saying where to send them.
- At startup core configures the secrets adapter first, decrypts each adapter's credentials, and hands
  them to `Configure` in memory under `credentials`. An adapter that finds a key at the top level of
  its stored configuration refuses to start.
- `GET /adapters` lists which credentials are set by name. Nothing returns a value, and the audit
  event names the fields that changed, never their values.
- A secrets adapter cannot be given credentials, since it is what would encrypt them.

`api_key_env` remains for an operator who keeps credentials in the environment; it stores a variable
name. The two alternatives considered were encrypting all of `adapter_configs.config`, which ties every
adapter's non-secret settings to the secrets key, and environment variables only, which the console
cannot set (R-002).

**O-19 — the session cookie's `Secure` attribute. Resolved:** option 3, an explicit
`PANDO_SERVER_EXTERNAL_URL`. The operator states the scheme browsers reach the installation on, and
Pando believes the operator rather than the request. Unset falls back to `r.TLS != nil`, which is
correct for the two topologies where the request tells the truth — Pando terminating its own TLS, and
the plain-HTTP localhost install the README documents. Implemented in
`internal/httpapi.Server.secureCookie`, with the decision table in
`cookie_secure_internal_test.go`; the value is parsed and rejected at startup rather than at the
first sign-in.

Option 2, a trusted-proxy list, was the other real candidate and is a fine answer; it loses on
having two things to get wrong instead of one, and on a wrong trusted-proxy list failing quietly.
Option 1 was never viable and option 4 breaks the README as written. The rest of this section is the
original write-up, kept because the reasoning is why the answer is what it is.

**It was a live weakness, not a tidiness question.** `handleLogin` sets the session cookie with
`Secure: r.TLS != nil` (`internal/httpapi/session_handlers.go`). That is right for the documented
default install — plain HTTP on `localhost`, where an unconditional `Secure` would stop sign-in
working, and where `localtest.me` is not a browser-trustworthy origin either. It is **wrong** for the
other documented topology. `SECURITY.md` says Pando does not terminate TLS and expects a reverse
proxy in front of it; behind one, `r.TLS` is nil on every request, so the session cookie is sent with
no `Secure` attribute even though the browser's connection is encrypted. A single plaintext request
to the Pando hostname — a typed URL, a stale bookmark, an `http://` link — then puts the cookie on
the wire in the clear.

What has to be decided is not *whether* to fix it but **which signal Pando trusts, and when**:

1. **Trust `X-Forwarded-Proto` unconditionally.** One line, and wrong in the same way forged headers
   are always wrong: any client that can reach Pando directly can also set it, and R-053 exists
   precisely because Pando does not treat an inbound header as a fact. Rejected on the same grounds
   Pando strips inbound `X-Pando-*`.
2. **Trust `X-Forwarded-Proto` only from a configured set of trusted proxy addresses.** Correct, and
   the standard answer. Costs a new configuration value, and an install that sets it wrong gets a
   subtle failure rather than a loud one.
3. **An explicit `PANDO_EXTERNAL_URL` (or `PANDO_SECURE_COOKIES`).** The operator states the scheme
   the installation is reached on, and Pando believes the operator rather than the request. No
   trusted-proxy list, one value to get wrong, and it doubles as the base for absolute URLs in
   notifications and redirects — which the system needs anyway. The `[P]` favorite.
4. **Always `Secure`, with an explicit opt-out for local development.** Safe by default, but it
   breaks the install instructions in the README as written, which is the one thing this project
   should not do quietly.

Whichever is chosen, the cookie set in `handleLogout` has to follow it: a clearing cookie whose
attributes differ from the original may not clear it at all.

`gosec`'s G124 finding on both call sites is still suppressed, because `Secure` is computed rather
than a literal `true` — but it now names the function that decides it instead of an open question.

**O-15** was found by asking whether a detected app could actually deploy. It could not: the spec had
no routing, and filling that in surfaced the question nobody had answered. **It is now resolved** —
see design 03 §4.2; the rest of this section is why.

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
working; reused rather than ever-increasing so a deleted app's port comes back.

**The other half is answered by phase 10 shipping.** What remained was whether lowest-free is the rule
and whether `9000-9999` is the right range, to be revisited once there was a second routing adapter to
compare against. There is: Traefik does subdomain and path, so port mode is the laptop default's path
rather than the only one, and an install that outgrows a thousand ports has a better answer available
than a wider range. The `[P]` stands as the `[D]`.

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

**O-5 is resolved** — see R-169 and design 03 §4.3. Deferring to each adapter was always most of the
answer (R-047's shape): an adapter that issues certificates declares how, one that cannot says so
through `RoutingCapabilities`. What that left unanswered arrived with R-174, which makes Pando run the
edge and therefore write its static configuration — so Pando is the thing choosing a challenge type,
and "per-adapter" stopped being an answer for the adapter Pando ships.

**Both, chosen in the edge settings.** HTTP-01 per hostname needs only a reachable `:80` and an email
address, and is the one an install can turn on without understanding its own DNS. DNS-01 needs a
provider credential and yields the wildcard R-166 prefers, covering an app's hostname before the app
is deployed. Picking one for everybody would be wrong in opposite directions: HTTP-01 alone leaves
R-166's preferred topology permanently unavailable, DNS-01 alone makes TLS conditional on credentials
many installs do not have.

Neither is a silent default. An install that configures neither gets `:80` and is told that is what it
has — a certificate that quietly failed to issue is worse than one nobody promised, because the
failure surfaces as a browser warning to a user rather than as a message to an operator.

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
| **O-5** | TLS issuance — ACME, wildcards, self-signed local | Per-adapter, and for Pando's own edge both challenge types offered and chosen per install | R-169, design 03 §4.3 |
| **O-13** | Session revocation mid-websocket | Re-authorize on the assertion lifetime; close on failure | design 06 §4.2 |
| **O-15** | How a host port is chosen in port-mode routing | Lowest free port in a configured range, held as a durable allocation; revisit closed by Traefik shipping | design 03 §4.2 |
| **O-14** | DR restore bootstrap ordering | Largely dissolved by O-11; confirm sequencing in phase 9 | design 07 D |
| **O-6** | Which backup destinations ship | Backup is an adapter category; destinations are adapters, and `local` ships in v1 | R-217, R-252, design 03 §8.1 |
| **O-16** | How log retention is enforced | Per-app cap applied at workload creation; the aggregate enforced at plan time against the **sum of committed caps**, not measured usage | R-222–R-224, design 03 §2 |
| **O-17** | What an "administrative verb" is (R-265) | Install-scoped verbs, held as a grant with no app; a fourth built-in role | design 06 §2.1, R-080/R-081 |

### O-16 — bound what is promised, not what accumulates

The three options were: cap per app and let the aggregate be a warning; put a capability on the
runtime adapter; or have Pando collect logs itself. The second, with a specific shape.

A runtime declares what it can do about logs (`LogRetentionCapability`). Docker answers: it can cap a
workload at creation, it cannot change that cap without recreating the container, and it cannot
report how much log space an app is using. All three answers matter.

**The aggregate is a plan-time bound on the sum of caps, not an observation of usage.** That is the
part worth arguing for. Bounding what is committed is the stronger guarantee — if every app's logs
are capped and the caps sum under the budget, the total cannot exceed it, and nothing has to be
watched. Measuring usage would mean acting *after* the disk was already filling, and the only remedy
at that point is recreating containers, which the reconciler may not do because an unrelated app
turned chatty: that is destruction on a schedule.

So R-224 does not become a notification, which is what option 1 would have cost. It becomes a refusal
at the point where a refusal is cheap and reversible: `CAPACITY_WOULD_OVERSUBSCRIBE` at plan time,
naming what would be committed and what is allowed.

Two things fell out of implementing it. Nothing carried `retention.log_bytes` into a container at
all, so every app's logs were unbounded regardless of what its spec said. And `Defaults.Apply` ran
only on the detection path — so a hand-written spec, which is the API's own documented way to
configure an app, got no retention defaults whatsoever. Every spec the acceptance suite writes is
hand-written, which is exactly why nothing noticed.

### O-6, and the `[D]` it reversed

**O-6 — resolved by making backup a category, which reversed a `[D]`.** The question was "which
destinations ship"; the answer changes the shape rather than the list. Destinations are adapters, so
which ones ship is the same kind of question as which runtimes ship, and it stops being an open
decision — `local` is v1, anything else is a pull request that touches no core code.

The reversal is the interesting part and it is recorded in full in design 03 §8.1, including what the
old argument got right. Short version: it described a byte sink correctly, then assumed the
destinations people want are byte sinks. An object store expires and versions objects on its own
schedule and a filesystem path does not, so R-211's retention has two possible owners and picking the
wrong one silently breaks either pruning or restoring. That is a capabilities question (R-254), and a
`Destination` interface would have grown a capabilities struct one provider later, consulted through
the type assertion R-254 exists to forbid.

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
