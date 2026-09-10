# 03 — Adapter Interfaces

Package `internal/adapter/api`. Definitions only, no implementations.

R-250: the app declares requirements; adapters translate. R-251: core never learns a provider's vocabulary. These interfaces are where that promise is either kept or broken — if a Docker-shaped concept appears in a signature below, the design has failed.

R-253: everything is compiled in-tree. These are Go interfaces, freely refactorable. There is no wire protocol and none is planned.

---

## 1. Common types

```go
package api

type Category string

const (
    CategoryIdentity Category = "identity"
    CategoryRouting  Category = "routing"
    CategoryBuilder  Category = "builder"
    CategoryRuntime  Category = "runtime"
    CategorySecrets  Category = "secrets"
    CategoryServices Category = "services"
    CategoryNotify   Category = "notify"
)

type IsolationClass int   // ordered, comparable — policy floors depend on this

const (
    IsolationContainer     IsolationClass = 10  // shared kernel
    IsolationSandboxed     IsolationClass = 20  // gVisor, Kata
    IsolationVM            IsolationClass = 30  // Firecracker, Incus
    IsolationDedicatedHost IsolationClass = 40
)

// Adapter is implemented by every adapter in every category.
type Adapter interface {
    Kind() string
    Category() Category
    Configure(ctx context.Context, raw json.RawMessage) error
    HealthCheck(ctx context.Context) error
}
```

**[D]** `IsolationClass` is an ordered integer, not a string enum, because R-114 and R-255 require a policy floor comparison. Gaps of 10 leave room to insert classes without a migration.

**[D]** Every adapter implements `HealthCheck`. The planner refuses to plan against an unhealthy adapter and returns `ADAPTER_UNAVAILABLE` rather than failing mid-deploy (R-254).

### 1.1 Capabilities as data

**[D]** R-254: capabilities are a returned struct, never a Go type assertion. Type assertions are invisible to the planner and cannot produce a readable plan-time error.

```go
type RuntimeCapabilities struct {
    IsolationClass          IsolationClass
    SupportsPersistentVolumes bool
    SupportsExec            bool
    SupportsMultipleWorkloads bool
    SupportsPrivateNetwork  bool   // required for R-026; an adapter without it is unusable
    SupportsResourceLimits  bool
    SupportsStartThenSwap   bool   // R-145
    MaxWorkloadsPerBundle   int    // 0 = unlimited
}

type RoutingCapabilities struct {
    Modes             []RoutingMode  // subdomain | path | port
    DefaultMode       RoutingMode    // R-162
    SupportsWildcardTLS bool
    SupportsTLS       bool
    RequiresPublicReachability bool  // false for loopback
}

type BuilderCapabilities struct {
    IsolationClass  IsolationClass  // R-114, independent of runtime
    Strategies      []BuildStrategy // dockerfile | compose | buildpack | static | prebuilt
    SupportsCache   bool
    SupportsEgressRestriction bool  // R-118
}
```

---

## 2. Runtime

The largest interface. Owns bundles, workloads, volumes, exec, logs, and capacity.

```go
type RuntimeAdapter interface {
    Adapter
    Capabilities(ctx context.Context) (RuntimeCapabilities, error)

    // Capacity is adapter-reported, never host-inspected (R-243).
    Capacity(ctx context.Context) (Capacity, error)

    // Apply converges the named bundle toward the plan. Idempotent:
    // calling it with an already-satisfied plan must be a no-op.
    Apply(ctx context.Context, p BundlePlan) (BundleHandle, error)

    // Observe reports what actually exists. Drives reconciliation (R-148).
    Observe(ctx context.Context, ref BundleRef) (ObservedBundle, error)

    Stop(ctx context.Context, ref BundleRef) error
    Destroy(ctx context.Context, ref BundleRef, opts DestroyOptions) error

    CreateVolume(ctx context.Context, req VolumeRequest) (VolumeHandle, error)
    DestroyVolume(ctx context.Context, h VolumeHandle) error
    SnapshotVolume(ctx context.Context, h VolumeHandle, dst io.Writer) error
    RestoreVolume(ctx context.Context, h VolumeHandle, src io.Reader) error

    Logs(ctx context.Context, ref WorkloadRef, opts LogOptions) (io.ReadCloser, error)
    Exec(ctx context.Context, ref WorkloadRef, req ExecRequest) (ExecSession, error)
}
```

