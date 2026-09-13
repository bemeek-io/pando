# Contributing

## Getting set up

```bash
git clone https://github.com/bemeek-io/pando.git
cd pando
make check        # vet, lint, unit tests — what CI runs on a pull request
```

Go 1.27 and Docker. Node 22 as well if you are touching the console.

```bash
make help                    # all targets
make build                   # the pando binary
make console                 # build the console into the embedded assets
make test-integration        # real Postgres and Docker, via testcontainers
make requirements-coverage   # which requirements have a named acceptance test
```

To run a full stack while you work:

```bash
PANDO_RECONCILER_BACKOFF=0s,1s,2s,3s,4s docker compose up -d --build
```

The compressed retry backoff matters: at the shipped defaults a single acceptance test that asserts
the give-up rule takes forty minutes. See [`test/acceptance/README.md`](test/acceptance/README.md).

## How the project is organized

One Go binary contains the HTTP API, the console, the proxy and the CLI. Postgres holds state.

```
cmd/pando/              CLI and server entrypoint. Adapter registration happens here.
internal/
  core/                 Business logic. Adapters may not import any of this except adapter/api.
    authz/              Verb evaluation, both planes.
    audit/              Append-only event log.
    spec/               AppSpec types, validation, classified diffing.
    planner/            spec + policy + adapters -> plan, or a plan-time error.
    reconciler/         The loop and the state machine.
    state/              sqlc-generated queries and repository types.
  adapter/              The seven adapter categories, and their implementations.
  detect/               Build detection: the auction, the detectors, the trial run.
  proxy/                The identity-aware reverse proxy.
  httpapi/              chi handlers. No business logic.
  mcp/                  MCP server, a client of the same service layer as httpapi.
console/                React and TypeScript. Built into internal/console.
migrations/             golang-migrate, embedded in the binary.
test/acceptance/        The four end-to-end sequences.
```

## Documentation

Two sets, and the distinction matters:

| Path | Authority |
|---|---|
| [`docs/requirements.md`](docs/requirements.md) | What Pando is. 210 requirements, IDs `R-###`. Changes slowly. |
| [`docs/design/`](docs/design/) | How it is built. Nine documents, `00`–`08`. |
| [`docs/plan/`](docs/plan/) | Build order by phase, open decisions, risk register. |
| [`docs/traceability/`](docs/traceability/) | Generated index mapping requirements to design, code and tests. |
| [`CLAUDE.md`](CLAUDE.md) | Conventions, invariants, and the definition of done. |

Requirements are tagged **[D]** decided, **[P]** proposed, **[O]** open. Where the design contradicts
a requirement, the requirement takes precedence — or the requirement is amended in the same change,
never left to diverge silently.

Suggested reading order for a first change: requirements §1–3, then design `01` (the app spec, which
most of the system revolves around), then design `07` (the four end-to-end flows), then the design
document for the area you are working in.

If the documentation does not answer a question your change depends on, add it to
[`docs/plan/open-decisions.md`](docs/plan/open-decisions.md) and raise it in the pull request rather
than deciding it quietly.

## Invariants

§2 of [`CLAUDE.md`](CLAUDE.md) lists the project's invariants and the mechanism that enforces each
one — a database constraint, a lint rule, a trigger, a test. Read it before a change that touches
authorization, the proxy, the audit log or the state schema. Do not weaken one of those mechanisms to
make a test pass.

Two that catch people out:

- **Adapters cannot import `core/authz`, `core/audit`, `core/state` or `core/policy`.** This is a
  depguard rule in `make lint`, not a convention.
- **Builds never get a container runtime socket.** BuildKit runs rootless in its own container, and
  an integration test asserts on that container's actual mount list.

## Writing an adapter

Adapters are compiled into the binary and contributed by pull request; there is no external plugin
system, and none is planned. Seven categories: runtime, routing, builder, secrets, services,
identity, notifications.

Start with [`docs/design/03-adapter-interfaces.md`](docs/design/03-adapter-interfaces.md). Two rules
shape every adapter:

- **The app declares requirements; the adapter translates.** An app never mentions a provider's
  vocabulary, and neither does core. An adapter turns "2 GB, one persistent volume, one exposed HTTP
  port" into whatever its provider wants.
- **Capabilities are reported as data, not discovered by type assertion.** The planner uses them to
  reject an impossible combination before a deploy starts, with an error naming what to change.

## Pull requests

A change is complete when:

1. **It has an acceptance test named for the requirement it satisfies:**
   ```go
   // TestR132_UnfilledRequiredSlotBlocksDeploy asserts R-132.
   func TestR132_UnfilledRequiredSlotBlocksDeploy(t *testing.T) { … }
   ```
   `make requirements-coverage` reports which requirement IDs have one. If a requirement your change
   touches has no test, say so in the pull request and whether that is a gap or deliberate.
2. `make check` passes.
3. Any `[P]` default you overrode is noted in the design document, with the reason — not only in a
   code comment.

Cite requirement IDs in commit messages and comments where a non-obvious choice traces to one. `R-151`
in a comment explains an absent code path better than three sentences will.

## User-facing text

Error messages and console copy are held to one standard, in the API and the UI alike: say what
happened and what to do, with no apology and no `Error:` prefix. "Which port?" fails it. "This app
appears to be a Node.js service. Pando could not determine which port it serves HTTP on. Valid
answer: a port number such as 3000." passes it.

The console is built from the design system in `.claude/skills/pando-design/`. Colors, type, spacing
and radius come from its tokens; a raw hex value, a raw `px` value, or a font that is not Newsreader,
Public Sans or IBM Plex Mono fails `npm run check`.

## Licensing of contributions

Pando is dual-licensed, so contributions must be available under both licenses. By opening a pull
request you agree that your contribution may be distributed under the AGPL and under the commercial
license. You retain copyright; there is no CLA and no copyright assignment. See
[`LICENSING.md`](LICENSING.md).
