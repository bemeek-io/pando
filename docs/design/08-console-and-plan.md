# 08 — Console and Build Plan

---

## 1. Console

React + TypeScript + Vite. Built to static assets, embedded in the Go binary via `embed.FS`, served by chi. One binary (R-253) includes the UI.

### 1.1 Two audiences, one app

**[D]** R-264/265. Root is the launcher — tiles for every app the user holds a **data-plane** grant on, from `GET /me/apps`. Users holding any administrative verb see an **Admin** entry that reveals the management console scoped to what they hold.

**[D]** The launcher is not a separate build. A user with no admin verbs simply never sees the admin routes. This matters because it means a non-technical user's first experience is a page of tiles, not a dashboard.

**[D] Resolved (O-9): the launcher is the notification.** Being granted access to an app does not send
a message. The app appears in the recipient's tiles the next time they load the launcher (R-264), and
that is the whole mechanism for v1.

A separate notification would be delivered by the notify adapter, which is console-only in v1 (R-231)
— so it would arrive as a message in the console, next to the tile that already appeared. For a
recipient who has never signed in, a console-only notification is invisible in a way a tile is not:
the tile is waiting for them whenever they arrive. Reconsider when an SMTP adapter exists and a share
can reach someone who is not already looking at Pando; the interface for it is already there (R-232).

**[D] Tiles.** A tile is a square — the app's image (R-340), or when it has none a patch of terrain
generated from the app's ID — with the name underneath. The terrain is the same generator as the
console background, on a small grid, printed on one of five sheets drawn from the palette's terrain
colors (vegetation, water, sand, paper, slate); red is left out so the marker stays rare. The
contours make each tile unique; the sheet varies it further. This puts the contour map in a place
the brand spec does not list, next to the launcher's own background terrain: decided by the product
owner, on the reasoning that a tile is content — a picture of the app — not decoration.

**[D] Favorites.** R-341. A star on each tile, shown always on a favorite and on hover or focus
otherwise, pins the app into a *Favorites* section above *Your apps*; a pinned app is not repeated
below. Both sections are split from the one `GET /me/apps` response. The toggle is optimistic and
rolls back on refusal. It carries **no status line**: an app that is running or degraded and has an
address is a link, and anything else is greyed out and is not. This is a deliberate exception to the
design system's "status is a symbol plus a word" rule, decided by the product owner: the launcher is
for opening apps, "running" is the normal case, and a word on every tile saying so is noise. The
state is still in each unreachable tile's accessible name and hover title, so the difference is
never carried by appearance alone.

**[D] Settings.** The signed-in person's own settings — theme and signing out — are a page of their
own at `/admin/settings`, opened by a gear in the launcher header and in the admin sidebar header,
with a back arrow to wherever it was opened from. Not a section of the admin console: nothing on it
is administration. Under `/admin` only because that prefix is already reserved against app slugs
(R-023). The launcher does not link to *API and tools*; it is in the admin console.

### 1.2 Stack [P]

| Concern | Choice |
|---|---|
| Router | TanStack Router — typed routes |
| Server state | TanStack Query |
| Client state | Zustand, sparingly |
| Forms | React Hook Form + Zod |
| Styling | Tailwind |
| Components | **The `pando-design` system** (`.claude/skills/pando-design/`). Radix only for behavior it does not implement, styled from its tokens. |
| API types | Generated from the OpenAPI spec — never hand-written |
| Streaming | Native `EventSource` for logs, `WebSocket` for exec |
| Terminal | xterm.js |

**[D]** API types are generated. Hand-written types drift from the server and R-261 depends on the API being authoritative.

**[D] A design system exists and supersedes the original "own layer on top" line above.** It is
imported into `.claude/skills/pando-design/` from a Claude Design project and carries tokens, 24
components, brand rules and voice guidance, plus a lint config that catches raw hex values, raw `px`
values and non-brand fonts. Building a parallel component layer beside it is how an install ends up
with two design systems and neither maintained.

**[D]** Its voice rules and §00 3.2's error standard are one standard, not two. R-105 asks for an
error a person can act on or paste into an assistant; the design system asks for no apology, no
`Error:` prefix and no exclamation mark. An API message and a console message reach the same person.

**[D] The console carries generated topographic terrain in its background.** This departs from the
design system, which confines the contour map to four places (marketing hero, docs home header,
empty states, 404) and says "never as wallpaper, never behind text". A console built strictly to
that rule carried no trace of the brand, and the product owner chose to put the terrain behind it.

It covers the whole background, so what keeps it from reading as wallpaper is how quiet it is:

- **Extremely faint, and even.** One colour (`--contour-line`), one line width, one opacity (0.35)
  across the whole thing. No index contours and no fading in or out — a darker line or a patch
  that fades draws the eye, which is exactly what a background must not do.
