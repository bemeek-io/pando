# 01 — The App Spec

The spec is the center of the system. Detection produces it, humans edit it, the planner consumes it, adapters translate it, revisions store it, export serializes it. Everything else is downstream of getting this right.

---

## 1. Properties

**[D]** **The spec is the sole record of how an app runs.** Nothing is read from the repo at deploy time (R-020). If it isn't in the spec, it doesn't happen.

**[D]** **Specs are immutable and versioned.** Editing produces a new revision. The running app points at a pinned revision. Rollback (R-152) is repointing.

**[D]** **The spec contains no policy and no grants.** Policy is evaluated live at plan time so that R-274 works; grants live separately so sharing doesn't create a revision.

**[D]** **The spec contains no secret values** — only slot declarations and references. Export (R-020) is therefore safe to hand to someone.

**[D]** **The spec names adapters by reference, not by vocabulary** (R-251). It says `runtime: {ref: "rt_docker_local"}`, never Docker arguments.

**[P]** Wire format is JSON. Stored as `jsonb`. Versioned with a `schema_version` so migrations across Pando releases are mechanical.

---

## 2. Schema

```go
package spec

type AppSpec struct {
    SchemaVersion int    `json:"schema_version"` // currently 1
    AppID         string `json:"app_id"`
    Revision      int    `json:"revision"`       // monotonic per app
    CreatedAt     time.Time `json:"created_at"`
    CreatedBy     string    `json:"created_by"`   // principal ID
    Origin        Origin    `json:"origin"`       // how this revision came to be

    Source     Source      `json:"source"`
    Build      Build       `json:"build"`
    Workloads  []Workload  `json:"workloads"`
    Volumes    []Volume    `json:"volumes"`
    Slots      []Slot      `json:"slots"`
    Routing    Routing     `json:"routing"`
    Runtime    RuntimeRef  `json:"runtime"`
    Deploy     Deploy      `json:"deploy"`
    Health     Health      `json:"health"`
    Resources  Resources   `json:"resources"`
    Egress     Egress      `json:"egress"`
    Retention  Retention   `json:"retention"`
    PerUser    PerUser     `json:"per_user"`      // R-290, LATER
    Warnings   []Warning   `json:"warnings"`      // carried, not resolved
}

type Origin string
const (
    OriginDetected  Origin = "detected"   // produced by the auction
    OriginEdited    Origin = "edited"     // human modified a prior revision
    OriginRedetect  Origin = "redetected" // explicit re-detection, R-022
    OriginImported  Origin = "imported"   // from an export or DR restore
    OriginManual    Origin = "manual"     // escape hatch, R-101
)
```

### 2.1 Source

```go
type Source struct {
    Type   SourceType `json:"type"`   // git | image | upload
    URL    string     `json:"url,omitempty"`
    Ref    string     `json:"ref,omitempty"`     // branch or tag as requested
    Commit string     `json:"commit,omitempty"`  // resolved SHA. R-120: this is what deploys.
    Subdir string     `json:"subdir,omitempty"`  // monorepo entrypoint
    Image  string     `json:"image,omitempty"`   // when Type == image
    Digest string     `json:"digest,omitempty"`  // resolved, pinned
    CredentialRef string `json:"credential_ref,omitempty"` // LATER, R-091. App-owned; see below.
}
```

**[D]** `Ref` is what the user asked for; `Commit` is what runs. Auto-deploy (R-141) advances `Commit` and creates a revision. A deploy never resolves `Ref` implicitly at runtime.

**[D] Resolved (O-3): source credentials are app-owned and attributed.** The question was whether a
private-repo credential belongs to the app or to the person who supplied it. User-owned is the
intuitive answer and the wrong one: it means an app stops deploying when its author leaves, and it
fails at the worst time — during an offboarding, when nobody remembers which apps depended on that
person. App-owned means an app keeps working.

The cost of app-owned is losing track of whose credential it is, so it is stored as an ordinary
app-scoped secret **with the supplying principal recorded in the audit event**, not in the spec. On
that user's deletion, §21's destruction rules surface every app holding a credential they supplied and
flag it for rotation. The credential keeps working — this is a prompt, not a revocation, because
breaking deploys is the failure mode being avoided.

### 2.2 Build