### 2.1 The plan

```go
type BundlePlan struct {
    BundleID   string          // stable across deploys of one app
    Workloads  []WorkloadPlan
    Volumes    []VolumePlan
    Network    NetworkPlan
    Labels     map[string]string
}

type WorkloadPlan struct {
    Name       string
    Image      string
    Command    []string
    Entrypoint []string
    WorkingDir string
    Env        map[string]string  // fully resolved. Slots filled, secrets injected.
    Mounts     []MountPlan
    Ports      []PortPlan
    DependsOn  []string
    Health     *HealthPlan
    Resources  ResourcePlan
    Exposed    bool
}

type NetworkPlan struct {
    Private     bool       // always true (R-026)
    EgressMode  EgressMode // allow_all | block_private | allowlist
    EgressAllow []string
}
```

**[D]** `Env` arrives fully resolved. Adapters never see a slot, never talk to the secrets adapter, and never learn that a value was sensitive. That keeps R-027's identity/authz/secrets boundary intact and keeps the interface small.

**[D]** `NetworkPlan.Private` is always true. It is a field rather than an assumption so an adapter that cannot provide a private network fails loudly at capability check (`SupportsPrivateNetwork`) rather than silently placing workloads on a shared network.

### 2.2 Observation

```go
type ObservedBundle struct {
    Exists    bool
    Workloads []ObservedWorkload
    Volumes   []ObservedVolume
}

type ObservedWorkload struct {
    Name        string
    Present     bool
    Running     bool
    Healthy     *bool      // nil = no health signal available
    ImageDigest string
    StartedAt   time.Time
    ExitCode    *int
    RestartCount int
}
```

**[D]** `Observe` reports facts and never remediates. The reconciler (§05) decides what to do. An adapter that silently restarts things makes drift undetectable and breaks R-148's report path.

**[D]** `Healthy` is a pointer because "no health signal" and "unhealthy" are different states and must not collapse (R-221).

### 2.3 Exec

```go
type ExecRequest struct {
    Command []string
    TTY     bool
    Env     map[string]string
}

type ExecSession interface {
    io.ReadWriteCloser
    Resize(rows, cols uint16) error
    ExitCode() (int, bool)
}
```

**[D]** Core opens an exec session only after `app.exec` is checked and policy is consulted (R-084, R-085), and writes the audit event before the session opens (R-228). The adapter does not authorize.

### 2.4 Capacity

```go
type Capacity struct {
    TotalCPUMillis   int
    TotalMemoryBytes int64
    TotalDiskBytes   int64
    UsedCPUMillis    int
    UsedMemoryBytes  int64
    UsedDiskBytes    int64
    Reported         time.Time
}
```

**[D]** R-243. The local Docker adapter reports its own machine; a clustered adapter reports its cluster. Core does not read `/proc` and has no concept of a host.

---

## 3. Builder

```go
type BuilderAdapter interface {
    Adapter
    Capabilities(ctx context.Context) (BuilderCapabilities, error)

    // Bid inspects the source and returns a confidence score plus a draft
    // spec fragment. Part of the detector auction (R-093).
    Bid(ctx context.Context, src SourceView) (Bid, error)

    Build(ctx context.Context, req BuildRequest) (BuildResult, error)
}

type Bid struct {
    Confidence float64        // 0..1
    Strategy   BuildStrategy
    Draft      spec.Partial   // workloads, ports, volumes, slots it can infer
    Evidence   []string       // human-readable, shown in the review UI
    Questions  []Question     // things it cannot determine (R-102)
}

type Question struct {
    Key       string
    Prompt    string   // must satisfy R-105: self-contained and pasteable
    Why       string
    Kind      QuestionKind  // choice | text | port | path
    Options   []string
}
```

**[D]** `Question.Prompt` carries a hard content requirement from R-105: it must be answerable by a model that cannot see the repo. Enforce it in review — a prompt reading "Which port?" fails; "This app appears to be a Node.js service. Pando could not determine which port it serves HTTP on. Valid answer: a port number such as 3000." passes.

**[D]** `SourceView` is a read-only filesystem view (R-020). It exposes `Open`, `Stat`, `Glob`. It has no write methods, structurally.

