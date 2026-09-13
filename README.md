# Pando

Pando hosts your apps on a host you own, without you having to set up deployment pipelines, tunnels,
or DNS more than once. Point it at a repo; it works out how to build and run the app, and serves it
behind an identity-aware proxy that every request goes through.

**The defining property: setup cost is paid once, at the host.** Deploying the tenth app should feel
like nothing.

Two audiences, one product, no tiers: someone who builds apps and does not want to spend as long
deploying them as building them, and someone — often non-technical — who built something useful and
needs it hosted securely and shared with coworkers or the public. Enterprise capability comes from
host configuration, not from a different edition.

---

## Status

**All eleven phases are built.** One binary serves the API, the console, the proxy and the CLI; the
four end-to-end sequences in [design 07](docs/design/07-sequences.md) run against a real Compose stack
with real Docker and real Postgres.

Two open questions remain and neither is a decision: **O-4** needs a measurement (slot detection
against a corpus with crashing apps in it) and **O-5** is per-adapter by design (TLS issuance). See
[`docs/plan/open-decisions.md`](docs/plan/open-decisions.md).

## Running it

```bash
docker compose up -d
```

Pando and Postgres start together. The console is on `http://localhost:8080`.

**Signing in the first time.** First run creates one account, `admin`, and prints its password once:

```bash
docker compose logs pando | grep '"first run"'
```

You are made to change it at first sign-in, so the printed one is a way in rather than a credential.

That line lives in the log of the container that printed it, so a `down`/`up` or a `--force-recreate`
loses it — the account already exists, and nothing is printed again. Two ways around that:

```bash
# Choose it up front. Only read on first run, and still changed at first sign-in.
PANDO_ADMIN_PASSWORD=... docker compose up -d

# Or set a new one later, from the host. Ends every session that account has.
docker compose exec pando pando admin reset-password
```

`pando admin` runs against the database rather than the API, because it exists for the case where
nobody can sign in. Host shell access is the authorization — whoever can run it can already read the
database credentials — and every use writes to the audit log.

## Where things are

| Path | What |
|---|---|
| [`CLAUDE.md`](CLAUDE.md) | **Start here if you are building this.** Invariants, conventions, definition of done. |
| [`docs/requirements.md`](docs/requirements.md) | What Pando is. 207 requirements, IDs `R-###`. The authority. |
| [`docs/design/`](docs/design/) | How it is built. Nine documents, `00`–`08`. |
| [`docs/plan/`](docs/plan/) | Build order, one claimable file per phase. Open decisions. Risk register. |
| [`docs/traceability/`](docs/traceability/) | Generated: every requirement, where it is designed, when it is built, what proves it. |
| `internal/`, `cmd/`, `migrations/`, `console/` | The scaffold. Each package's `doc.go` states its contract. |

**Reading order for a new engineer:** requirements §1–3 → design `01` (the spec, the center of the
system) → design `07` (four end-to-end flows) → whatever you are building.

## Stack

Go, chi, zap, Postgres, sqlc, BuildKit, Docker. React + TypeScript + Vite for the console, embedded
into the binary. One binary ships everything.

## Working on it

```bash
make help                    # all targets
make check                   # vet, lint, test — what CI runs
make requirements-coverage   # which requirements have a named acceptance test
make requirements-index      # regenerate the traceability index
```

`make lint` enforces the adapter import boundary (R-027) as a depguard rule, not a convention.

The end-to-end suite needs a running stack and a fresh one — see
[`test/acceptance/README.md`](test/acceptance/README.md), which also explains why the retry backoff is
worth compressing while you iterate.

## What Pando is not

Permanent non-goals. A request falling into one of these is answered "no," not "later."

It is **not a scheduler** — it places, it does not schedule. **It does not test.** It is **not a
marketplace**, **not a disaster-recovery product** with RPO/RTO guarantees, **not multi-region**, and
**not multi-tenant** — one install serves one organization.

## License

AGPL, dual-licensed with commercial exceptions available. AGPL → MIT is reversible; MIT → AGPL is not.
