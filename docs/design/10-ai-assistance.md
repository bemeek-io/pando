# 09 — AI Assistance

R-106 has been in the requirements since draft 1 and has never had a design. It says AI assistance is
optional supporting functionality, names three places it may be applied, and sets the constraint that
matters: it emits the same spec object and passes the same review gate, and with nothing configured
each tap degrades to a question rather than a dead end.

This document designs the tenth adapter category (R-258) and the first function it performs:
**screening a deployment plan** (§7.4, R-330 through R-339). The other two functions R-106 names —
reading README prose and proposing repairs from a failed build log — are not built here. The
capabilities struct is shaped so they arrive without changing the interface.

---

## 1. What screening is

Detection produces a proposal: a draft spec, the evidence behind it, the questions it could not
answer, and the trial run's log. It is deterministic — the auction is a pure function of the source,
and §07 A is the sequence it follows.

Screening runs **after** that, on the finished proposal, and asks one question: *given this repository,
what did that proposal get wrong or leave out?* The answer comes back as amendments to the spec.

**[D] It is not a detector and does not bid** (R-330). The tempting design is a tenth detector that
bids in the auction alongside the Dockerfile and compose ones, and it is wrong in two directions. A
model that bids competes with evidence it should be using: the compose detector's reading of a compose
file is better than a model's, and a confidence score is the wrong way to express "that reading is
right and it missed the `depends_on`". And it would make the auction non-deterministic, which is the
one property that makes a proposal explainable — R-102 promises the user sees how the answer was
reached, and "a model ranked it highest" is not that.

**[D] What it is for is R-103.** R-103 counts questions per deploy. The related number is how often
an app that deployed cleanly then failed to start. Both usually come from something in the repository
that no detector checks: a start script, a bind address, an environment variable named only in a
README. Each detector handles the layouts it was written for, so there will always be repositories
outside that set. Screening reads the repository for this reason, rather than only the spec.

### 1.1 What it is applied to

**[D] The draft spec, not the bundle plan.** "The deployment plan" is ambiguous in Pando and the two
readings have different answers:

| | | Screened? |
|---|---|---|
| The **draft spec** | what detection proposes and a person reviews (§01) | **Yes** |
| The **bundle plan** | `api.BundlePlan`, what the planner hands a runtime adapter (§03 2.1) | No |

The spec is the sole record of how an app runs (R-020) and is the thing a person reviews before
anything is pinned. The bundle plan is derived from it at deploy time, in adapter vocabulary, with
secrets already materialized into `WorkloadPlan.Env` — amending it would put a model's output inside
the plan boundary §07 B draws, after the last point at which a person saw anything, and would mean the
running app no longer matched its spec. The spec is what a person reviews and what export and
rollback work from, so it is the thing screening amends.

**[D] Screening is applied to the assembled spec, after the install's defaults.** A screener that sees
`Routing.Mode: path` can say something useful about an app that writes absolute asset paths; one that
sees a bare draft cannot. The defaults are applied in `core/detection.Runner` before the proposal is
shown (§07 A step 12), and screening runs after them so it sees the same spec the person reviewing
will see.

---

## 2. The interface

Package `internal/adapter/api`, alongside the other nine.

```go
type AIAdapter interface {
    Adapter
    Capabilities(ctx context.Context) (AICapabilities, error)
    ScreenPlan(ctx context.Context, req ScreenRequest) (ScreenResult, error)
}

type AICapabilities struct {
    Functions []AIFunction // screen_plan, and R-106's other two when they exist
    Model     string       // shown in the review: which model read this code
    MaxFiles  int
    MaxBytes  int64
}
```

**[D]** `Functions` is data, not a type assertion (R-254, R-259). An adapter that does not screen is
skipped with a reason in the proposal rather than by a failed assertion nobody can plan around.

**[D]** `Model` is on the capabilities rather than only in the adapter's config because the review
shows it, for example "Anthropic (`claude-opus-5-5`) read 7 files and changed 3 things". Core cannot show
a value it never receives.

```go
type ScreenRequest struct {
    Source    SourceView     // read-only, structurally (R-020)
    Spec      spec.AppSpec   // the proposal, complete, with install defaults applied
    Evidence  []string       // why the winning detector bid what it did
    Questions []Question     // what detection could not work out (R-102)
    Trial     TrialSummary   // what the trial run saw, including the log
    Budget    ScreenBudget
}

type TrialSummary struct {
    Ran            bool
    Crashed        bool
    ObservedPorts  []int
    ObservedWrites []string
    Log            string
}

type ScreenBudget struct {
    MaxFiles int
    MaxBytes int64
    Timeout  time.Duration
}
```

