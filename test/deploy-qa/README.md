# Deploy QA

Deploys 169 apps through a throwaway Pando built from the current checkout and
reports which ones end up serving HTTP through Pando's proxy, with and without
the AI adapter. It measures the whole path a person takes: create, detect,
answer questions, accept, deploy, open the app.

The first run (2026-09-22, `main` @ 53d5939) and the fixes it asked for are in
issue #55. Its per-app results are in `baseline/2026-09-22.json`, and every
report shows what changed against that baseline.

The number to track is the pass rate **without** AI. The AI pass shows whether
screening helps or hurts on top of it; success is not meant to depend on it.

## What is tested

The test sources live in their own repository,
[bemeek-io/pando-qa-fixtures](https://github.com/bemeek-io/pando-qa-fixtures),
so this one does not carry their Dockerfiles, compose files and app configs.
`qa.py up` clones it into `out/fixtures` at the commit pinned in `qa.py`
(`FIXTURES_REF`).

| Set | Apps | What |
|---|---|---|
| `cases/generated.json` | 66 | Small apps written for this test (in `apps/`), each verified to run the normal way for its ecosystem: static sites, Node/Deno/Bun, Python, Go, Java, Rust, Ruby, PHP, .NET, Elixir, Dockerfile variants, compose topologies, and repositories Pando should refuse (a library, a CLI, a README) |
| `cases/public-repos.json` | 79 | Public GitHub repositories: official samples, getting-started repos, `docker/awesome-compose`, self-hosted apps |
| `cases/images.json` | 24 | Published images |

An app passes when it answers HTTP through the proxy (with its expected page,
where known), or, for a repository that is not a web app, when Pando declines to
deploy it. Detection questions are answered the way the app's author would,
from each case's `facts`.

## Running it

Needs Docker, Python 3.10+, about 60 GB of free disk, and no other Pando running
on the same Docker host (two installs on one host adopt each other's networks).

```bash
python3 test/deploy-qa/qa.py preflight     # checks the machine and prints the plan; changes nothing
python3 test/deploy-qa/qa.py up            # builds this checkout into compose project pando-qa on :8199
python3 test/deploy-qa/qa.py run --pass noai
python3 test/deploy-qa/qa.py dashboard     # in a second terminal: progress, ETA, success rate, CPU/memory/disk
python3 test/deploy-qa/qa.py report        # out/report.html and out/summary.md
python3 test/deploy-qa/qa.py down          # removes everything the QA instance created
```

For the AI pass, export `ANTHROPIC_API_KEY` and run `run --pass ai` (or
`--pass both`). The key goes to the QA instance as a sealed credential; it is
not written to disk or printed. `run` resumes where it left off; `--fresh`
starts a pass over, `--only id1,id2` runs specific cases.

A full pass takes about 45–60 minutes at 25 apps at a time.

## What keeps it from filling your disk or breaking Docker

- **Preflight** refuses to start without enough disk and memory, or while another
  Pando is running, and sizes concurrency from Docker's memory, CPUs and network
  pool.
- **Every app is removed completely after its case** — containers, volumes,
  network, images, and its data inside Pando — and then checked: if anything
  carrying the app's ID remains, the run stops. Pando's own delete leaves most of
  this behind (issue #55), so the harness does it.
- **A disk gate** holds new cases while free disk is under 40 GB, and the build
  cache is trimmed to 20 GB every 10 cases.
- **Another install's networks are protected.** The QA instance's startup removes
  empty Pando networks, including another install's; a placeholder container
  keeps each of them non-empty until `down`.
- **A watchdog** restarts the QA instance if nothing finishes for 6 minutes (the
  git-fetch hang in #55) and re-runs the cases that were in flight.
- **`down`** removes only what carries an app ID from the QA instance's own
  database, plus images that were not on the machine before `up`.

Test settings that differ from Pando's defaults: 0.25 CPU per app (1 CPU would
cap a 12-CPU host at 12 apps), at most 4 clones and 12 builds at once, and a
hand-written spec for published images.

## Keeping it honest

Every failure is assigned one cause (`lib/classify.py`). After a run, look for
failures classified as "Other" and add a rule, or a per-app entry with the
evidence, so the causes keep adding up to the failure count. Environment
failures (transient pulls, build OOM kills, the git-fetch hang) are re-run once
automatically.

To add a case, commit it to bemeek-io/pando-qa-fixtures (an entry in one of its
`cases/*.json` files, and for a generated app its directory under `apps/`, after
checking it runs on its own), then move `FIXTURES_REF` in `qa.py` to that
commit.
