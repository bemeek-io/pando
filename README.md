<div align="center">

<img src="docs/assets/pando-mark.png" alt="Pando" width="88">

# Pando

**Deploy and share apps**

[![CI](https://github.com/bemeek-io/pando/actions/workflows/ci.yml/badge.svg)](https://github.com/bemeek-io/pando/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/bemeek-io/pando/branch/implementation/graph/badge.svg)](https://codecov.io/gh/bemeek-io/pando)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-B23A2C)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/go-1.27-B23A2C)](go.mod)

</div>

<img src="docs/assets/console.png" alt="An app in the Pando console, showing its address, repository, deploy log and two warnings about its compose file" width="100%">

---

## What is Pando?

Pando is a deployment platform you run on one machine you control — a VPS, a home server, a laptop.
It takes the place of a hosting provider for web applications: you give it a git repository, and it
handles building the app, running it, giving it an address, and controlling who can reach it.

It is aimed at people who deploy a handful of applications rather than hundreds: side projects,
internal tools, small products. The work of setting up builds, networking and access happens once
when you install Pando, rather than once per application.

What it does:

- **Deploys from a repository URL.** Pando clones it, works out how the app should be built and run,
  and shows you what it worked out before building anything.
- **Builds five ways**, picked automatically: a Dockerfile, a compose file, an image you already
  publish, a static site, or a buildpack for apps with none of those. Compose files are imported as
  written, including multiple services, healthchecks, volumes and startup order.
- **Puts sign-in in front of every app.** Apps are private by default. Share one with specific people
  or groups, or make it public. Your app receives a signed token describing who is visiting, so it
  does not need its own login.
- **Creates databases on request.** Postgres, MySQL and Redis can be provisioned per app, with
  credentials generated, injected as environment variables, and kept stable across redeploys.
- **Manages secrets, environment variables, volumes and resource limits** per app.
- **Gives you logs, a terminal into a running container, and one-click rollback** to any previous
  configuration.
- **Backs up app storage**, including automatically before an app is deleted.
- **Records an audit log** of everything anyone did, which cannot be edited or deleted.

## Install

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

`pando admin` talks to the database rather than the API, so it works when nobody can sign in.

### Deploying your first app

In the console, choose **Add app** and paste a repository URL. Pando clones it and shows you a
proposal: which build method it chose, which port it thinks the app listens on, which services it
appears to need, and what it is unsure about. Review it and accept. Anything it could not determine
becomes a question rather than a guess.

Apps are reachable on their own port, starting at `http://localhost:9000`, and the address is shown
on the app's page. Twenty ports are published by default; `PANDO_APP_PORT_START` and
`PANDO_APP_PORT_END` change the range.

## Console

`http://localhost:8080`. Two views, depending on your permissions: a page of app tiles for people who
only need to open apps, and an admin interface for people who deploy and configure them.

The admin interface covers apps and their configuration, sharing and access, environment variables
and secrets, host policy, user accounts, groups and roles, backups, the audit log, and a terminal
into any running container.

## CLI

The CLI is the same binary as the server, so you can install it on your own machine:

```bash
go install github.com/bemeek-io/pando/cmd/pando@implementation
```

Or use the one already inside the container, via `docker compose exec pando pando …`.

```bash
pando login https://pando.example.com    # stores an API token for this machine

pando app add https://github.com/you/notes
pando app list
pando app show notes
pando deploy notes                       # or: pando deploy ./local-directory
pando logs notes --follow
pando exec notes -- sh

pando slot set notes database --provision   # let Pando create the database
pando secret set notes STRIPE_KEY
pando grant add notes --user usr_01HQ8…     # give someone access
pando rollback notes                        # to the previous configuration
pando export notes                          # the app's full spec, as JSON
```

Also `pando backup`, `pando policy` and `pando token`. Run `pando <command> --help` for details, and
`--server` to talk to an installation other than the one you logged into.

## MCP server

Pando exposes its API to coding agents over MCP, so an agent can deploy and inspect apps directly.
It runs on your machine over stdio and uses the token from `pando login`.

```bash
pando login https://pando.example.com
```

**Claude Code:**

```bash
claude mcp add pando -- pando mcp
```

**Any other MCP client**, in its config file:

```json
{
  "mcpServers": {
    "pando": {
      "command": "pando",
      "args": ["mcp"]
    }
  }
}
```

Tools: `pando_list_apps`, `pando_get_app`, `pando_create_app`, `pando_get_detection`,
`pando_answer_detection`, `pando_accept_proposal`, `pando_plan`, `pando_deploy`, `pando_get_logs`,
`pando_get_status`.

An agent's token carries the same permissions you do and no more, and everything it does appears in
the audit log under your name. Running commands inside apps, reading secret values and changing who
has access are not available as MCP tools, and are denied to tokens by host policy by default.

## HTTP API

Everything above is a client of `/api/v1`, which you can use directly with a session cookie or a
bearer token from `pando token`. Errors return a machine-readable code, a description, and a
suggested fix:

```json
{
  "code": "PLAN_SLOT_UNFILLED",
  "message": "This app needs a PostgreSQL database, and one hasn't been chosen yet.",
  "remedy": "Choose how to fill the database slot: provision one inside this app, connect to an existing one, or paste a connection string.",
  "details": { "slots": [{ "key": "database", "type": "postgres" }] },
  "request_id": "req_01HQ8…"
}
```

Reference: [`docs/design/04-api.md`](docs/design/04-api.md).

## Configuration

Set on the `pando` service in `docker-compose.yml`, or in the environment.

| Variable | Default | Purpose |
|---|---|---|
| `PANDO_PORT` | `8080` | Port the console and API are served on. |
| `PANDO_ADMIN_PASSWORD` | generated | Initial admin password. Read only on first run. |
| `PANDO_APP_PORT_START` / `_END` | `9000` / `9019` | Range of host ports apps are given. Sets both what Compose publishes and what Pando allocates. |
| `PANDO_BASE_DOMAIN` | `localtest.me` | Domain per-app subdomains are taken from, when using hostname routing. |
| `PANDO_DATABASE_URL` | the bundled Postgres | Point Pando at an existing database instead. |

## What Pando does not do

It does not schedule across machines: one installation runs apps on its host, and multi-machine
support would come from a runtime adapter that spans machines. It does not run your tests. It is not
an app marketplace, not a disaster-recovery product with RPO/RTO guarantees, not multi-region, and
not multi-tenant — one installation serves one organization.

It reports problems rather than working around them. If an app loads its assets from the domain root
and will break under a path prefix, Pando says so; it does not rewrite the app's pages.

## Contributing

Bug reports and pull requests are welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for how the
project is organized, how to build it, and what a change needs before it can be merged.

## License

AGPL-3.0, with a commercial license available for embedding Pando in a proprietary product or
offering it as a hosted service without publishing modifications. See
[`LICENSING.md`](LICENSING.md) for which applies to you, and [`LICENSE`](LICENSE) for the full text.