```go
type Build struct {
    Strategy   BuildStrategy `json:"strategy"`
    // dockerfile | compose | buildpack | static | prebuilt

    AdapterRef string   `json:"adapter_ref"`
    Dockerfile string   `json:"dockerfile,omitempty"`
    Context    string   `json:"context,omitempty"`
    Target     string   `json:"target,omitempty"`
    Args       []KV     `json:"args,omitempty"`       // build args, never secrets
    ComposeFile string  `json:"compose_file,omitempty"`
    StaticDir  string   `json:"static_dir,omitempty"`

    IsolationFloor IsolationClass `json:"isolation_floor"` // R-114, independent of runtime
    TimeoutSeconds int            `json:"timeout_seconds"` // R-119
    EgressMode     EgressMode     `json:"egress_mode"`     // R-118
    EgressAllow    []string       `json:"egress_allow,omitempty"`
}
```

**[D]** `IsolationFloor` here is deliberately separate from `Runtime.IsolationFloor`. R-114.

**[D]** Build args are not secrets and are stored in the clear. Anything sensitive is a slot.

### 2.3 Workloads

```go
type Workload struct {
    Name      string   `json:"name"`
    Image     string   `json:"image,omitempty"`     // set post-build, or from compose
    Command   []string `json:"command,omitempty"`
    Entrypoint []string `json:"entrypoint,omitempty"`
    WorkingDir string  `json:"working_dir,omitempty"`

    Env       []EnvEntry `json:"env"`
    Ports     []Port     `json:"ports"`
    Mounts    []Mount    `json:"mounts"`
    Files     []File     `json:"files,omitempty"`   // configuration carried in the spec, R-099a
    DependsOn []string   `json:"depends_on,omitempty"`  // from compose, R-096
    Healthcheck *Healthcheck `json:"healthcheck,omitempty"`

    Exposed   bool       `json:"exposed"`   // R-026. Exactly one primary, see below.
    Primary   bool       `json:"primary"`   // the one the app's URL resolves to
    Resources *ResourceLimits `json:"resources,omitempty"` // nil = inherit app-level
}

type EnvEntry struct {
    Key       string  `json:"key"`
    Value     *string `json:"value,omitempty"`      // literal, non-sensitive
    SlotRef   *string `json:"slot_ref,omitempty"`   // resolved at deploy from a slot
    SecretRef *string `json:"secret_ref,omitempty"` // resolved at deploy from secrets
}

type Port struct {
    Number   int    `json:"number"`
    Protocol string `json:"protocol"` // http | tcp
    Source   string `json:"source"`   // observed | expose | compose | framework | user
}
```

**[D]** Exactly one workload has `Primary: true`. Others may be `Exposed` (reachable through the proxy at a sub-route) but the app has one canonical endpoint (R-026 and the bundle model).

**[D]** `Port.Source` is retained because it feeds the review UI. "We watched your app bind 3000" reads differently from "we guessed 3000 because it's a Next.js app," and the user reviewing the proposal deserves to know which.

**[D]** An `EnvEntry` has exactly one of `Value`, `SlotRef`, `SecretRef`. Validation enforces it.

**[D]** `Files` carries a configuration file the app cannot start without (R-099a):

```go
type File struct {
    Path    string `json:"path"`              // absolute, inside the container
    Content string `json:"content"`           // text, <= 64 KB
    Mode    int    `json:"mode,omitempty"`    // default 0644
}
```

A compose service that bind-mounts `./Caddyfile:/etc/caddy/Caddyfile` produces one. Neither of the
two mechanisms Pando has for a path fits it: storage is a directory, and Docker refuses to mount a
directory over a file that exists in the image, while a host bind mount would mean reading the
repository at deploy time. So the bytes are read once at detection and stored here — the same
reasoning as `Build.GeneratedFiles`, one layer along, and for the same requirement (R-020). The spec
stays the sole record of how the app runs, which also means the file is a **snapshot**: editing it in
the repository changes nothing until the app is detected again, and the import warning says so.

The runtime places it between create and start, which is the only moment available — the container's
filesystem exists and nothing has read it. Directories above it are created; a runtime that cannot do
any of this reports `SupportsCarriedFiles: false` and the planner refuses before anything is created
(R-254).

Not a way to ship an application. Text only, capped at 64 KB, and a larger or binary file is refused
at import with that as the reason: a file that size is a build input, and builds have their own path.

### 2.4 Volumes

