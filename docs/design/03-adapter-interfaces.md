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

**[D] Resolved (O-7): the command is recorded, the stream is not.** `ExecRequest.Command` goes into
the audit event at session open, along with the workload and the principal. The PTY stream is not
captured.

The argument for full capture is that `app.exec` is the highest-privilege action in the system and
R-086 already concedes the verb list is not a boundary against someone holding it. The arguments
against are decisive: a terminal stream contains secrets — the operator will `env`, will `psql`, will
paste a token — so capturing it builds a durable, searchable store of every secret in the install and
puts it in the audit log, which is the one table deliberately readable by anyone holding audit access.
It also cannot be redacted, because `secret.Value` protects values Pando handles and a stream is
bytes Pando never parses.

Recording the command preserves what the audit log is for — *who did what, when, on which app* — and
answers the question an investigation actually asks first. It does not pretend to answer what was
typed after the shell opened, and the documentation must say so rather than implying exec is fully
audited (R-086's standard).

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

**[D]** The address must be reachable **from wherever the adapter's data plane runs**, which is not
necessarily where Pando runs. With Traefik they are the same host; with an outbound-tunnel adapter the
tunnel daemon dials it from its own container. Surfaced by the Cloudflare sketch
(`notes-cloudflare-routing-sketch.md`) — a documentation change, not an interface one.

**[D]** `TLSRequest` is **advisory**. An adapter may satisfy it however it likes, or ignore it because
its edge already terminates TLS. It is an intent, not a set of instructions. This is O-5 resolving the
way the design assumed, and the tunnel sketch is the second adapter to want it.

**[D] The interface survived being sketched against a provider that works nothing like Traefik** — no
local config file, no listening port, no certificate on the host, configuration applied by remote API.
The reason it survived is that `Ensure`/`Remove`/`Observe` describe *intent* rather than mechanism.
Remember that when someone proposes adding a `Reload()` or a `ConfigPath` here: the abstraction holds
because it has neither.

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

### 4.2 Host ports in port mode

**[D] Resolved (O-15): the lowest free port in a configured range, held as a durable allocation.**
Default range `9000-9999`, one row per `(adapter_ref, port)`, exhaustion returning
`CAPACITY_NO_FREE_PORT` and naming the setting to widen.

A port is an **allocation**, not a derivation. The first version computed "the lowest number no pinned
spec is using", which is a guess about an allocation rather than one, and it raced in the ordinary
case rather than an exotic one: adding five apps at once runs five background detections, two compute
the same answer before either writes anything down, and both are handed 9001 with nothing noticing.
The unique constraint is what makes a collision impossible instead of unlikely.

Lowest-free rather than random, so an app tends to keep its port across a rebuild and a bookmark keeps
working. Reused rather than ever-increasing, so a deleted app's port comes back.

**A port is only an address if something listens on it.** The allocation was written into every
port-mode spec and shown to users for several phases before anything accepted a connection on it, so
a loopback install advertised an address that refused the connection and the apps were in fact
reachable only under Pando's path prefix. That is the worse half of the bug rather than the cosmetic
one: under a prefix an app that writes `/assets/app.js` into its own HTML comes up blank, and R-167
forbids rewriting the page to hide it — port mode is the answer to exactly that case. The proxy now
opens one listener per allocated port (`internal/proxy/ports.go`), reconciled against the allocations
rather than against running apps, so a stopped app answers "this app isn't running right now" at its
own address instead of refusing the connection. Requests on those listeners take the same path as
every other request: the port is one more way to resolve an app, not a second enforcement point
(R-023).

**[P] In a containerized install the published range and the allocation range are the same range.**
`docker-compose.yml` sets both from one pair of variables, defaulting to `9000-9019` rather than the
shipped `9000-9999`: Docker publishes a range by opening every port in it, and a thousand is minutes
of startup and a process per port. They have to move together — an app allocated a port outside the
published range gets an address nothing can reach, which is the bug above with extra steps.

**The revisit is closed by Traefik shipping.** O-15 was to be reconsidered at phase 10, on the
question of whether lowest-free and `9000-9999` were right. Phase 10 shipped a routing adapter that
does subdomain and path, so port mode is now the laptop default's path and not the only path —
`loopback` supports neither of the R-166 topologies (§4.1 above, and the table in §10), which is why
it needs a host port at all. An install that outgrows the range has a better answer available than a
wider range, and R-005 rules out the obvious alternative for the laptop case: someone who may not know
what a port is cannot pick a free one.

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
    Capabilities() ServicesCapabilities
    Supports() []SlotType
    Provision(ctx context.Context, req ProvisionRequest) (ProvisionResult, error)
    Destroy(ctx context.Context, h ServiceHandle) error
    Snapshot(ctx context.Context, h ServiceHandle, dst io.Writer) error
    Restore(ctx context.Context, h ServiceHandle, src io.Reader) error
}

