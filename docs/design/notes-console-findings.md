# What building the console turned up

Phase 8 in one sentence: the launcher, the management console, and the four
screens that carry requirement weight. Most of it went the way the design said
it would. These did not.

## R-265 has no administrative verb to hold

> **R-265** Users holding any administrative verb see an **Admin** entry point
> from the launcher, exposing the console scoped to whatever privileges they
> hold.

There is no such verb, anywhere:

- **R-080's catalog is entirely `app.*`** — thirteen verbs, every one scoped to
  a single app. Nothing install-level.
- **Grants are per-app.** `grants.app_id` is `NOT NULL`, so an install-level
  grant cannot be written.
- **Users carry no admin flag.** The first-run "administrator" (R-046) is an
  ordinary local user named `admin`.
- **`app.create` is referenced but does not exist.** Sequence A step 1 says
  "authz: user holds `app.create` (install-level)". It is not in the catalog and
  nothing checks it — `handleCreateApp` checks only that the caller is not
  anonymous.

Verified empirically: `GET /me` returns `principal_kind`, `id`, `groups`,
`user_id`, `email`, `display_name` and `must_change_password`. No verbs.

**What was built.** The half of R-265 that is implementable, which is also the
half the screens that exist need: the management console opens for someone
holding a **control-plane** grant on at least one app. `GET /apps` is
control-plane scoped — a different list from `GET /me/apps`, which is data-plane
scoped (R-070, R-071) — so a non-empty result means "there is something here you
can administer", and what opens is scoped to exactly those apps. The scoping is
the server's, not the console's.

**What is still missing** is install-level administration: users, hosts, host
policy, the audit log. Those need verbs that do not exist — and none of those
screens exist either, so nothing is being hidden from anyone. Recorded as
**O-17**.

Worth noting how much else depends on this. Design 05 §3 says "the console lists
violating apps when a policy is saved, before it is saved"; there is no policy
endpoint and no verb to gate one. R-085's "host policy may disable exec
install-wide" is the same shape.

**Resolved within this phase**, once the privilege escalation this gap allowed was
demonstrated on the shipped stack:
option 1 from `open-decisions.md` — install-scoped verbs held as a grant with no
app, plus a fourth built-in role. `GET /me` now carries `verbs`, and
`app/principal.ts` reads them rather than inferring administration from
`GET /apps`. The six endpoints are gated. The paragraphs above describe what was
true when this note was written, and are left as written; design 06 §2.1 is the
current account. What is still absent is the install-level *screens* — the verbs
and endpoints they would use exist now.

## The design system's lint config does not run

`_adherence.oxlintrc.json` is the phase's stated mechanism: *"Wire
`_adherence.oxlintrc.json` into the console's lint step, so a raw hex or px
fails CI the way the adapter import rule does for R-027."*

It uses exactly two rules, and **oxlint implements neither**:

- `no-restricted-syntax` — absent from `oxlint --rules` entirely, and dropped
  from the resolved config without a word. `--print-config` confirms it.
- `no-restricted-imports` — listed in `oxlint --rules`, with no checkmark in the
  implemented column.

A file containing `{ color: "#ff0000", padding: "12px" }` lints clean. Every
brand rule in the file — hex, px, fonts, and all 24 components' props — is
inert.

The file's *contents* are an ESLint config: `no-restricted-syntax` with esquery
selectors is an ESLint core rule. Only its name says otherwise. So the console
reads the rules out of the vendored JSON and hands them to ESLint, which
implements both, and raises them from `warn` to `error` — upstream ships
everything as a warning, and a warning fails nothing. Read rather than
restated: the design project is the source of truth, and a second copy of forty
selectors would be a second opinion about the brand.

It then caught real violations in this console's own first draft: two raw px
values and one raw hex.

## And its per-component prop rules are wrong

Under ESLint the prop selectors fire — 21 errors, all false. Each enumerates
only its interface's *own* members:

