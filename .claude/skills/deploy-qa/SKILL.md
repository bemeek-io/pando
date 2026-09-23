---
name: deploy-qa
description: Run Pando's deploy QA — deploy ~170 real and generated apps (static, Node, Python, Go, Java, Rust, Ruby, PHP, .NET, Dockerfiles, compose stacks, published images) through a throwaway Pando built from the current checkout, with and without the AI adapter, and report the success rate by source, language and packaging with a cause for every failure. Use when asked to test detection/deploys end to end, measure the pass rate, check whether fixes from issue #55 landed, or compare AI vs no-AI. Handles cleanup and disk safety; read before running anything.
---

# Deploy QA

The tool is `test/deploy-qa/qa.py`; `test/deploy-qa/README.md` explains what it
tests and why. This file is how to run it without harming the machine it runs on.
The last full run and the fixes it asked for are issue #55.

The test sources (the generated apps and the lists of public repositories and
images) are in bemeek-io/pando-qa-fixtures, cloned at the commit pinned as
`FIXTURES_REF` in `qa.py`. Never add app configs, Dockerfiles or compose files
for test apps to this repository; to add or change a case, commit to the
fixtures repository and move the pin.

## Rules

These exist because the first run broke them. Do not relax them.

- **Never run two Pandos on one Docker host.** `preflight` refuses while another
  Pando server is running. Stopping someone's Pando is their decision: tell the
  user the exact `docker stop` command preflight printed and wait for them.
  Afterwards remind them to `docker start` it.
- **Only remove what the QA instance created.** Use `qa.py down`; never
  `docker system prune`, `docker volume prune -a`, `docker rm` by label, or any
  cleanup of containers, volumes, networks or images you did not create. Another
  install's app containers carry the same `io.pando.*` labels.
- **Never kill a process by port or name.** Stop only PIDs you started.
- **Never print, log or write the Anthropic API key.** It is read from
  `ANTHROPIC_API_KEY` in the environment of the `run` command. If the user pastes
  one into chat, pass it only as that environment variable on that one command.
- **Do not run long blocking commands while the user is waiting for an answer.**
  Start the run in the background and answer questions from the log and results
  files.
- **If the run stops for cleanup residue, do not delete the residue by hand and
  resume.** Report what was left and why; residue means the cleanup is wrong.

## Steps

1. **Preflight.** `python3 test/deploy-qa/qa.py preflight`. Relay any PROBLEM
   lines to the user as-is; each names the fix. Notes (memory, network pool) are
   informational: say what concurrency the run will use and how to raise it.
2. **Up.** `python3 test/deploy-qa/qa.py up` builds the checkout that is under
   test, so check out the ref the user wants first and say which one it is. It
   serves on http://localhost:8199; the admin password is in
   `test/deploy-qa/out/qa.env`.
3. **Run in the background** and give the user the dashboard command:
   ```bash
   nohup python3 test/deploy-qa/qa.py run --pass noai > test/deploy-qa/out/run-console.log 2>&1 &
   python3 test/deploy-qa/qa.py dashboard            # the user runs this in their own terminal
   ```
   For the AI pass, the user must supply a key: `ANTHROPIC_API_KEY=… qa.py run
   --pass ai` (or `--pass both`). Ask for it only when you reach that pass. Watch
   `test/deploy-qa/out/run-<pass>.log` with a monitor that fires on `RESIDUE`,
   `disk gate`, `watchdog`, `Traceback`, `stopped:` and `all done`, not on every
   line.
4. **Report.** `qa.py run` writes the report when it finishes; `qa.py report`
   rebuilds it. Open `test/deploy-qa/out/report.html` or publish it as an
   artifact, and paste `out/summary.md` into the relevant issue if asked. Before
   presenting numbers, check the causes: no failure should be classified "Other"
   (add a rule or a per-app entry in `lib/classify.py` with the evidence), and
   every failure count must add up.
5. **Down.** `python3 test/deploy-qa/qa.py down` when the user is done looking at
   the instance. Confirm free disk afterwards and remind them to restart any
   Pando they stopped.

## Reading the results

- The headline is the pass rate **without AI**, and its change since the
  baseline in `test/deploy-qa/baseline/`. Success is not meant to depend on the AI
  adapter; the AI pass shows only whether screening helps or hurts on top.
- Report regressions (passed without AI, failed with it) individually with the
  amendment screening applied and the deploy log line. Check whether the failure
  was environmental (out-of-memory build, Docker error, pull failure) before
  attributing it to the AI.
- "Not Pando's fault" is a narrow bucket: upstream repositories that no longer
  build, apps needing configuration no one supplied, compose files Pando blocks
  on purpose, and test-environment failures.
- When a run's results should become the new reference (for example after a
  batch of fixes lands on `main`), run `qa.py baseline --issue <n>` and commit the
  new `test/deploy-qa/baseline/<date>.json`.
