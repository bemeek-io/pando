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

## Stack

| Concern | Choice |
|---|---|
| Router | TanStack Router — typed routes |
| Server state | TanStack Query |
| Client state | Zustand, sparingly |
| Forms | React Hook Form + Zod |
| Styling | Tailwind |
| Components | Radix primitives, own layer on top |
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
