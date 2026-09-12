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

## Still unresolved: the console needs the internet

Two substitutions flagged upstream, both of which matter more for Pando than for
most products, because Pando is software someone installs on their own host:

- **Icons** load from the unpkg CDN. `readme.md` says "for production or offline
  use, vendor the icons you actually use". Phase 8 is production, and the
  working set is seven named glyphs.
- **Fonts** load from Google Fonts. An install on a private network renders in
  fallback faces.

Neither is addressed here. An air-gapped install currently gets a console with
no icons and the wrong type, which is a real defect for a self-hosted product
rather than a cosmetic one.

## No OpenAPI spec exists

The phase asks for "API types generated from the OpenAPI spec — never
hand-written (R-261)". There is no OpenAPI document in the repository.

`cmd/gen-api-types` reflects over the Go types the handlers serialize instead.
Writing an OpenAPI document by hand to generate from would reintroduce exactly
the drift the requirement forbids, one level removed: the spec would be the
thing that fell out of step. The generated file is committed and CI fails on a
diff, the same way the traceability index works, so a change to the API's
surface shows up in review.