**[D]** The adapter gets the `SourceView`, not a digest core assembled. The whole value of screening is
in the files Pando's detectors do not read — a `Procfile` beside a `fly.toml`, a `config/database.yml`
that never says `DATABASE_URL`, a README that names the one environment variable without which the app
exits. Core cannot pre-select those without already knowing what it is looking for, which is the
problem. `SourceView` has `Open`, `Stat` and `Glob` and no write method, structurally, so handing it
over grants reading and nothing else.

**[D] `Trial` is passed in whole, including the log.** R-107 is explicit that a crashed trial run shown
to a person is the correct outcome, not a gap to close with inference. That stays true — screening does
not rescue R-107's repository, because nothing in it says it needs Postgres. But the log is the single
most informative thing Pando has about an app that did not start, and withholding it from the one
component positioned to read it would be withholding it for no reason. What the log may *produce* is
bounded by §3, not by keeping it secret.

```go
type ScreenResult struct {
    Amendments []Amendment
    Notes      []string   // things worth saying that change nothing
    FilesRead  []string   // what left the host. Audited (R-337)
    Model      string
}
```

**[D]** `FilesRead` is reported by the adapter and recorded in the audit event. It is not a security
boundary, since an adapter that misreported it would already have read the file. It is the operator's
record of what was sent to the provider, which R-337 requires.

---

## 3. Amendments: the closed set

**[D] This is the load-bearing decision of the document** (R-332).

The obvious interface is `ScreenPlan` returning a `spec.AppSpec` — the amended proposal, whole. It is
wrong, and the reason is not that a model might be careless. It is that a returned spec can express
every field in the spec, including `Runtime.IsolationFloor`, `Build.EgressMode`, `Routing.AdapterRef`
and `Resources`. Those are the install's answers, not the repository's (R-104, R-272); an isolation
floor in particular is a policy floor that host policy sets and app configuration may only move within.
A whole-spec return makes "a model lowered an install's isolation floor and a person clicked accept" a
thing the type system permits, and leaves a denylist in core as the only thing standing between it and
happening. A denylist has to be extended by hand every time the spec gains a field.

So a screener does not return a spec. It returns amendments drawn from a fixed list, and no amendment
in the list changes any of those fields.

| Kind | Changes | Refused when |
|---|---|---|
| `set_command` | a workload's command | the command is empty, or the workload does not exist |
| `set_env` | one literal environment entry | the key is a declared slot, or the existing entry resolves from a slot or a secret |
| `set_port` | the primary port | a port on that workload was observed (R-333) |
| `set_health` | health path and port | the path is not absolute |
| `add_slot` | a declared dependency Pando missed | the key already exists |
| `set_build_context` | the build context directory | the path is absent from the source, or escapes it |
| `set_dockerfile` | which Dockerfile builds | the path is absent from the source, or escapes it |
| `set_static_dir` | which directory is served | the path is absent from the source, or escapes it |
| `add_volume` | persistent storage at a path | the path is not absolute, or is already mounted |
| `answer_question` | one of detection's questions | no outstanding question has that key |
| `add_warning` | says something, changes nothing | — |

**[D] Every amendment requires evidence** (R-334): at least one path in the repository it rests on,
and the paths are checked against the source. A model that names a file that is not there has its
amendment refused with that as the reason. This check catches amendments based on what a framework
usually does rather than on what this repository contains, and it costs one `Stat` per path.

**[D] Refusals are recorded.** A refused amendment appears in the proposal with its reason, so the
person reviewing can see it and whoever maintains the prompt can see which rules the model is
breaking.

**[D] `add_warning` writes one code, `WARN_SCREENING_ADVISORY`.** Not the screener's choice of code.
The codes in §01 2.8 mean specific things that specific parts of Pando produced — a screener able to
emit `WARN_COMPOSE_CONSTRUCT_REWRITTEN` can claim the compose importer rewrote something it did not.

### 3.1 An observation outranks a screening

**[D]** R-333, and it is the rule most likely to be argued with. The trial run watched the process bind
3000 and wrote `Port.Source: observed`. A screener that has read the framework's documentation and
believes it serves on 8080 is refused.

