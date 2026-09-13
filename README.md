<div align="center">

<img src="docs/assets/pando-mark.png" alt="Pando" width="88">

# Pando

**Host your apps on a host you own — without setting up deployment more than once.**

[![CI](https://github.com/bemeek-io/pando/actions/workflows/ci.yml/badge.svg)](https://github.com/bemeek-io/pando/actions/workflows/ci.yml)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-B23A2C)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/go-1.27-B23A2C)](go.mod)
[![One binary](https://img.shields.io/badge/ships%20as-one%20binary-1A1C1B)](#stack)

Point it at a repository. It works out how to build and run the app, gives it an address, and serves
it behind an identity-aware proxy that every request goes through.

</div>

<img src="docs/assets/console.png" alt="An app in the Pando console: its address, its repository, its deploy log, and two warnings about what its compose file asked for that Pando did not carry over" width="100%">

---

## Status

**Built and running.** All eleven phases are implemented, and the four end-to-end sequences in
[design 07](docs/design/07-sequences.md) run against a real Compose stack with real Docker and real
Postgres. One design question is still open — **O-4**, which needs a measurement rather than a
decision (see [`docs/plan/open-decisions.md`](docs/plan/open-decisions.md)).

It has not been run anywhere but its authors' machines. Treat it accordingly.

## Why

Deployment tooling usually charges its setup cost per app. A pipeline, a tunnel, a DNS record, a
certificate, a secret store — each one again, slightly differently, for every project.

**Pando charges that cost once, at the host.** Deploying the tenth app should feel like nothing.

That serves two people with the same product and no tiers. Someone who builds apps and does not want
to spend as long deploying them as building them. And someone — often non-technical — who built
something useful and needs it hosted safely and shared with a few coworkers, or with everyone.
Enterprise capability comes from host configuration, not a different edition.

## Quickstart

You need Docker, and nothing else.

```bash
git clone https://github.com/bemeek-io/pando.git
cd pando
docker compose up -d
```

Pando and Postgres start together. The console is at **http://localhost:8080**.

**Signing in the first time.** First run creates one account, `admin`, and prints its password once:

```bash
docker compose logs pando | grep '"first run"'
```

You are made to change it at first sign-in, so the printed one is a way in rather than a credential.

That line lives in the log of the container that printed it, so a `down`/`up` or a `--force-recreate`
loses it. Two ways around that:

```bash
# Choose it up front. Read only on first run, and still changed at first sign-in.
PANDO_ADMIN_PASSWORD=... docker compose up -d

# Or set a new one later, from the host. Ends every session that account has.
docker compose exec pando pando admin reset-password
```

`pando admin` runs against the database rather than the API, because it exists for the case where
nobody can sign in. Host shell access is the authorization — whoever can run it can already read the
database credentials — and every use is written to the audit log.

### Adding an app

Add app, paste a repository URL, and Pando clones it and proposes how to build and run it. **It
proposes; you accept.** The proposal names what it found and why, and anything it could not work out
becomes a question with a real answer rather than a guess.

Apps get their own port on a default install — `http://localhost:9000` upward, shown on the app's
page. Twenty ports are published; `PANDO_APP_PORT_START` and `PANDO_APP_PORT_END` widen the range,
and they set both what Compose publishes and what Pando hands out, because an app given a port
outside the published range has an address nothing can reach.

## What it does

**Works out how to build your app, and shows its work.** Detectors bid against the repository —
Dockerfile, compose file, static site, buildpack, a published image — and the winner becomes a
proposal you can read. A compose file is treated as a complete answer and imported as written, not as
a hint. Anything Pando changes on the way in, it says so, in the app's own warnings.

**Five ways to build, all of them real.** A Dockerfile, an image you already publish, a compose file
(one image per service), a static site, and buildpacks via nixpacks — which generates a Dockerfile
and stops, so BuildKit builds the result and no build ever touches a container runtime socket.

**One way in.** Every request to every app goes through Pando's proxy, which authenticates the caller,
makes the authorization decision, and hands the app a signed assertion saying who is there. There is
no bypass — not for public apps, not for websockets. Apps are isolated from each other on private
networks and publish nothing to the host.

**The state store is the record.** Nothing is read from the repository at deploy time. How an app runs
lives in a spec revision, revisions are append-only, and a rollback is always to something that
provably existed.

**It observes; it does not remediate.** Pando will tell you that an app loads its assets from the top
of the domain and will come up blank under a path prefix. It will not rewrite the page to hide it.

## Stack

Go, chi, zap, Postgres, sqlc, BuildKit, Docker. React and TypeScript for the console, embedded into
the binary with `embed.FS`. **One binary ships the API, the console, the proxy and the CLI.**

Providers are adapters — runtime, routing, builder, secrets, services, identity, notify — and core
never learns a provider's vocabulary. An adapter states what it can do as data, so an impossible
request fails at plan time with a message you can act on rather than halfway through a deploy.

## What Pando is not

Permanent non-goals. A request falling into one of these is answered "no", not "later".

It is **not a scheduler** — it places, it does not schedule. **It does not test.** It is **not a
marketplace**, **not a disaster-recovery product** with RPO/RTO guarantees, **not multi-region**, and
**not multi-tenant** — one install serves one organization.

## Documentation

| Path | What |
|---|---|
| [`docs/requirements.md`](docs/requirements.md) | What Pando is. 210 requirements, IDs `R-###`. The authority. |
| [`docs/design/`](docs/design/) | How it is built. Nine documents, `00`–`08`. |
| [`docs/plan/`](docs/plan/) | Build order, one file per phase. Open decisions. Risk register. |
| [`docs/traceability/`](docs/traceability/) | Generated: every requirement, where it is designed, when it is built, what proves it. |
| [`CLAUDE.md`](CLAUDE.md) | **Start here if you are building this.** Invariants, conventions, definition of done. |

**Reading order:** requirements §1–3 → design `01` (the spec, the center of the system) → design `07`
(four end-to-end flows) → whatever you are building.

Requirements are tagged **[D]** decided, **[P]** proposed, **[O]** open. When design contradicts a
requirement, the requirement wins — or the requirement is amended in the same change. Never a silent
divergence.

## Development

```bash
make help                    # all targets
make check                   # vet, lint, test — what CI runs
make console                 # build the console into the embedded assets
make requirements-coverage   # which requirements have a named acceptance test
```

`make lint` enforces the adapter import boundary as a depguard rule rather than a convention: an
adapter that imports authorization, audit or state fails the build.

Integration tests run against real Postgres and real Docker through `testcontainers-go`. The four
sequences in [design 07](docs/design/07-sequences.md) are the acceptance criteria — if those pass end
to end, v1 works. That suite needs a running, fresh stack; see
[`test/acceptance/README.md`](test/acceptance/README.md).

## Contributing

Adapters are compiled in-tree and contributed by pull request — there is no external plugin protocol
and none is planned. Two things to read first: §2 of [`CLAUDE.md`](CLAUDE.md), which lists the
invariants and the mechanism enforcing each one, and
[`docs/design/03-adapter-interfaces.md`](docs/design/03-adapter-interfaces.md).

A change is done when it has an acceptance test named for the requirement it satisfies
(`TestR132_UnfilledRequiredSlotBlocksDeploy`), and `make check` passes.

## License

**AGPL-3.0**, dual-licensed. A commercial licence is available for embedding Pando in a proprietary
product or offering it as a hosted service without publishing modifications — see
[`LICENSING.md`](LICENSING.md), and [`LICENSE`](LICENSE) for the full text.

AGPL → MIT is reversible; MIT → AGPL is not.