- **Part of the page.** `console/src/ui/TopoBackground.tsx` fills its page root and scrolls with
  the content; pinned to the window it looked like a layer floating over the product. It is behind
  the admin console and the launcher.
- **In colour, beside the form, on sign-in.** The sign-in and first-run password screens show the
  same terrain as a picture (`TopoMap`): index contours in `--contour`, the rest in
  `--contour-line`, and the marker-red summit triangle on the top of the central hill, as on the
  brand's hero figure. On a wide window the land rises on the right and falls away before the form
  on the left; below 60em it rises at the top and falls away above the form. There is no panel
  edge: the map ends where its lowest contour does, an irregular line made by the terrain, not a
  crop and not a fade. The form is never drawn over the map.
- **Seamless at any length.** It is a tile, and the terrain is periodic — hills wrap round the
  tile's edges and the warp uses whole periods — so contours meet across every seam. The tile is
  1600 × 1200, so a repeat is rarely in view at once.
- **Different ground per page.** Hills and warp come from a seed: the section, and the app when
  there is one. Each screen has its own map, the tabs of one app share that app's map, and the same
  seed always gives the same map.
- **Real terrain, not a pattern.** A height field of irregular hills, contoured by marching squares
  and smoothed into curves. Lines are level sets of one surface, so they never cross.
- **Tokens only.** Nothing new enters the palette, and the night-survey theme follows.
- **No frame.** Pages are not bordered or boxed; `ui/Sheet.tsx` is only the shared heading-and-
  content layout, uncapped to match `ui/layout.ts`.

Where the contour figure itself appears, it is by the system's existing recipe: 120px inside empty
states. The 404 figure (320px, collared, summit mark absent) is **not**
used for an app whose record fails to load: that figure means "the thing you came for is not here",
and such an app is in the list — the screen shows the server's own reason (R-105) and offers to
delete it instead.

### 1.3 Screens that carry requirement weight

Most screens are ordinary CRUD. These four are where requirements are either honored or lost.

**Detection review** — R-102, R-105, R-103.
Shows the winning bid with its evidence, the runners-up, and every outstanding question. **Each question has a copy button**, because the intended workflow is pasting it into the assistant that wrote the app. Question text is rendered verbatim from the API; the console does not paraphrase it, or the R-105 guarantee is lost in the UI layer.

**Warnings** — R-201, R-168, R-028.
Rendered inline where they apply, dismissible, never blocking. The persistence warning uses the observed directory when available: *"Your app wrote to `/app/data` during setup. That data won't survive a redeploy unless you add a volume here."* Warnings and blockers are visually distinct — a warning must never look like an error, or people learn to ignore both.

**[D]** Nothing warns about stacked logins (R-171). An app that presents its own login page behind
Pando's is that app working correctly, and Pando has no basis for calling a working app a problem.
This is the general rule the warning set is held to: a warning describes something that will bite the
user later — data that will not survive a redeploy, routing that will break — not something that
merely looks unusual. Warnings that fire on correct behavior are how users learn to dismiss the ones
that matter.

**Sharing** — R-076, R-077.
The anonymous grant never stands on the bare word "public." **[P] R-077 overridden in the heading:**
the action is called *Make it public*, because that is what it is called everywhere else and a
heading nobody recognizes is unclear in its own way. What the requirement protects is kept — the
consequence, *anyone on the internet can open this, without signing in*, sits directly beneath the
heading and again in the confirmation, so the word never does the work alone. The confirmation step
stays. When host policy forbids it (R-076), the option is visible but disabled with an explanation of who to ask — not hidden, because a hidden option produces a support ticket instead of understanding.

**Deploy settings** — R-145, R-147.
Start-then-swap shows its constraint in body text at the point of enabling, not a tooltip: *two copies of your app run at the same time during a deploy. Do not enable this if your app writes to a local file or runs migrations on startup.* Auto-rollback likewise explains why it is off by default.

### 1.4 Design principle

**[D]** R-005 and R-104. The default path shows almost nothing — name, source, deploy. Everything with a sane default is behind **Advanced** and never surfaced during setup. If a new setting is added to the primary flow, someone must justify why it is a blocker rather than configuration.

---

## 2. Build order

Sequenced so each phase produces something runnable and the riskiest work happens while it is still cheap to change.

### Phase 0 — Skeleton
Repo layout, Postgres + migrations, sqlc, zap, chi, config loading, ID generation, the error envelope, `secret.Value`, audit table with `REVOKE UPDATE, DELETE`, the import-lint CI rule (§03 9).

*Done when:* the server starts, `/healthz` responds, an audit event can be written and provably not modified.

### Phase 1 — Identity and authorization
Local identity adapter, sessions, tokens (both kinds), roles seeded by migration with the immutability trigger, the authorizer, the verb catalog.

*Done when:* §06 evaluation order is fully covered by unit tests, including a delegated token orphaned by its owner's deletion.