The rule runs one way only. Where nothing was observed, a screened port is better than a question.
Where something was observed, the observation is kept, because R-097 prefers watching the process to
inferring from its source, and a model's reading of the source is an inference.

The same rule covers writes: a screener may add a volume at a path the trial run never reported, and
may not remove one it did.

### 3.2 Required slots

**[D] A slot a screener adds is not `Required` unless the trial run crashed.**

R-132 blocks a deploy on an unfilled required slot, so a screener that marks slots required too freely
converts "the app might not start" into "the app cannot be deployed" — which is worse, and worse in the
way R-104 warns about, because it is a blocker where configuration would do.

The rule is not invented for this. O-4's `[P]` fallback in §01 2.5 already says exactly this: default
`Required: false` for anything not typed to a known service, and let the trial run settle it — a slot
whose absence crashes the trial run is promoted to required with the crash log as evidence. Screening
adopts it unchanged. A screener that finds `DATABASE_URL` in `config/database.yml` on a repository
whose trial run came up clean adds an optional slot, visible in the review, fillable in one click. One
that finds it on a repository whose trial run died on a connection refused adds a required one.

---

## 4. Where it runs

```
core/detection.Runner.Detect
  ├─ policy: source allowlist (R-092)
  ├─ source.Fetch                     → checkout
  ├─ detect.Job.Run                   → proposal   ← deterministic, unchanged
  ├─ applyDefaults                    → the install's answers
  ├─ screening.Run                    → amendments, applied and refused   ← this document
  ├─ audit: detection.screen (R-337)
  └─ Detections.Save
```

**[D] In `core/detection`, not in `internal/detect`.** `detect` is the auction and the trial run, and
keeping it free of this preserves the property §1 argued for: the auction is a pure function of the
source, testable without a network and without a model. It is also where the audit event has to be
written, because R-027 forbids an adapter writing one, and where host policy is already read.

**[D] The enforcement lives in `internal/core/screening`, not in the adapter and not in the handler.**
The amendment types are in `adapter/api` because an adapter returns them; deciding which ones are
allowed to land is core's, for the same reason authorization is (R-027). An adapter cannot amend a spec
— it can only propose an amendment to one, and `screening.Apply` is the only thing in the tree that
turns the second into the first.

### 4.1 Failure is the deterministic proposal

**[D]** R-335. Every one of these leaves the proposal untouched and records why in `Outcome.Skipped`:

- no AI adapter configured
- the adapter does not advertise `screen_plan`
- host policy forbids screening
- the adapter's `HealthCheck` fails, or `ScreenPlan` returns an error
- the budget's timeout expires
- the result parses to nothing

None of them is an error returned from `Detect`. Everything the auction produced is still in the
proposal, including any questions, so failing the detection would discard a usable result because an
optional step did not run.

---

## 5. Review and provenance

**[D] Amendments are applied, not queued for per-item approval.** Holding each one for a click would
add a question per amendment, which R-103 counts against the product. The proposal a person reviews is
the amended one, and it passes the same review gate as any other proposal (R-331, R-098).

**[D] The spec records screened provenance**, alongside the sources it already records:

```go
spec.EnvFromScreening  EnvSource    = "screened"
spec.PortScreened      PortSource   = "screened"
spec.VolumeFromScreening VolumeSource = "screened"
```

Three properties follow from reusing those fields rather than adding a flag. The review UI already
renders provenance, so "we watched your app bind 3000" and "Anthropic read your Dockerfile and set
NODE_ENV" are shown by the same code. `spec.Carry` (§01, re-detection) already keeps only `user`
sources, so a screened value is replaced by the next screening rather than carried forward as though a
person had chosen it — which is right: re-detection re-screens. And R-333's refusal is a comparison
between two values of a field the spec already has.

**[D] The outcome is recorded on the proposal**, not only in the audit log:

```go
type Outcome struct {
    Ran        bool
    Skipped    string            // why nothing ran (§4.1); empty when it did
    SkipCode   SkipCode          // the same, as a stable value: not_configured | policy | unsupported | unavailable | blocked
    AdapterRef string
    Model      string
    FilesRead  []string
    Applied    []Applied
    Refused    []Refused
    Answers    map[string]string // detection questions it answered (R-338)
    Notes      []string
    DurationMS int64
}
```