```
<Button> doesn't accept that prop. Declared props: variant, size, icon,
disabled, fullWidth, children
```

But `ButtonProps extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>,
'size'>`. So the rule rejects `onClick` on a button. It rejects the design
project's own console prototype, which is written
`<Button variant="primary" onClick={onAdd}>Add app</Button>`.

TypeScript checks the same thing correctly from the same 24 `.d.ts` files,
inherited attributes included: `variant="nonsense"` is caught, `onClick` is not.
That is exactly the checking the phase asked the declaration files to provide,
so it is the one kept, and the prop selectors are filtered out with the reason
recorded in `eslint.config.mjs`. Reported upstream rather than corrected here —
PROVENANCE.md makes the design project the source of truth.

## `degraded` has no status symbol

`StatusIndicator` takes `running | building | failed | stopped | info`. Pando's
state machine has eight states, and `degraded` is the reconciler's most
load-bearing distinction: degraded is recoverable and still being worked, failed
is terminal and needs a person (R-151).

Mapping it to `failed` would be wrong twice — marker red on an app that is being
recovered, breaking both that distinction and the under-3%-red rule. It maps to
`building`'s hollow ring, which already means "in flux, being worked on", and
the word beside it says "Degraded". Status is a symbol *plus a word*; here the
word carries the difference. The system should grow a sixth symbol, in the
design project.

## There is no warning tone, and that turned out to be right

`Banner` takes `info | running | building | failed`. Design 08 §1.3 requires
warnings and blockers to be visually distinct, "a warning that looks like an
error teaches people to ignore both" — and the palette deliberately has no
amber: marker red is reserved for three things, one of which is a failed app.

`Banner.prompt.md` also says never to stack banners, and a proposal can carry
several warnings at once.

Both constraints point the same way, and design 08 already said it: warnings go
**inline, where they apply**, not stacked at the top. `ui/InlineWarning.tsx` is
sunken paper, a rule, an `info` symbol and the text — and **no red at all**. An
error is red; a warning is quiet. That is a larger difference than any two tints
would give, and it needs no new color.

## Two build failures from embedding

`//go:embed all:dist` fails the build outright when its pattern matches nothing.
That broke the build twice, in different places:

- `internal/console/dist/` was gitignored, so a fresh clone could not compile the
  Go code at all until someone ran npm. A committed `dist/README.md` keeps the
  pattern satisfied; the binary reports the console is absent and serves the API
  regardless.
- `.dockerignore` excluded `internal/console/dist`, so the image build failed
  inside Docker even with the assets present locally. Excluding the built
  console from the image defeats the point of embedding it (R-253).

## The console needed the internet — fixed

Two substitutions flagged upstream, both of which matter more for Pando than for
most products, because Pando is software someone installs on their own host. An
installation on a private network is a normal way to run it, and there both of
these fail silently — the product renders, just wrong.

**Icons** were fetched from the unpkg CDN *at runtime*, per glyph, on first
render. Offline that meant no icons at all; online it meant every icon appearing
a moment after the rest of the interface. Ten Lucide SVGs (ISC) are vendored in
`assets/icons/` and compiled to a plain JS module by `assets/icons/build.mjs`,
which `Icon` imports synchronously. A module rather than `import.meta.glob`
because the design system has no build step and no npm dependencies — bundler
magic would give that up.

**Fonts** were a `@import` from Google Fonts. The `.woff2` files are now in
`assets/fonts/`: 16 files, 355 KiB, five subsets, so an app named in Cyrillic or
Vietnamese still renders in the real face. `tokens/fonts.css` is Google's own CSS
with the URLs swapped, because the unicode-ranges and subset split are theirs and
re-deriving them is how a subset quietly stops loading.

That second point is not hypothetical — the first attempt *did* re-derive the
names and broke nine of twenty-five faces, because Newsreader and Public Sans are
variable fonts where several `@font-face` blocks share one file while IBM Plex
Mono ships one file per weight. Naming by family and subset alone made the two
Plex files collide and pointed the 400 face at the 500 glyphs.