### Phase 2 — Spec and state
`AppSpec` types, validation, the classified differ, `spec_revisions` with the append-only trigger, apps and grants, the app CRUD endpoints.

*Done when:* an app can be created and a spec hand-written, validated, and pinned via the API. Nothing runs yet.

### Phase 3 — Adapters and the planner
Interface definitions, the registry, the Docker runtime adapter, the loopback routing adapter, the local secrets adapter. The planner with every plan-time error path.

*Done when:* `POST /apps/{id}:plan` returns each of `PLAN_SLOT_UNFILLED`, `PLAN_CAPABILITY_UNSUPPORTED`, `CAPACITY_WOULD_OVERSUBSCRIBE`, and `PLAN_NO_ADAPTER_MEETS_POLICY` on the right inputs.

### Phase 4 — Build and deploy
BuildKit adapter, rootless, containerized, no socket. The deployment pipeline. Recreate strategy.

*Done when:* **Sequence B** passes with a hand-written spec, including the assertion that the build container has no runtime socket.

### Phase 5 — Proxy
The identity-aware proxy, assertion minting, JWKS, header stripping, streaming.

*Done when:* **Sequence C** passes, including the forged-header test and the cross-app `aud` rejection.

### Phase 6 — Detection
Detector auction, the confidence ladder, compose import, trial run, question generation, warnings.

*Done when:* **Sequence A** passes against a set of real public repos — a Dockerfile app, a compose stack, a static site, a Node app with no deployment artifacts, and a monorepo.

### Phase 7 — Reconciler
The loop, the state machine, drift classification, backoff, the give-up threshold.

*Done when:* killing a container by hand restores it; killing it repeatedly reaches `failed` and **stays** there.

### Phase 8 — Console
Launcher, admin, the four weight-bearing screens.

### Phase 9 — Backup and DR
Rolling backups, the DR bundle, verify-then-restore, GC with the aggregate disk budget.

*Done when:* **Sequence D** passes, including rejection of a tampered bundle with the target untouched.

### Phase 10 — CLI, MCP, Traefik
The remaining surfaces and the second routing adapter.

**[D]** Traefik lands last deliberately. It is the second implementation of the routing interface, and building it is the test of whether §03 4's abstraction actually holds. If Traefik requires changing the interface, the interface was wrong — better to learn that in phase 10 than to have assumed it was right in phase 3.

---

## 3. Risk register

| Risk | Phase | Mitigation |
|---|---|---|
| Detection quality below the R-103 bar | 6 | Build a corpus of 30 real repos early. Track questions-per-deploy as a tracked metric, not a vibe. |
| Docker socket leaks into a build | 4 | Integration test asserting the build container's mount list. Non-negotiable. |
| Header spoofing through the proxy | 5 | Explicit test with forged headers. |
| Postgres prerequisite undermines the hobbyist install | 0 | **Resolved.** Compose supplies Postgres beside Pando; no prerequisite beyond the container runtime v1 already requires. |
| Routing abstraction is Docker/Traefik-shaped | 10 | Sketch the Cloudflare adapter on paper during phase 3, before the interface is fixed. |
| Required-vs-optional slots (O-4) | 6 | Trial-run promotion fallback (§01 2.5). Measure false-block rate against the corpus. |
| The two planes get conflated again | 1 | The comment in `CheckData` explaining that this was reversed once, plus a test asserting an operator on someone else's app is denied use. |

---

## 4. Open decisions added during design

These extend §24 of the requirements document.

| ID | Question | Where |
|---|---|---|
| **O-11** | ~~How Postgres is supplied~~ — **resolved:** the install topology supplies it (Compose), with an external-database override | §00 1.1 |
| **O-12** | ~~Whether the MCP exclusion list is hard or policy-controlled~~ — **resolved:** policy-controlled, default-closed, expressed as host policy per verb rather than a second mechanism | §04 3 |
| **O-13** | ~~Session revocation mid-websocket~~ — **resolved:** re-authorize on the assertion lifetime, close on failure; falls out of the single revocation window in §06 3.1 | §06 4.2 |
| **O-14** | ~~DR restore bootstrap ordering~~ — **largely dissolved** by O-11; confirm sequencing in phase 9 | §07 D |

**All four are resolved.** Of the ten in requirements §24, three remain open: O-4 (slot detection,
awaiting measurement rather than decision), O-5 (TLS issuance, genuinely per-adapter), and O-6 (which
backup destinations ship — provider-shaped; that a destination is *not* an adapter category is settled
in §03 8.1). None blocks any phase.

A second review also closed three fragilities that were not on any list: the unreconciled revocation
window (§06 3.1), `markUnobservable` having no field to write to (§02 2.3), and R-193's rotation
restart having no detection mechanism (§02 2.4). See `../plan/design-gaps.md`.
