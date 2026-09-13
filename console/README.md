# Console

React + TypeScript + Vite. Built to static assets, embedded in the Go binary via `embed.FS` and
served by chi. One binary includes the UI (R-253).

Built by `make console`, which outputs into `internal/console/dist/`.

**Not scaffolded yet — phase 8.** See [`../docs/plan/phase-08-console.md`](../docs/plan/phase-08-console.md)
and design [`08-console-and-plan.md`](../docs/design/08-console-and-plan.md) §1.

## Two audiences, one app

Root is the **launcher** — tiles for every app the user holds a *data-plane* grant on, from
`GET /me/apps`. Users holding any administrative verb additionally see an **Admin** entry that reveals
the management console, scoped to what they hold.

**The launcher is not a separate build.** A user with no admin verbs simply never sees the admin
routes. This matters: it means a non-technical user's first experience is a page of tiles, not a
dashboard.

## The design system comes first

`.claude/skills/pando-design/` holds the Pando design system — "Topo map": tokens, 24 React
components, brand rules, voice guidance, and a lint config that catches raw hex values, raw `px`
values and non-brand fonts. **Invoke the `pando-design` skill and read its `readme.md` before writing
a component or a line of CSS.**

Link one file and set one attribute:

```html
<link rel="stylesheet" href="../.claude/skills/pando-design/styles.css">
<html data-theme="dark">  <!-- the "night survey" theme -->
```

Import components from its `index.js`, never from a component's own file.

## Stack

| Concern | Choice |
|---|---|
| Router | TanStack Router — typed routes |
| Server state | TanStack Query |
| Client state | Zustand, sparingly |
| Forms | React Hook Form + Zod |
| Styling | Tailwind |
| Components | **`pando-design`** — Radix only for behavior it lacks, styled from its tokens |
| API types | **Generated from the OpenAPI spec — never hand-written** |
| Streaming | Native `EventSource` for logs, `WebSocket` for exec |
| Terminal | xterm.js |

API types are generated because hand-written types drift from the server, and R-261 — the API is the
product — depends on the API being authoritative.

## Design principle

**The default path shows almost nothing** — name, source, deploy. Everything with a sane default lives
behind **Advanced** and is never surfaced during setup (R-005, R-104). If a new setting appears in the
primary flow, someone must justify why it is a blocker rather than configuration.

Four screens carry requirement weight and are specified in detail in the phase file: detection review,
warnings, sharing, and deploy settings. Most other screens are ordinary CRUD.
