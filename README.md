<div align="center">

<img src="docs/assets/pando-mark.png" alt="Pando" width="88">

# Pando

**Self-hosted app deployment. Give it a git repository and it builds the app, runs it in a container,
and serves it behind a proxy that handles sign-in and access control.**

[![CI](https://github.com/bemeek-io/pando/actions/workflows/ci.yml/badge.svg)](https://github.com/bemeek-io/pando/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/bemeek-io/pando/branch/implementation/graph/badge.svg)](https://codecov.io/gh/bemeek-io/pando)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-B23A2C)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/go-1.27-B23A2C)](go.mod)
[![One binary](https://img.shields.io/badge/ships%20as-one%20binary-1A1C1B)](#stack)

</div>

<img src="docs/assets/console.png" alt="An app in the Pando console, showing its address, repository, deploy log and two warnings about its compose file" width="100%">

---

## What it is

Pando runs on one machine you control — a VPS, a home server, a laptop — and hosts web applications
on it. You give it a repository URL. It clones the repository, works out how the app should be built
and run, shows you what it worked out, and after you accept it, builds the app and starts it.

Every request to a hosted app goes through Pando's proxy, which signs the user in, decides whether
they are allowed through, and passes the app a signed token identifying them. Apps run on private
container networks and publish no ports of their own.

It is a single Go binary containing the HTTP API, the web console, the proxy and the CLI, plus a
Postgres database for state.

**Who it's for:** developers who want to deploy side projects and internal tools without configuring
a pipeline per project, and small teams who need something hosted and shared with specific people.

## Quickstart

Requires Docker and Docker Compose.

```bash
git clone https://github.com/bemeek-io/pando.git
cd pando
docker compose up -d
```

Pando and Postgres start together. Open **http://localhost:8080**.

### First sign-in

The first run creates an `admin` account and prints its password to the log once:

```bash
docker compose logs pando | grep '"first run"'
```

You will be asked to change it when you sign in.

If you miss that log line — a `down`/`up` or a `--force-recreate` discards it — you have two options:

```bash
# Set the initial password yourself. Read only on first run.
PANDO_ADMIN_PASSWORD=... docker compose up -d

# Or reset it later from the host. This ends every session for that account.
docker compose exec pando pando admin reset-password
```

`pando admin` talks to the database rather than the API, so it works when nobody can sign in. Every
use is written to the audit log.

### Deploying an app

In the console, choose **Add app** and paste a repository URL. Pando clones it and shows you a
proposal: which build method it chose, which port it thinks the app listens on, what services it
appears to need. You review it and accept. If something could not be determined, it asks rather than
guessing.

Apps are reachable on their own port on a default install, starting at `http://localhost:9000`. The
address is shown on the app's page. Compose publishes twenty ports by default;
`PANDO_APP_PORT_START` and `PANDO_APP_PORT_END` change the range, and they set both the ports
published by Compose and the range Pando allocates from.

## Interfaces

The HTTP API is the primary interface. The console, the CLI and the MCP server are all clients of it.

### Web console

At `http://localhost:8080`. Contains two views: a list of app tiles for people who only need to open
apps, and an admin interface for people who deploy and configure them. Which one you see depends on
your permissions.

The admin interface covers apps, sharing, environment variables, secrets, host policy, user accounts,
groups and roles, backups, the audit log, and a terminal into a running container.

### CLI

The same binary. `pando login` stores an API token for the machine.

```bash
pando login https://pando.example.com

pando app add https://github.com/you/notes    # clone, detect, propose
pando app list
pando app show notes
pando deploy notes                            # or: pando deploy ./local-directory
pando logs notes --follow
pando exec notes -- sh

pando slot set notes database --provision     # let Pando create the database
pando secret set notes STRIPE_KEY
pando grant add notes --user usr_01HQ8…       # give someone access
pando rollback notes                          # to the previous revision
pando export notes                            # the app's full spec, as JSON
```

Also `pando backup`, `pando policy`, `pando token`, and `pando admin reset-password`. Run
`pando <command> --help` for details.

### MCP server

For coding agents. Runs over stdio using the token from `pando login`:

```bash
pando mcp
```

Tools: `pando_list_apps`, `pando_get_app`, `pando_create_app`, `pando_get_detection`,
`pando_answer_detection`, `pando_accept_proposal`, `pando_plan`, `pando_deploy`, `pando_get_logs`,
`pando_get_status`.

An agent's token is subject to the same authorization as any other credential, and its actions are
recorded in the audit log against the token's owner. Running commands inside apps, reading secret
values and changing access are not exposed as MCP tools, and host policy denies them to tokens by
default.

### HTTP API

`/api/v1`, authenticated with a session cookie or a bearer token. Errors return a machine-readable
code, a description, and a suggested fix where one exists:

```json
{
  "code": "PLAN_SLOT_UNFILLED",
  "message": "This app needs a PostgreSQL database, and one hasn't been chosen yet.",
  "remedy": "Choose how to fill the database slot: provision one inside this app, connect to an existing one, or paste a connection string.",
  "details": { "slots": [{ "key": "database", "type": "postgres" }] },
  "request_id": "req_01HQ8…"
}
```

Documented in [`docs/design/04-api.md`](docs/design/04-api.md).

## Features

**Build detection.** Several detectors examine the repository and bid — Dockerfile, compose file,
static site, buildpack, an already-published image — and the best match becomes a proposal you review
before anything is built. A compose file is imported as written, including service ordering,
healthchecks, named volumes and the internal network. Anything Pando changes during import is listed
in the app's warnings.

**Five build methods.** A Dockerfile, a prebuilt image, a compose file (one image built per service),
a static site, and buildpacks through nixpacks, which generates a Dockerfile that BuildKit then
builds. Builds run in a rootless BuildKit container with no access to a container runtime socket.

**Authenticated proxy.** All traffic to hosted apps passes through Pando, which authenticates the
caller, checks authorization, and forwards a signed JWT describing the user. Apps can be private to
specific users or groups, or made public. Websockets and server-sent events are supported.

**Configuration in the database, not the repository.** How an app runs is stored as a spec revision
and nothing is read from the repository at deploy time. Revisions are append-only, so a rollback
targets a configuration that previously existed.

**Managed services.** Postgres, MySQL and Redis can be provisioned per app, with credentials
generated and injected as environment variables, and preserved across redeploys.

**Diagnostic warnings.** Pando reports problems it detects, such as an app that loads assets from the
domain root and will break under a path prefix, or a container writing outside a declared volume. It
reports them; it does not modify the app to work around them.

## Stack

Go 1.27, chi, zap, Postgres, sqlc, BuildKit, Docker. React and TypeScript for the console, compiled
into the binary with `embed.FS`.

Providers are implemented as adapters — runtime, routing, builder, secrets, services, identity,
notifications — and each reports its capabilities as structured data, so an unsupported combination
is rejected before a deploy starts rather than partway through.

## Non-goals

Pando is not a scheduler: it places workloads on a host, and multi-machine support would come from an
adapter that spans machines. It does not run tests. It is not an app marketplace, not a
disaster-recovery product with RPO/RTO guarantees, not multi-region, and not multi-tenant — one
installation serves one organization.

## Documentation

| Path | Contents |
|---|---|
| [`docs/requirements.md`](docs/requirements.md) | What Pando does and does not do. 210 requirements, IDs `R-###`. |
| [`docs/design/`](docs/design/) | Architecture, in nine documents, `00`–`08`. |
| [`docs/plan/`](docs/plan/) | Build order by phase, open decisions, risk register. |
| [`docs/traceability/`](docs/traceability/) | Generated index mapping requirements to design, code and tests. |
| [`CLAUDE.md`](CLAUDE.md) | Conventions, invariants and the definition of done, for contributors. |

Suggested reading order: requirements §1–3, then design `01` (the app spec), then design `07`
(end-to-end flows).

Requirements are tagged **[D]** decided, **[P]** proposed, **[O]** open. Where the design contradicts
a requirement, the requirement takes precedence, or the requirement is amended in the same change.

## Development

```bash
make help                    # all targets
make check                   # vet, lint, test — what CI runs
make console                 # build the console into the embedded assets
make test-integration        # real Postgres and Docker, via testcontainers
make requirements-coverage   # which requirements have a named acceptance test
```

`make lint` includes a depguard rule that fails the build if an adapter imports the authorization,
audit, state or policy packages.

Integration tests run against real Postgres and real Docker through `testcontainers-go`. The four
end-to-end sequences in [design 07](docs/design/07-sequences.md) are the acceptance criteria; they
need a running, freshly-created stack, described in
[`test/acceptance/README.md`](test/acceptance/README.md).

## Contributing

Adapters are compiled into the binary and contributed by pull request; there is no external plugin
system. Before writing one, read §2 of [`CLAUDE.md`](CLAUDE.md), which lists the project's invariants
and how each is enforced, and
[`docs/design/03-adapter-interfaces.md`](docs/design/03-adapter-interfaces.md).

A change is complete when it has an acceptance test named after the requirement it satisfies —
`TestR132_UnfilledRequiredSlotBlocksDeploy` — and `make check` passes.

## License

AGPL-3.0, with a commercial license available for embedding Pando in a proprietary product or
offering it as a hosted service without publishing modifications. See
[`LICENSING.md`](LICENSING.md) for which applies to you, and [`LICENSE`](LICENSE) for the full text.