```go
type Volume struct {
    ID       string `json:"id"`        // vol_...
    Name     string `json:"name"`
    Declared VolumeSource `json:"declared"` // compose | user | detected-warning
    SizeHint int64  `json:"size_hint_bytes,omitempty"`
}

type Mount struct {
    VolumeID string `json:"volume_id"`
    Path     string `json:"path"`
    ReadOnly bool   `json:"read_only"`
}
```

**[D]** Volumes are top-level and referenced by mounts, not nested in workloads, because a volume outlives any single workload definition and must survive a revision that renames the workload.

**[D]** `Declared` distinguishes a compose-declared volume from one the user added after the R-201 warning. Feeds the review UI and the delete prompt.

### 2.5 Slots

```go
type Slot struct {
    Key        string      `json:"key"`         // e.g. REDIS_URL
    Type       SlotType    `json:"type"`        // postgres|mysql|redis|s3|smtp|unknown
    Required   bool        `json:"required"`    // O-4 — see below
    Evidence   []string    `json:"evidence"`    // why we think this is a slot
    Resolution *Resolution `json:"resolution"`  // nil = unfilled, blocks deploy (R-132)
}

type Resolution struct {
    Mode      ResolutionMode `json:"mode"` // provisioned | bound | literal
    ServiceRef string        `json:"service_ref,omitempty"` // provisioned instance
    Target     string        `json:"target,omitempty"`      // bound: an external URL or host service
    SecretRef  string        `json:"secret_ref,omitempty"`  // literal values are stored as secrets
}
```

**[D]** A literal slot value is stored as a secret, not inline. Keeps R-020's "export is safe" property true without a special case.

**[O-4]** `Required` has no reliable derivation. Candidate heuristics for the implementer to evaluate, none authoritative:
- present in `.env.example` **without** a default value → likely required
- referenced in compose `environment` without a default → required
- matches a known service type (`*_URL`, `*_DSN`, `DATABASE_*`) → likely required
- has a plausible default in the sample (`LOG_LEVEL=info`) → optional

**[P] Fallback that preserves R-103:** default `Required: false` for anything not typed to a known service, and let the trial run (R-097) settle it. A slot that isn't filled and whose absence crashes the trial run gets promoted to required with the crash log as evidence. This turns an unanswerable question into an observation, consistent with how port discovery works.

### 2.6 Routing, runtime, deploy

```go
type Routing struct {
    AdapterRef string      `json:"adapter_ref"`
    Mode       RoutingMode `json:"mode"`      // subdomain | path | port
    ModeSource string      `json:"mode_source"` // adapter_default | user_override (R-163)
    Hostname   string      `json:"hostname,omitempty"`
    PathPrefix string      `json:"path_prefix,omitempty"`
    Port       int         `json:"port,omitempty"`
}

type RuntimeRef struct {
    AdapterRef     string         `json:"adapter_ref"`
    IsolationFloor IsolationClass `json:"isolation_floor"`
}

type Deploy struct {
    Strategy   DeployStrategy `json:"strategy"`    // recreate (default, R-144) | start_then_swap
    AutoDeploy AutoDeploy     `json:"auto_deploy"`
    AutoRollback bool         `json:"auto_rollback"` // default false, R-147
}

type AutoDeploy struct {
    Enabled bool             `json:"enabled"`  // default false, R-141
    Trigger AutoDeployTrigger `json:"trigger"` // branch_updated | release_tagged
    Branch  string           `json:"branch,omitempty"`
}
```

**[D]** `ModeSource` is retained so the console can show whether a routing mode was inherited or deliberately chosen — relevant when host policy later restricts overrides (R-274 / O-10).

### 2.7 Health, resources, egress, retention

```go
type Health struct {
    Source   HealthSource `json:"source"` // compose | http | tcp | process (R-221)
    Path     string       `json:"path,omitempty"`
    Port     int          `json:"port,omitempty"`
    IntervalSeconds int   `json:"interval_seconds"`
    TimeoutSeconds  int   `json:"timeout_seconds"`
    Retries         int   `json:"retries"`
}

type Resources struct {
    CPUMillis   int   `json:"cpu_millis"`    // inherited from host default, R-240
    MemoryBytes int64 `json:"memory_bytes"`
    DiskBytes   int64 `json:"disk_bytes"`
    Overridden  bool  `json:"overridden"`    // required app.resources.override (R-241)
}

type Egress struct {
    Mode      EgressMode `json:"mode"`       // inherit | allowlist (R-182)
    Allowlist []string   `json:"allowlist,omitempty"`
}

type Retention struct {
    LogBytes         int64 `json:"log_bytes"`          // R-223
    BackupDaily      int   `json:"backup_daily_count"` // R-211
    SpecRevisions    int   `json:"spec_revisions"`     // R-152
}
```