```go
type BuildRequest struct {
    Source         SourceView
    Strategy       BuildStrategy
    Dockerfile     string
    Context        string
    Args           map[string]string
    IsolationFloor IsolationClass
    Timeout        time.Duration
    EgressMode     EgressMode
    EgressAllow    []string
    CacheNamespace string        // per-app (R-117)
    LogSink        io.Writer     // streamed to the console live
}

type BuildResult struct {
    ImageRef    string
    Digest      string
    ObservedPorts []int    // from the trial run (R-097)
    ObservedWrites []string // directories written outside declared volumes (R-202)
}
```

**[D]** `ObservedPorts` and `ObservedWrites` are the trial run's output. They are the mechanism behind R-097's "watch what it binds, don't ask" and R-202's improved persistence warning.

**[D]** A builder implementation must never receive or request a container runtime socket (R-112). This cannot be enforced by the type system; it is a review checklist item and an integration test that asserts the build environment has no socket mounted.

**[P]** R-116: where a runtime adapter can provision an isolated environment per app — Incus, a
Firecracker VM — building *inside that environment* is preferred, because build isolation then comes
free from the boundary that already exists rather than from a second mechanism layered beside it. This
is not v1 work: v1 ships a Docker runtime whose isolation class is `container`, so BuildKit supplies
the build boundary independently. The note is here so that whoever writes the first VM-class runtime
adapter considers it before building a parallel isolation path. `BuilderCapabilities.IsolationClass`
stays independent of the runtime's either way (R-114) — a builder that borrows the runtime's boundary
reports the class it actually gets.

---

## 4. Routing

```go
type RoutingAdapter interface {
    Adapter
    Capabilities(ctx context.Context) (RoutingCapabilities, error)

    // Ensure makes traffic for this app arrive at Pando's proxy.
    // It never routes to the workload (§00 1.3).
    Ensure(ctx context.Context, r RouteRequest) (RouteHandle, error)
    Remove(ctx context.Context, h RouteHandle) error
    Observe(ctx context.Context, h RouteHandle) (RouteState, error)
}

type RouteRequest struct {
    AppID      string
    Mode       RoutingMode
    Hostname   string
    PathPrefix string
    Port       int

    // Where the adapter must send traffic. Always Pando's proxy.
    ProxyUpstream string
    TLS           TLSRequest
}
```

**[D]** `ProxyUpstream` is in the request rather than discovered by the adapter, so the contract is explicit: an adapter is told where to point, and that destination is always the proxy. R-023 has no exceptions and this field is where that is made obvious to an adapter author.

**[D]** Path-mode adapters must strip the prefix and set `X-Forwarded-Prefix` (R-167). They must not rewrite response bodies (R-028).

### 4.1 Two topologies

R-164 and R-165 describe two ways an install can be reached. Both are supported; they are not adapter
choices so much as postures the routing configuration expresses.

**Proxy mode** — one hostname, one certificate, one thing to open on the firewall. Every app is
reached by logging into Pando first and following a path or a menu. **[D]** This is the recommended
enterprise topology, and the reason is procurement rather than engineering: one public hostname is far
easier to get approved than N of them. An install that cannot get a wildcard DNS record or a second
firewall rule can still run every app.

**Per-hostname** — each app has its own hostname; users bookmark URLs and carry a session. Pando is
invisible except at login. Better for apps that feel like products in their own right, and required
for anything where the URL is shared outside the organization.

**[D]** Neither is a global setting. The topology is the aggregate of each app's `Routing.Mode`, and an
install can mix them — an internal tool on a path and a customer-facing app on its own hostname, on
one Pando. What makes an install feel like one topology or the other is the routing adapter's
`DefaultMode` (R-162), which is why that field exists and why deviating from it is gated by
`app.routing.override`.

**[D]** The choice is not surfaced during setup (R-005, R-104). A user adding an app gets the
adapter's default; `ModeSource` records whether the mode was inherited or deliberately chosen, so the
console can later show which apps deviate and host policy can restrict overrides (R-274, O-10).

---

## 5. Identity

