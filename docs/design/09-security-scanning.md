# 09 — Security scanning

Requirements §23 (R-310 – R-320). This document is how the score is produced, stored, enforced and
undone.

---

## 1. The shape of it

A scan is a **fact about one spec revision and the image built from it**, produced by an adapter,
stored append-only beside the app, and read by three places: the planner, the reconciler, and the
console.

```
deploy ──▶ build ──▶ scan ──▶ score ──▶ plan check ──▶ run
                       │                    │
on demand ─────────────┘                    └── refuse: PLAN_SECURITY_BELOW_THRESHOLD
policy change ──────────────────────────▶ re-evaluate the stored score, never rescan
```

**[D] A policy change re-evaluates; it never rescans.** Lowering the threshold must not queue a scan
of every app on the host, and the score of a revision does not change because the rule changed.

**[D] The scan does not gate the build.** It runs after the image exists, because the image is what
it has to look at, and because a build that succeeded is worth keeping even when its result is not
deployable.

---

## 2. The adapter (R-317)

A seventh adapter category, in `adapter/api`:

```go
type Scanner interface {
    Adapter
    Scan(ctx context.Context, req ScanRequest) (ScanResult, error)
    ScannerCapabilities() ScannerCapabilities
}

type ScanRequest struct {
    AppID    string
    SpecID   string
    Image    string // what was built, when there is one
    SourceDir string // the checkout, for scanners that read the tree
}

type ScanResult struct {
    Findings []Finding
    Scanner  string // what produced it, and at what version
    Ran      time.Time
}

type Finding struct {
    ID       string   // CVE-2024-…, or the rule that fired
    Severity Severity // critical | high | medium | low | unknown
    Title    string
    Target   string   // the package, file or layer it is in
    Fix      string   // the version that fixes it, when the scanner knows
}
```

**[D] Severity is Pando's vocabulary, not the scanner's.** Every scanner has its own scale and its
own extra levels; the adapter maps into these five and the score arithmetic never sees a string it
has not defined. `unknown` scores as `low` and is shown as unknown — inventing a severity for a
finding whose severity nobody has decided is how a score stops meaning anything.

**[P] The first adapter is Trivy**, in a container, with no daemon and no socket: it reads the image
from the local runtime and the source from a directory bind. It reports vulnerable dependencies,
leaked secrets and misconfiguration. Static analysis of the app's own code is **O-19** — a different
class of tool, per-language and noisy, and a score that swings on lint opinions is a score people
learn to ignore.

**[D] The scanner is not given the app's secrets, its network, or its volumes.** It sees an image and
a checkout. A component that scans untrusted code is the last component that should hold anything.

---

## 3. The score (R-313)

```
score = max(0, 100 − 25·critical − 10·high − 3·medium − 1·(low + unknown))
```

Deliberately not an average, a ratio, or a curve: one critical finding costs more than fifty low
ones, and the number a threshold is set against does not move because an app grew.

**[D] A score belongs to a revision, not to an app.** `app_scans` is append-only, keyed by
`(app_id, spec_id, scanner_ref, ran_at)`, and the app's current score is the newest scan of its
pinned revision. A rollback therefore restores the score of what it rolled back to, with no rescan
and no lag.

**[D] No score is not a score of zero.** Unscanned, unscannable and "scanned and found nothing" are
three different states and the model keeps them apart (R-318).

---

## 4. Enforcement

### 4.1 At deploy (R-314)

In the planner, beside the other plan-time refusals, so it fails before anything is created:

- Threshold set, scanner configured, newest scan of the revision being deployed is **below** it →
  `PLAN_SECURITY_BELOW_THRESHOLD`, with the score, the threshold and the three findings that cost
  the most in `details`, and a remedy naming both ways forward: fix the findings, or ask whoever
  holds `install.policy.manage`.
- Threshold set, scanner configured, **no scan at all** for that revision → the same refusal with a
  different message: it has never been scanned, and here is how to scan it.
- Threshold set, **no scanner configured** → nothing is refused (R-317). The policy screen says the
  threshold is inert, because a rule that silently blocks everything is worse than a rule that is
  visibly off.
- Scan failed → the previous score stands (R-318).

### 4.2 While running (R-315, R-316)

The reconciler's own pass, not the deploy path, because the trigger is usually neither: a policy
change, or a rescan that found something new in an app nobody has touched for a month.

```
insecure_since = the first time this app was found below the threshold
```