**[D]** `Egress.Mode == allowlist` **replaces** the install-wide list rather than intersecting (R-182). The field name and its documentation must say so, because "allowlist" reads like narrowing and it is not.

### 2.8 Warnings

```go
type Warning struct {
    Code       string `json:"code"`
    Message    string `json:"message"`
    Dismissed  bool   `json:"dismissed"`
    DismissedBy string `json:"dismissed_by,omitempty"`
}
```

**[D]** Warnings live in the spec and survive revisions until dismissed. Codes that must exist:

- `WARN_NO_PERSISTENT_VOLUME` (R-201) — details name the directory the trial run observed being written, when available (R-202)
- `WARN_PATH_ROUTING_INCOMPATIBLE` (R-168)
- `WARN_COMPOSE_CONSTRUCT_REWRITTEN` (R-099)
- `WARN_UNDECLARED_DEPENDENCY_SUSPECTED` — informational only; Pando still does not act (R-021, R-107)

**[D]** A warning is never a blocker. Blockers are `PLAN_*` errors (§00 3.2).

---

## 3. Validation

Run on every spec before it is pinned, and again at plan time.

| Rule | Error |
|---|---|
| Exactly one workload with `Primary: true` | `VALID_PRIMARY_WORKLOAD` |
| Every `Mount.VolumeID` resolves to a declared volume | `VALID_DANGLING_MOUNT` |
| Every `EnvEntry` has exactly one of value/slot/secret | `VALID_ENV_AMBIGUOUS` |
| Every `SlotRef` resolves to a declared slot | `VALID_DANGLING_SLOT_REF` |
| `DependsOn` is acyclic | `VALID_DEPENDENCY_CYCLE` |
| Adapter refs resolve to configured adapters | `PLAN_ADAPTER_NOT_CONFIGURED` |
| Routing mode is advertised by the routing adapter | `PLAN_CAPABILITY_UNSUPPORTED` |
| No workload requests a rejected compose construct | `PLAN_COMPOSE_CONSTRUCT_REJECTED` |
| Every `Required` slot has a `Resolution` | `PLAN_SLOT_UNFILLED` |

---

## 4. Diffing

**[D]** R-022 and R-100 both promise a diff. R-152 needs one for rollback review.

Diff operates on the spec tree and classifies each change:

| Class | Meaning | UI treatment |
|---|---|---|
| `benign` | Retention, warnings, display-only | Collapsed |
| `restart` | Env, resources, health | Shown; requires restart |
| `rebuild` | Source, build config | Shown; triggers a build |
| `destructive` | Volume removed, slot resolution changed from provisioned to bound, routing mode changed, **runtime adapter changed** | **Shown prominently, requires explicit confirmation** |

**[D]** Re-detection (R-022) presents this diff. So does promoting a compose service to Pando-managed (R-100). Same machinery.

**[D] Resolved (O-8): changing the runtime adapter is a destructive spec change, not a migration.**
The question was whether swapping an app's runtime (Docker → Incus) migrates state or forces a
redeploy. Neither, as posed: it is classified `destructive` in the table above, requiring explicit
confirmation, and volumes go through the existing keep-or-discard flow (R-204) rather than a bespoke
transfer path.

Building a migration would mean moving volume contents between adapters that have no common
representation of a volume — Pando cannot know what is inside one (R-206) — and would put Pando in the
business of relocating running workloads, which is one step from the scheduling R-010 forbids. The
honest operation is: confirm, keep the volumes, redeploy onto the new runtime, reattach. The existing
machinery already does every part of that.

---

## 5. Export and import

**[D]** `pando export <app>` emits the spec plus its resolved non-secret configuration. Secrets are named but not valued.

**[D]** Import produces `OriginImported` and lands as a proposal requiring review, never a live deployment. An import is untrusted input like any other.

**[D]** The DR bundle (R-212) is not this. Export is per-app and human-readable; the DR bundle is whole-install and encrypted (R-213).
