# Phase 8 — Console

**Goal:** two audiences, one app. A non-technical user's first experience is a page of tiles, not a
dashboard.

**Prerequisites:** phase 5.

**Design:** `../design/08-console-and-plan.md` §1; `../design/04-api.md` §2.9.

## Before anything else: there is already a design system

`.claude/skills/pando-design/` holds the Pando design system — "Topo map". Invoke the `pando-design`
skill and read its `readme.md` **before** writing a component or a line of CSS. It carries the tokens,
24 React components, the brand rules, the voice guidance and a lint config that catches raw hex
values, raw `px` values and non-brand fonts.

Design 08 §1.2 predates it and says "Radix primitives, own layer on top". **That is superseded**: the
components exist, they have no npm dependencies and no CSS-in-JS, and building a second layer beside
them is how an install ends up with two design systems and neither maintained. Use Radix only for
behaviour the system does not implement — focus trapping, a combobox — and style it from the tokens.

Three things the system settles that this phase would otherwise re-decide:

- **Status is a symbol plus a word, never a colored pill** — so it never depends on color alone. That
  is the same distinction the reconciler draws between `running`, `degraded` and `failed`.
- **Marker red appears in under 3% of any screen**: the summit mark, failed apps, destructive
  actions. If a screen shows red in more than one or two places, something is wrong with the screen.
  This is the visual form of "a warning must never look like an error".
- **An action keeps its name through the whole flow.** "Deploy" produces "Deployed" — the button, the
  toast and the log line use the same word.

The design project also holds a clickable console prototype (`ui_kits/console/`) built from the
brand spec's own wireframes — the apps table, app detail, build log, variables, settings, add-app
flow and the dark theme. It is not vendored here. **Read it before designing these screens again**;
see `.claude/skills/pando-design/PROVENANCE.md` for how to pull it.

## Tasks

- [x] Pull the per-component `.d.ts` and `.prompt.md` sidecars from the design project — the console
      is TypeScript and needs the types, and each `.prompt.md` states when *not* to use a component
- [x] Vite + React + TypeScript in `console/`, built to static assets embedded via `embed.FS`
- [x] Link `styles.css` from the design system; dark theme is `<html data-theme="dark">`
- [x] Wire `_adherence.oxlintrc.json` into the console's lint step, so a raw hex or px fails CI the
      way the adapter import rule does for R-027 — **it does not run under oxlint**, which implements
      neither rule the file uses. Run under ESLint instead, and its per-component prop rules are
      filtered out because they reject `onClick` on a `<Button>`
      ([note](../design/notes-console-findings.md))
- [x] **API types generated, never hand-written** (R-261) — there is no OpenAPI spec in the
      repository, so `cmd/gen-api-types` reflects the Go types the handlers serialize. Committed and
      diff-checked in CI, like the traceability index
- [x] Launcher at root: tiles from `GET /me/apps`, data-plane grants only (R-264)
- [~] Admin entry (R-265) — **half**. There is no administrative verb anywhere in the model (O-17).
      Shipped: the console opens for someone holding a control-plane grant on at least one app,
      scoped by the server to those apps. Missing: install-level administration, which has neither
      verbs nor screens
- [x] Detection review screen (R-102, R-103, R-105)
- [x] Warnings, rendered inline where they apply (R-201, R-168, R-028) — carrying no red at all,
      which is what makes them distinct from an error in a palette that has no amber
- [x] Sharing screen (R-076, R-077)
- [x] Deploy settings (R-145, R-147)
- [~] Log streaming via `EventSource` — **done**. Exec via WebSocket + xterm.js — **not started**

## Requirements in scope

R-005, R-076, R-077, R-102–R-105, R-145, R-147, R-168, R-201, R-261, R-264, R-265.

## Done when

A user with no admin verbs sees only tiles; a user with admin verbs sees the management console scoped
to what they hold; the four screens below meet their stated requirements.

**Verified in a browser against the shipped binary**, with two apps from the bemeek-io org: the
launcher renders tiles with symbol-plus-word status and no Admin entry for a user with no
control-plane grant; the Admin entry appears for one who has; detection review shows the winning bid
with its evidence, the question **verbatim** with a working copy button, the runners-up behind a
disclosure, and the path-routing warning inline and quiet; answering the question enabled Accept, and
accepting pinned revision 1 and left the app `proposed` — it did not deploy.

**The second clause is half met.** "Scoped to what they hold" is the server's scoping of
`GET /apps`; "holding an administrative verb" has no implementation because no such verb exists
(O-17).

## The four screens that carry requirement weight

Most screens are ordinary CRUD. These four are where requirements are honored or lost.

**Detection review.** Shows the winning bid with its evidence, the runners-up, and every outstanding
question. **Each question has a copy button** — the intended workflow is pasting it into the assistant
that wrote the app. Question text is rendered **verbatim** from the API; the console does not
paraphrase, or the R-105 guarantee is lost in the UI layer.

**Warnings.** Inline, dismissible, never blocking. The persistence warning uses the observed directory
when available: *"Your app wrote to `/app/data` during setup. That data won't survive a redeploy
unless you add a volume here."* Warnings and blockers must be **visually distinct** — a warning that
looks like an error teaches people to ignore both.

**Sharing.** The anonymous grant is **never labeled "public."** It reads *anyone on the internet,
without signing in*, with a confirmation step. When host policy forbids it, the option is **visible
but disabled with an explanation of who to ask** — not hidden. A hidden option produces a support
ticket instead of understanding.

**Deploy settings.** Start-then-swap shows its constraint in **body text at the point of enabling, not
a tooltip**: *two copies of your app run at the same time during a deploy. Do not enable this if your
app writes to a local file or runs migrations on startup.* Auto-rollback likewise explains why it is
off by default.

## Design principle

R-005 and R-104: **the default path shows almost nothing** — name, source, deploy. Everything with a
sane default lives behind **Advanced** and is never surfaced during setup. If a new setting appears in
the primary flow, someone must justify why it is a blocker rather than configuration.