type ServicesCapabilities struct {
    DataInAppVolumes bool  // the data is in volumes Pando already backs up
}

type ProvisionRequest struct {
    AppID, BundleID, SlotKey string
    Type      SlotType
    ServiceID string        // minted by core, so Provision is idempotent
    ExistingSecret secret.Value // the DSN a previous Provision returned, if any
}

type ProvisionResult struct {
    Handle      ServiceHandle
    ConnectionSecret secret.Value  // the URL/DSN, stored as a secret (R-131 literal note)
    Workloads   []WorkloadPlan     // injected into the bundle, never exposed (R-026)
    Volumes     []VolumePlan       // the workloads' storage, owned by the app
}
```

**[D]** A provisioned service returns workloads that join the app's private bundle. It is not exposed, not addressable from outside, and not shareable with another app (R-134).

**[P]** `ProvisionResult.Volumes` — added in phase 9. A database workload with nowhere to put its files
loses everything on the next deploy, and R-135 says a provisioned service's data follows the app's
volume rules, which it can only do if it is in a Pando volume. The volumes are the app's: recorded
after Apply, carried in the DR bundle, offered at delete, reclaimed once backed up. None of that
machinery knows services exist.

**[D] `Provision` must be pure and cheap.** It is called on every deploy, and again on every
reconcile — the reconciler's "what should be running" has to contain the provisioned service, or a
killed database is never restored (R-148) and the running one is reported every fifteen seconds as a
workload the spec does not declare. An implementation that talked to a provider here would be talking
to it four times a minute per app. The interface is shaped so it does not have to: `Provision` returns
plans, and the runtime adapter creates things. The reconciler drops `Env` from what comes back, so
building the comparison is never a reason to decrypt a secret (R-193).

**[P]** `ProvisionRequest.ServiceID` and `ExistingSecret` — added in phase 9 because **Provision runs on
every deploy**, not only the first: the workloads it returns *are* the service, so a redeploy that
skipped it would produce a bundle with no database in it and the runtime would converge to that.
Running every time means every run after the first must produce the same names and the same
credentials. A database sets its password when its data directory is created and ignores the variable
forever after, so an adapter generating a fresh one each call would hand the app a password the
database has never heard of — a deploy that succeeds and an app that cannot authenticate, with the
symptom nowhere near the cause.

**[P]** `Capabilities().DataInAppVolumes` — how the DR path knows whether to call `Snapshot` (R-212).
True for the in-bundle provisioner: its data is under `volumes/` in the bundle already, and calling
`Snapshot` would put the same bytes in twice. False for an adapter that provisions somewhere Pando can
only reach over the wire, where `Snapshot` is the only way the data arrives. The bundle manifest counts
both `services` and `services_snapshotted`, so "5 services, 0 snapshotted" is legible rather than
alarming. Data and not a type assertion, per R-254.

### 7.1 The in-bundle provisioner

**[P]** `internal/adapter/services/docker` fills `postgres`, `mysql` and `redis` and talks to no daemon.
It *plans* a service; the runtime adapter runs it. Two properties follow, and both are the point: the
service is reachable only on the app's private network because that is the only network its workloads
join and nothing publishes a port (R-134), and its data is an ordinary app volume (R-135).

**[P]** Images default to `postgres:17-alpine`, `mysql:8.4`, `redis:7-alpine`, overridable per install
through the adapter's config. Alpine variants because a hobbyist's host is the target and a 400MB
Postgres image on a two-core VPS is a cost with nothing to show for it.

**[P] The builder synthesizes a Dockerfile for strategies that have none.** `static` and `buildpack`
both end as a Dockerfile build, written outside the repository where possible and handed to BuildKit
as a separate filesystem from the context — so nothing is copied and the app's source is untouched.
The Dockerfile is Docker's vocabulary, so this belongs in the adapter and not in core (R-251): core
says "a directory of files to serve" or "a repository with no instructions", and a different builder
may answer either differently.

**[P] `buildpack` is nixpacks, invoked only to plan.** `nixpacks build --out` writes a Dockerfile and
builds nothing, so R-112 is not in play and the install needs no extra service. The generated
Dockerfile stays in the checkout because nixpacks' own output does `COPY . /app/.` alongside
`COPY .nixpacks/…` — the build context must be the source with the generated directory inside it.
Generation runs with no network and a minimal environment: it reads the repository and decides, and a
planner that can reach the internet while reading untrusted source is a wider boundary than this
needs. Paketo is the opt-in alternative and needs a registry in the install topology (R-095).

**[P] Every image Pando supplies itself is pinned by tag, not digest** — these three and the runtime
adapter's `busybox:stable` volume helper. This is a real weakness and worth naming rather than
leaving in a code comment: the bytes behind a tag can change, and the BusyBox one is pulled at
*restore* time, which means it can change between the backup and the disaster. A digest belongs in all
four places once there is a way to update them — a pinned digest with no update path is an image that
never gets a security fix, which is the failure this trades against.

**[P]** `s3` and `smtp` are slot types Pando recognises (R-130) and deliberately does not provision:
standing up MinIO or an SMTP server is running infrastructure, which R-010 says Pando is not. The
planner refuses a `provisioned` resolution for them by name, at plan time.

**[P]** `Destroy` is a no-op and `Snapshot`/`Restore` return an error rather than an empty archive. The
workloads go with the bundle and the volume follows R-204 and R-135 — it outlives the app on purpose
and is reclaimed once it is backed up. An empty archive would look like a service with no data, which
is a thing an operator discovers at restore time.

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

## 8.1 What is and is not an adapter category

**[D] Backup is the eighth category (R-252).** This reverses the decision recorded here through phase
8, and the reversal is written down rather than quietly applied.

**What this section used to say**, and why it was reasonable: writing a DR bundle to local disk, S3 or
a mounted share is a choice of byte sink. The adapter model exists for capability negotiation and
vocabulary translation; a destination that accepts bytes and returns them has neither, so it should be
a small `Destination` interface over `io.Writer`/`io.Reader` and nothing more. Making it a category
would give it a `Capabilities()` no caller consults and a `HealthCheck()` whose failure means nothing
until a backup runs.

**Why that was wrong.** It described a *destination* accurately and then assumed the destinations
people want are destinations. They are not. An object store expires objects on its own schedule,
versions them, and may hold a compliance lock that prevents deletion; a filesystem path does none of
those. So R-211's retention has two possible owners — Pando, or the destination — and which one is in
charge is a real question with a real wrong answer: if Pando prunes what the store has already locked,
every prune fails; if the store expires what Pando still counts as retained, a restore finds nothing.
That is precisely the "advertise capabilities as data" case R-254 exists for. A `Destination`
interface would have grown a capabilities struct within one more provider, under a worse name, and it
would have been consulted through a type assertion — which R-254 forbids because a type assertion is
invisible to the caller that needs to plan around it.

`HealthCheck()` earns its place for the same reason the argument dismissed it: an unreachable backup
destination is worth knowing about *before* the disaster, not at the moment a backup runs. That is
R-216's argument for verifying a bundle before it is needed, one level up.

**The test to apply before adding a ninth** is unchanged, and it is a good test: **does the planner
need to ask it a question, and does it have a vocabulary worth hiding?** Backup passes the first half
— retention ownership and whether the destination can list and expire are questions with plan-time
consequences. It passes the second thinly: "bucket" and "prefix" are a vocabulary, if a small one.
A category that passes neither is a library.

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
| backup | `local` | a filesystem path; retention owned by Pando |
| services | `docker` | postgres, mysql, redis in-bundle |
| notify | `console` | R-231 |