Verified at the artifact level: `grep` for `fonts.googleapis`, `fonts.gstatic` or
`unpkg.com` across the built CSS and JS returns nothing.

## Exec: the endpoint did not exist

Phase 8's task list says "exec via WebSocket + xterm.js", which reads like console
work. The server side was missing too — no route, no handler — and no phase file
claimed it. Design 04 specified it, so both halves landed here.

One correction to that spec: it wrote `POST /api/v1/apps/{id}/exec`. A WebSocket
handshake is a **GET** by protocol; RFC 6455 requires it and a browser's
`new WebSocket()` cannot issue anything else, so `POST` was not implementable
from the console the endpoint exists for. Design 04 now says GET, and nothing
about the ordering or the checks changed.

The ordering is the requirement, and it is what the tests assert: check
`app.exec`, then host policy (R-085 → `POLICY_EXEC_DISABLED`, which denies the
owner too), then **write the audit event**, and only then open the session. A
session authorized and abandoned without a byte sent is still recorded — there is
a test for exactly that.

R-086 also decides what the screen says. It requires the documentation to state
plainly that the verb list is not a security boundary against someone holding
`app.exec`, so the console says so before opening anything: a terminal can read
the database directly, read the values Pando passed the app including its
secrets, and change the running app in ways that will not appear in its
configuration. And it says what is recorded — the command, not the session (O-7)
— because someone about to type a password is entitled to know which.

## No OpenAPI spec exists

The phase asks for "API types generated from the OpenAPI spec — never
hand-written (R-261)". There is no OpenAPI document in the repository.

`cmd/gen-api-types` reflects over the Go types the handlers serialize instead.
Writing an OpenAPI document by hand to generate from would reintroduce exactly
the drift the requirement forbids, one level removed: the spec would be the
thing that fell out of step. The generated file is committed and CI fails on a
diff, the same way the traceability index works, so a change to the API's
surface shows up in review.

## The console could not delete an app, and the content column was capped

Two gaps found by using the console on a wide display.

**Delete.** `DELETE /apps/{id}` has existed since phase 2, with the CLI calling
it and tests covering both answers to R-204's question. The console had no
button for it, so the one surface where R-204's *asks* is literally an ask was
the one surface that could not delete anything.

It sits in the app's header, beside the name, and not on the settings tab. The
app most likely to be deleted is the one with no settings tab: an app whose
source could not be fetched fails detection, never pins a spec, and shows a
single Configuration tab carrying the reason. A delete on the settings tab is a
delete that app cannot reach. The same reasoning covers the app whose record
will not load at all — that screen was a back button and nothing else, and now
carries the error, the name from the list row, and the delete.

The dialog is R-204's question: two radios, keep a final backup or discard the
storage, with keeping selected — the default R-205 gives the CLI, so the two
surfaces do not disagree about what happens when nobody thinks about it. An app
known to keep nothing gets a plain confirmation instead. *Known* is load-bearing:
an unanswered volumes request is not evidence that an app keeps nothing, so
unknown asks the question rather than assuming the empty case and discarding
data.

The dialog shows the server's message when a delete fails and not its remedy.
The remedy names `force=true`, which is the CLI's way of answering the question
the radios already ask; the console answers it with the second radio and says
so.

**[P] The brand spec's 1280px console column is overridden in the admin
console.** Content there is uncapped and stays left-aligned. The spec's number
was written for a laptop; on a 2560px display it left a three-column table in
the left third of the window and two thirds of empty paper, and centering the
column — tried first — only moved the emptiness to both sides. Tables, logs and
the audit trail take the window. The things with a natural reading width keep
their own caps where they are written: prose at 68ch, the audit filter at 40ch,
sharing and deploy settings at the 1280 the spec asks for, because a form that
spans a wide display is harder to read, not easier. The launcher is unchanged.