- Found below → set `insecure_since` if unset, warn, notify the owner, audit.
- Found at or above → clear `insecure_since`, audit the recovery.
- `insecure_since` older than the grace, and policy says stop → `desired_state = stopped`, audited
  with the score, the threshold and the grace it was given.

**[D] Stopping is `desired_state = stopped`.** Reversible, survives a restart, and visible in the
console as a stopped app with a reason. Never a delete, never an edit to the app's configuration,
and never `failed` — `failed` is terminal and means the reconciler gave up (R-151), which is not
what happened here.

**[D] The grace is stated in the policy and shown to the owner at the moment it starts.** A deadline
nobody was told about is an outage with extra steps.

---

## 5. Host policy

Three fields, defaulting to off, following R-272's pattern — a permissive default that policy may
raise:

| Field | Default | Meaning |
|---|---|---|
| `min_security_score` | `0` | Below this, a deploy is refused. `0` is off. |
| `insecure_action` | `warn` | `warn` or `stop`. |
| `insecure_grace_hours` | `24` | How long an app has after it is first found insecure, before `stop` applies. |

**O-20** is whether a score ages: nothing rescans an app that has not been deployed since, so a
clean score can describe three-week-old vulnerability data. A scheduled rescan is the obvious answer
and needs a decision about what it costs on a small host.

---

## 6. Surfaces

**Console.** The score lives on the app's **overview**, with its status, address and deploy log — the
score is a property of what is running, and the person who has to act on it is the one reading that
page. The section carries the badge, the counts, when it was taken, every finding, and **Scan now**.
It is not in settings: settings is where an app is configured, and a score is not a setting.

The **apps list** carries the badge too, at `score / 100`, beside status — which is the question a
list is for: which of these needs me.

**[D] The badge always contains the number** (R-320). Color is the second signal, never the only one:
"F" tells a deployer nothing they can act on, and a red pill tells somebody who cannot see red
nothing at all. The color says what the number means *here* — against the installation's threshold
where one is set, and against bands where none is, because a score of 20 is worth noticing on an
installation that enforces nothing.

Host policy's screen carries the three fields, with the consequence written at the point of setting
them.

**API.** `GET /apps/{id}/security` for the current score and findings, `POST /apps/{id}/security/scan`
to take a new one. Both behind `app.view` and `app.deploy` respectively — asking for a scan changes
what a deploy will do, so it is not a read.

**CLI and MCP.** They follow from the API without further design (R-261); the MCP tool is a read,
because an agent asking Pando to rescan until it passes is a loop nobody wants.

---

## 7. What the first implementation does, and does not

**Deploy-time scans read the image and the checkout.** Both are in hand at that
point: the image was just built, and the source is still on disk. **An on-demand
rescan reads the image only** — the one the app's newest successful deploy
shipped, recorded on the deployment row. Re-fetching a repository to rescan its
tree is a clone per rescan, and the findings that only appear in a tree
(committed secrets, misconfiguration) do not change without a commit, which is
a deploy. Revisit if that turns out to be wrong.

**The scanner is seeded on and enforces nothing.** A fresh installation gets
Trivy as its default scanner, so every deploy produces a score — and
`min_security_score` defaults to 0, so nothing is refused until an administrator
sets one (R-270). A score nobody has is a score nobody acts on, and a threshold
nobody chose is an installation that cannot deploy.

**The scanner's handoff is a file, not a socket.** Pando saves the image with
the runtime it already has a socket for, and copies the tar into the scanner's
own filesystem. Two things were learned building it:

- A source file's mode travels through a tar. A checkout copied in at 0600
  owned by a host uid was unreadable inside the scanner, which reported a clean
  tree — a pass nobody earned, and the worst failure this component has. Modes
  and ownership are normalized on the way in.
- The category had to reach the database. `adapter_configs.category` carries a
  CHECK enumerating the categories, and seeding the default scanner against a
  schema that had never heard of `scanner` failed at startup, on every restart.
  Migration 000016 is that fix; the lesson is in 000010's comment already and is
  worth repeating: a category that exists only in Go is a category no adapter
  can be configured in.

**Not built yet.** A scheduled rescan (O-20), static analysis of the app's own
code (O-19), and the plan-time half of R-314 — `POST /apps/{id}/plan` does not
yet report the standing, so a below-threshold app is refused when its deploy
reaches the scan rather than at `:plan`. The refusal is the same error with the
same detail either way; what is missing is the earlier warning.