The console renders it as a section of the review, directly under the winning bid: the model, each
change with its reason and the files it cites, the files read, and what it asked for that Pando would
not do. An answered question is listed among the changes with its reason, so the review shows why the
question stopped being asked. When `skip_code` is `not_configured` the section is absent, because an
install without an AI adapter is not degraded; any other skip is shown as one line with its reason.

---

## 6. The Anthropic adapter

`internal/adapter/ai/anthropic`, kind `anthropic`. The first implementation, and the one the interface
was shaped against.

**[P] The official Go SDK**, `github.com/anthropics/anthropic-sdk-go`. Hand-rolling the HTTP would
avoid a dependency and would mean owning the request shape, the retry policy, the streaming envelope
and the error taxonomy for a provider whose API is not ours to keep up with.

**[P] `claude-opus-5-5` is the default model**, overridable per install. Screening runs once per
detection, on a repository somebody is about to deploy, and the thing being optimized is whether the
app comes up on the first try — this is not a high-volume path where a cheaper model pays for itself.
It replaced `claude-opus-5` as the default because it is newer and costs less per token. An install
that disagrees sets `model` in the adapter's config.

**[P] The model reads the repository through two tools**, `list_files` and `read_file`, backed by the
`SourceView` and counted against `ScreenBudget`. The alternative — core packing a bundle of files into
the prompt — has to guess which files matter, which is the thing screening exists to stop guessing at.
Both tools refuse a path that escapes the checkout and stop returning content once the budget is spent.

**[P] Amendments come back through a `submit_findings` tool with `strict: true`** rather than as prose
to be parsed. The schema is the closed set from §3, so a malformed amendment is rejected by the API
before it reaches Pando, and the shape core validates is the shape the model was given.

**[D] The API key is a `secret.Value`** (§00 3.3), so it renders `[redacted]` in every marshaler and
cannot reach a log line (R-194).

**[D] The adapter imports nothing from core but `spec` and `secret`,** like every other adapter. The
depguard rule in `.golangci.yml` covers it without a new entry, because it matches `**/internal/adapter/**`.

---

## 7. Configuration

An `adapter_configs` row like any other (§02 2.5), seeded **disabled and absent**: unlike the eight
categories `seedDefaultAdapters` fills, there is no AI adapter that works without a credential, and
seeding a broken one would put a permanently unhealthy adapter in every install's console.

```json
{
  "model": "claude-opus-5-5",
  "screen_plans": true,
  "max_files": 40,
  "max_bytes": 262144,
  "timeout_seconds": 120
}
```

Configured through `POST /api/v1/adapters` like any other category, with `category: "ai"`,
`kind: "anthropic"`, the settings above as `config`, and the key as `credentials: {"api_key": "…"}`.
It takes effect at the next restart, because adapters are registered at startup (R-253).

**[D] The API key is never stored in the clear (O-20).** `credentials` is write-only and is sealed by
the install's secrets adapter into `adapter_credentials`, bound to the adapter's ID. The database
refuses a `credentials` key in `adapter_configs.config`, the handler refuses `api_key` and similar
names there, and the adapter refuses to start if it finds one at the top level of its stored
configuration. Core decrypts the credential at startup and passes it to `Configure` in memory.
`GET /adapters` reports `credentials_set: ["api_key"]` and nothing more. An operator who prefers the
environment can instead set `api_key_env`, or `ANTHROPIC_API_KEY`, which the adapter reads when no
credential is stored.

**[P] `screen_plans` defaults to true** (R-336). Configuring the adapter required a credential; that
was the decision.

**[P] Host policy carries `DisableAIScreening`**, install-wide, evaluated in `core/detection` beside
the source allowlist. It can only deny, like everything else in the document (R-272).

---

## 8. What this is not

**[D] It does not remediate the app** (R-028). It amends Pando's description of how to run the
repository. It does not write to the repository, open a pull request, patch a Dockerfile, or inject
configuration into a running workload. `SourceView` has no write method.

**[D] It does not invent topology** (R-021). Every amendment rests on a path in the repository, checked
to exist. Finding a `DATABASE_URL` in a file Pando's detectors do not read is finding a declaration;
deciding an app needs Postgres because it imports an ORM is not, and nothing in §3's list can express
it — `add_slot` needs a key, and a key comes from somewhere in the source.

**[D] It is not required.** Everything the auction produces is produced whether or not a screener
runs (R-106). §4.1 lists the cases in which screening is skipped.
