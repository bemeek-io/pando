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
console.** The page is not capped and stays left-aligned: a table's rows and
their rules run to the edge of the window. Its *content* is capped, which is a
different thing and turned out to be the whole of the problem. Three attempts,
in order:

1. 1280px, left-aligned — the spec as written. On a 2560px display that is a
   console in the left third of the window and two thirds of empty paper.
2. 1280px, centered. The emptiness moves to both sides, which is worse: now
   nothing is anchored.
3. Uncapped. The rows look right and the content inside them does not — Status
   and Updated end up two thousand pixels from the name they describe, and Add
   app sits in the far corner.

What is actually wanted is rules to the window and columns at a measure. So
every table sizes its columns in `ch` rather than `fr`, summing to 84ch, and
`ui/layout.ts` carries that measure plus the Table's own gaps and padding for
the page headers, so a header's action stops where the last column does. A
header carrying the measure also carries `--type-body-ui`, because `ch` is
measured in the font of the element it is written on and the tables' font is
what the number was counted in.

Prose keeps its own caps where they are written — 68ch, the audit filter at
40ch. Sharing, deploy settings and the terminal take the same measure as
everything else. The launcher is unchanged.

## Logs: one box, showing one deploy, growing without limit

`GET /apps/{id}/logs` — the running app's own output — existed and nothing
called it. The console's only log was the newest deployment's build output, on
Overview, in a box with no height limit: a few thousand lines of build output
pushed the rest of the screen out of reach, and there was no way back to the
deploy before it.

There is now a Logs tab holding both, because both are what somebody means by
"the logs": the app's output, tailed at 500 lines and re-asked every five
seconds while the app runs, and every deploy with its build log. Overview keeps
the current deploy's log, since that is the screen a deploy is watched from,
and every earlier one is a history that belongs with the other history.

Both boxes stop at 60vh and scroll, and follow the end of the stream until the
reader scrolls up — a log that jumps back to the bottom while somebody is
reading the failure three screens above cannot be read at all.

**Deploy logs do not survive a restart, and the console now says so.**
`deploy.LogStore` is in memory, bounded at 2000 lines, and written nowhere. A
deploy from before the last restart has no log, and `Follow` on an unknown
deployment ID opens an empty stream rather than reporting that there is
nothing — so the console would have shown an empty black box, which reads as a
deploy that printed nothing. It says what is true instead. Logs on disk under a
size cap are R-222/R-223 and belong to the reconciler's garbage collection;
they are not implemented.

## A warning that says "define one here", on a screen with no here

R-201's persistence warning ends *"Otherwise define one here"*, and it renders
on Overview, where nothing can be defined. Storage is on the settings tab, four
sections down.

Warning text is rendered verbatim (R-105, design 08 §1.3) so the console does
not get to fix that sentence by rewriting it. It carries a way to the place
instead: `InlineWarning` takes an action, and Overview maps the warning codes it
has a screen for — `WARN_NO_PERSISTENT_VOLUME` to storage,
`WARN_COMPOSE_CONSTRUCT_REWRITTEN` to the configuration,
`WARN_UNDECLARED_DEPENDENCY_SUSPECTED` to dependencies. The storage one opens
the settings tab scrolled to Storage rather than at its top, which is what
"here" was promising. `WARN_PATH_ROUTING_INCOMPATIBLE` has no action, because
the console has no routing screen to send anyone to; a code with no entry
renders as text, with nothing claiming to be actionable.

## Deploy settings are unreachable

`DeploySettings.tsx` implements R-145 and R-147 — start-then-swap, auto-rollback
and deploy-on-push, each explaining itself at the point of enabling, as design
08 §1.3 requires. Nothing imports it. The component exists, is typed, passes
lint, and no route or tab renders it, so neither setting can be changed from the
console. Not fixed here; it needs a decision about where it belongs, and the
settings tab is already four sections long.

## The app-logs endpoint returned Docker's stream framing

`GET /apps/{id}/logs` copies the runtime adapter's reader into the response
body. The Docker adapter handed back `ContainerLogs` unchanged, and a container
with no TTY — which is every workload Pando creates — has its output framed: an
8-byte header before each chunk saying which stream it came from and how long it
is. So the endpoint answered with control bytes at the start of every line.

Nothing had read it. The console had no screen for an app's own output, the CLI
has no `logs` command, and the one place in the codebase that already reads
container output — the trial run's crash capture — strips the framing itself,
in `trial.go`, which is where the knowledge stayed. The adapter now unpicks the
frames as they arrive rather than from a buffer, so a followed log stays live,
and `TestR071_LogsArriveWithoutDockerFraming` asserts that what comes out is
what the app printed.

## Rolling backups were failing for the whole installation

Found while watching the log of a restart, not through the console: the
reconciler logs *"could not list apps for rolling backups: cannot get array
length of a scalar (SQLSTATE 22023)"* on every pass, and the sweep returns
before taking any (R-210).

`AppSpec.Volumes` is tagged `json:"volumes"` with no omitempty, so an app that
declares no storage — most of them — stores `"volumes": null`. The query read it
with `->`, which answers with that JSON null and not SQL NULL, so the
`coalesce(…, '[]')` guarding the call never fired and `jsonb_array_length` was
handed a scalar. One such app failed the query, and the query is the whole
sweep, so no app on the installation was backed up.

The first fix — a `jsonb_typeof(...) = 'array'` test in front of the length
call — still failed. `AND` promises no evaluation order and the planner reached
the length call first. The predicate is now a comparison against `'[]'`, which
is defined for every value that column can hold, and
`TestR210_AnAppWithNoVolumesDoesNotStopTheRollingBackupSweep` asserts it against
a real Postgres.

## Light and dark, and who decides

The design system has shipped both themes from the start — `data-theme="dark"`
on the root, a full second set of color tokens — and nothing in the console ever
set the attribute, so an installation was paper-white at three in the morning
whatever the machine was set to.

`ui/theme.ts` applies it, with three states rather than two. "System" is the
default and is not a third appearance: it is the absence of a choice, and it
keeps following the machine when the machine changes. Choosing light or dark
records that choice in `localStorage` and stops following — per browser, because
the same account on a bright desk and in a dark room wants different answers and
neither belongs in the database. It is applied at the root of the app, above the
sign-in page, which is the screen most likely to be met in the dark.

The control is a ghost button naming what it will do — "Dark" in the light
theme, "Light" in the dark one — because a toggle labelled with its current
state reads as a statement and gets clicked by people who wanted the opposite.
It sits in the launcher's header and the console's sidebar footer. Nothing else
needed changing: every color in the console already comes from a token, and the
logo is drawn from `--ink` and `--marker` rather than loaded as artwork.

## Smaller things the same session turned up

**A screen's action belongs beside its heading.** Add app was opposite the
heading, at the far end of the content measure, which on a wide window is a
long way from the word it belongs to. Both page headers — the apps list and the
shared install screen — now put the action directly after the title.

**An empty terminal is not a log.** A finished deploy whose output Pando no
longer holds rendered on Overview as a black box saying "Deploy log ·
succeeded", with nothing in it and nothing to scroll. It now shows the deploy's
result and a way to the Logs tab, and the box is drawn only when there is
something in it.

**An app opens in its own tab.** The launcher's tiles and the address on an
app's Overview replaced the console with the app, leaving the browser's back
button as the way back to what you were doing.

**The console does not explain itself.** "This is the running app's own output,
kept by the runtime — not by Pando — so it goes when the app is redeployed" was
written for a reader who asked, and the reader of a log has not asked anything.
Both section preambles on the Logs tab are gone; what is on the screen says what
it is.
