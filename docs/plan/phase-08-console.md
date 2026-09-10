# Phase 8 — Console

**Goal:** two audiences, one app. A non-technical user's first experience is a page of tiles, not a
dashboard.

**Prerequisites:** phase 5.

**Design:** `../design/08-console-and-plan.md` §1; `../design/04-api.md` §2.9.

## Tasks

- [ ] Vite + React + TypeScript in `console/`, built to static assets embedded via `embed.FS`
- [ ] **API types generated from the OpenAPI spec** — never hand-written (R-261)
- [ ] Launcher at root: tiles from `GET /me/apps`, data-plane grants only (R-264)
- [ ] Admin entry, visible only to users holding an administrative verb, scoped to what they hold
      (R-265)
- [ ] Detection review screen (R-102, R-103, R-105)
- [ ] Warnings, rendered inline where they apply (R-201, R-168, R-028)
- [ ] Sharing screen (R-076, R-077)
- [ ] Deploy settings (R-145, R-147)
- [ ] Log streaming via `EventSource`; exec via WebSocket + xterm.js

## Requirements in scope

R-005, R-076, R-077, R-102–R-105, R-145, R-147, R-168, R-201, R-261, R-264, R-265.

## Done when

A user with no admin verbs sees only tiles; a user with admin verbs sees the management console scoped
to what they hold; the four screens below meet their stated requirements.

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