```go
type IdentityAdapter interface {
    Adapter

    // Begin returns where to send the user, or nil for adapters that
    // authenticate inline (local username/password).
    Begin(ctx context.Context, redirect string) (*Redirect, error)

    // Authenticate resolves an inbound callback or credential to a subject.
    Authenticate(ctx context.Context, c Credential) (Subject, error)

    SessionPolicy() SessionPolicy   // R-047: each adapter declares its own
    SupportsPush() bool             // SCIM or webhook (R-048)
}

type Subject struct {
    ExternalID  string   // stable within this adapter
    Email       string
    DisplayName string
    Groups      []string // external group identifiers
}

type SessionPolicy struct {
    MaxLifetime      time.Duration
    RevocationMode   RevocationMode // push | refresh | expiry_only
    RefreshInterval  time.Duration
}
```

**[D]** Identity adapters authenticate only (R-044). `Subject` carries no roles, no verbs, no permissions. Group *names* cross the boundary; what a group can *do* is Pando's (R-078).

**[D]** `SessionPolicy` is how R-047 is honored mechanically: each adapter declares its own lifetime and revocation mode, and the console can display the effective revocation window per adapter rather than implying a global guarantee.

---

## 6. Secrets

```go
type SecretsAdapter interface {
    Adapter
    Put(ctx context.Context, ref SecretRef, v secret.Value) (StoredRef, error)
    Get(ctx context.Context, ref StoredRef) (secret.Value, error)
    Delete(ctx context.Context, ref StoredRef) error
}
```

**[D]** `secret.Value` is the redacting wrapper from §00 3.3. Its `String()`, `MarshalJSON()`, and zap marshaler all return `[redacted]`. A secret cannot be accidentally logged because the type will not render.

**[D]** `Get` is called by core only during plan resolution, to populate `WorkloadPlan.Env`. It is never called on behalf of a user request except when `app.secrets.read` is held, and that path writes an audit event first.

---

## 7. Services

Fills provisioned slots (R-131).

```go
type ServicesAdapter interface {
    Adapter
    Supports() []SlotType
    Provision(ctx context.Context, req ProvisionRequest) (ProvisionResult, error)
    Destroy(ctx context.Context, h ServiceHandle) error
    Snapshot(ctx context.Context, h ServiceHandle, dst io.Writer) error
    Restore(ctx context.Context, h ServiceHandle, src io.Reader) error
}

type ProvisionResult struct {
    Handle      ServiceHandle
    ConnectionSecret secret.Value  // the URL/DSN, stored as a secret (R-131 literal note)
    Workloads   []WorkloadPlan     // injected into the bundle, never exposed (R-026)
}
```

**[D]** A provisioned service returns workloads that join the app's private bundle. It is not exposed, not addressable from outside, and not shareable with another app (R-134).

---

## 8. Notification

```go
type NotifyAdapter interface {
    Adapter
    Notify(ctx context.Context, n Notification) error
}

type Notification struct {
    Kind      NotificationKind // app_failed | deploy_failed | policy_violation | backup_failed
    AppID     string
    Recipients []Recipient
    Subject   string
    Body      string
}
```

**[D]** v1 ships console-only (R-231). The interface exists so SMTP and SendGrid (R-232) drop in without touching core.

---

## 9. Registration

```go
type Registry struct { /* ... */ }

func (r *Registry) Register(a Adapter)
func (r *Registry) Get(ref string) (Adapter, error)
func (r *Registry) ByCategory(c Category) []Adapter
func (r *Registry) Default(c Category) (Adapter, error)
```

**[D]** Registration happens in `main` at startup, from compiled-in packages (R-253). Configured instances come from `adapter_configs` (§02 2.5).

**[D]** CI enforces an import rule: nothing under `internal/adapter/` may import `internal/core/authz`, `internal/core/audit`, or `internal/core/state`. This is R-027 as a lint rule rather than a convention.

---

## 10. v1 implementations

| Category | Kind | Note |
|---|---|---|
| identity | `local` | username/password, argon2id |
| routing | `loopback` | port mode, no TLS, laptop default |
| routing | `traefik` | subdomain and path, TLS |
| builder | `buildkit` | rootless, containerized, no socket (R-111) |
| runtime | `docker` | container isolation class |
| secrets | `local` | encrypted at rest, key on disk (R-190) |
| services | `docker` | postgres, mysql, redis in-bundle |
| notify | `console` | R-231 |
