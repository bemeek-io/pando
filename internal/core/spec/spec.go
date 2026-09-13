package spec

import "time"

// SchemaVersion is the current AppSpec wire version.
//
// Stored alongside every revision so migrating specs across Pando releases is
// mechanical rather than archaeological.
const SchemaVersion = 1

// AppSpec is the sole record of how an app runs.
//
// Nothing is read from the repo at deploy time (R-020). If it is not here, it
// does not happen. Three things are deliberately absent:
//
//   - Policy, which is evaluated live at plan time so R-274 works.
//   - Grants, which live separately so sharing does not create a revision.
//   - Secret values, which makes export safe to hand to someone (R-020).
//
// Adapters are named by reference, never by vocabulary (R-251): the spec says
// runtime: {adapter_ref: "rt_docker_local"}, never Docker arguments.
type AppSpec struct {
	SchemaVersion int       `json:"schema_version"`
	AppID         string    `json:"app_id"`
	Revision      int       `json:"revision"`
	CreatedAt     time.Time `json:"created_at"`
	CreatedBy     string    `json:"created_by"`
	Origin        Origin    `json:"origin"`

	Source    Source     `json:"source"`
	Build     Build      `json:"build"`
	Workloads []Workload `json:"workloads"`
	Volumes   []Volume   `json:"volumes"`
	Slots     []Slot     `json:"slots"`
	Routing   Routing    `json:"routing"`
	Runtime   RuntimeRef `json:"runtime"`
	Deploy    Deploy     `json:"deploy"`
	Health    Health     `json:"health"`
	Resources Resources  `json:"resources"`
	Egress    Egress     `json:"egress"`
	Retention Retention  `json:"retention"`

	// Warnings are carried, not resolved. A warning is never a blocker —
	// blockers are PLAN_* errors.
	Warnings []Warning `json:"warnings,omitempty"`
}

// Origin records how a revision came to be.
type Origin string

const (
	OriginDetected Origin = "detected"
	OriginEdited   Origin = "edited"
	OriginRedetect Origin = "redetected"
	OriginImported Origin = "imported"
	OriginManual   Origin = "manual"
)

// SourceType is where an app comes from.
type SourceType string

const (
	SourceGit    SourceType = "git"
	SourceImage  SourceType = "image"
	SourceUpload SourceType = "upload"
)

// Source identifies the app's input.
type Source struct {
	Type SourceType `json:"type"`
	URL  string     `json:"url,omitempty"`

	// Ref is what the user asked for; Commit is what runs (R-120). Auto-deploy
	// advances Commit and creates a revision. A deploy never resolves Ref
	// implicitly at runtime.
	Ref    string `json:"ref,omitempty"`
	Commit string `json:"commit,omitempty"`

	Subdir string `json:"subdir,omitempty"`
	Image  string `json:"image,omitempty"`
	Digest string `json:"digest,omitempty"`

	// UploadID names a stored upload — the app's own ID, for `pando deploy ./`
	// (design 04 §4). An upload has no commit and no ref: there is no revision
	// to resolve, and pretending otherwise would put a fabricated commit in the
	// spec, which R-120 exists to prevent.
	UploadID string `json:"upload_id,omitempty"`

	// CredentialRef names an app-owned secret (O-3). The credential belongs to
	// the app, not the person who supplied it, so an app keeps deploying after
	// its author leaves; the supplier is recorded in the audit event instead.
	CredentialRef string `json:"credential_ref,omitempty"`
}

// BuildStrategy is how an app is built.
type BuildStrategy string

const (
	BuildDockerfile BuildStrategy = "dockerfile"
	BuildCompose    BuildStrategy = "compose"
	BuildBuildpack  BuildStrategy = "buildpack"
	BuildStatic     BuildStrategy = "static"
	BuildPrebuilt   BuildStrategy = "prebuilt"
)

// IsolationClass is ordered so policy floors can be compared (R-114, R-255).
// Gaps of 10 leave room to insert classes without a migration.
type IsolationClass int

const (
	IsolationContainer     IsolationClass = 10
	IsolationSandboxed     IsolationClass = 20
	IsolationVM            IsolationClass = 30
	IsolationDedicatedHost IsolationClass = 40
)

// EgressMode controls outbound network access.
type EgressMode string

const (
	EgressAllowAll     EgressMode = "allow_all"
	EgressBlockPrivate EgressMode = "block_private"
	EgressAllowlist    EgressMode = "allowlist"
	EgressInherit      EgressMode = "inherit"
)

// KV is a name/value pair.
type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Build describes how the app is built.
type Build struct {
	Strategy   BuildStrategy `json:"strategy"`
	AdapterRef string        `json:"adapter_ref"`

	Dockerfile  string `json:"dockerfile,omitempty"`
	Context     string `json:"context,omitempty"`
	Target      string `json:"target,omitempty"`
	ComposeFile string `json:"compose_file,omitempty"`
	StaticDir   string `json:"static_dir,omitempty"`

	// GeneratedFiles are build inputs Pando produced rather than found, keyed
	// by path relative to the build context. Written into the checkout before
	// the build and never into anybody's repository.
	//
	// Opaque to core, which stores and replays them and interprets nothing:
	// only the builder that produced them knows what they mean (R-251). Today
	// that is a nixpacks plan — a Dockerfile and the files it references.
	//
	// They live in the spec because R-020 makes the state store the sole record
	// of how an app runs, and a plan regenerated at each build is not a record:
	// the same commit would build differently after the planner was upgraded.
	// Storing them pins the build to what somebody reviewed, and makes it
	// something they can edit without putting deployment files in their repo —
	// which is the other half of R-020.
	//
	// Append-only like the rest of the spec (R-152), so an edit is a revision
	// that can be diffed and rolled back.
	GeneratedFiles map[string]string `json:"generated_files,omitempty"`

	// Args are build arguments and are stored in the clear. Anything sensitive
	// is a slot — this is why there is no secret-bearing build arg.
	Args []KV `json:"args,omitempty"`

	// IsolationFloor is deliberately separate from Runtime.IsolationFloor
	// (R-114): how isolated a build must be is a different question from how
	// isolated the running app must be.
	IsolationFloor IsolationClass `json:"isolation_floor"`

	TimeoutSeconds int        `json:"timeout_seconds"`
	EgressMode     EgressMode `json:"egress_mode"`
	EgressAllow    []string   `json:"egress_allow,omitempty"`
}

// Workload is one process within the app's bundle.
type Workload struct {
	Name       string   `json:"name"`
	Image      string   `json:"image,omitempty"`
	Command    []string `json:"command,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	WorkingDir string   `json:"working_dir,omitempty"`

	Env       []EnvEntry   `json:"env,omitempty"`
	Ports     []Port       `json:"ports,omitempty"`
	Mounts    []Mount      `json:"mounts,omitempty"`
	DependsOn []string     `json:"depends_on,omitempty"`
	Health    *Healthcheck `json:"healthcheck,omitempty"`

	// Exposed means reachable through the proxy at a sub-route (R-026).
	Exposed bool `json:"exposed"`

	// Primary is the workload the app's URL resolves to. Exactly one workload
	// has it: the app has one canonical endpoint.
	Primary bool `json:"primary"`

	Resources *ResourceLimits `json:"resources,omitempty"`

	// Build is how this workload's image is produced, when it is built rather
	// than pulled. Nil means Image names something that already exists.
	//
	// Per workload because that is how a compose file describes an app: each
	// service either names an image or says how to build one, and a file with
	// two buildable services needs two images. The app-level Build block cannot
	// say that — it is one build — which is why importing a compose file used
	// to discard the build instructions entirely.
	Build *WorkloadBuild `json:"build,omitempty"`
}

// WorkloadBuild is how one workload's image is built.
//
// Deliberately smaller than the app-level Build: isolation, timeout and egress
// are properties of the build *environment*, which is the installation's
// business and the same for every workload in an app. What differs per service
// is only where its source is and which file describes it.
type WorkloadBuild struct {
	Context    string `json:"context,omitempty"`
	Dockerfile string `json:"dockerfile,omitempty"`
	Target     string `json:"target,omitempty"`
}

// EnvEntry is one environment variable. Exactly one of Value, SlotRef, or
// SecretRef is set — validation enforces it, because an entry with two sources
// has no defined resolution and an entry with none silently vanishes.
type EnvEntry struct {
	Key       string  `json:"key"`
	Value     *string `json:"value,omitempty"`
	SlotRef   *string `json:"slot_ref,omitempty"`
	SecretRef *string `json:"secret_ref,omitempty"`

	// Source records where this entry came from, the way Port.Source and
	// Volume.Declared already do.
	//
	// It decides what survives a re-detection. Detection proposes a whole spec,
	// and accepting one used to replace the pinned spec entirely — so a
	// variable somebody typed was gone the moment they re-detected after adding
	// a Dockerfile, with the app still running and no sign anything had been
	// dropped. Provenance is what lets the two be merged instead: an entry a
	// person set outlives anything Pando worked out for itself.
	Source EnvSource `json:"source,omitempty"`
}

// EnvSource records how an environment entry was determined.
type EnvSource string

const (
	// EnvFromUser is set by a person, and survives re-detection.
	EnvFromUser EnvSource = "user"

	// EnvFromCompose was imported from a compose file, and is replaced when
	// that file is read again — the file is the record, and somebody editing it
	// expects the change to land.
	EnvFromCompose EnvSource = "compose"

	// EnvFromDetection was inferred. Empty means the same thing: every entry
	// predating this field came from detection or an import, never from a
	// person, because there was no way to add one by hand.
	EnvFromDetection EnvSource = "detected"
)

// PortSource records how a port was determined.
//
// Retained because the review UI needs it: "we watched your app bind 3000"
// reads differently from "we guessed 3000 because it's a Next.js app", and the
// user reviewing the proposal deserves to know which.
type PortSource string

const (
	PortObserved  PortSource = "observed"
	PortExpose    PortSource = "expose"
	PortCompose   PortSource = "compose"
	PortFramework PortSource = "framework"
	PortUser      PortSource = "user"
)

// Port is a port a workload listens on.
type Port struct {
	Number   int        `json:"number"`
	Protocol string     `json:"protocol"`
	Source   PortSource `json:"source"`
}

// VolumeSource records how a volume came to be declared.
type VolumeSource string

const (
	VolumeFromCompose VolumeSource = "compose"
	VolumeFromUser    VolumeSource = "user"
	VolumeFromWarning VolumeSource = "detected-warning"
)

// Volume is persistent storage.
//
// Top-level and referenced by mounts rather than nested in a workload, because a
// volume outlives any single workload definition and must survive a revision
// that renames the workload.
type Volume struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Declared      VolumeSource `json:"declared"`
	SizeHintBytes int64        `json:"size_hint_bytes,omitempty"`
}

// Mount attaches a volume to a path.
type Mount struct {
	VolumeID string `json:"volume_id"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"read_only"`
}

// SlotType is the kind of dependency a slot declares.
type SlotType string

const (
	SlotPostgres SlotType = "postgres"
	SlotMySQL    SlotType = "mysql"
	SlotRedis    SlotType = "redis"
	SlotS3       SlotType = "s3"
	SlotSMTP     SlotType = "smtp"
	SlotUnknown  SlotType = "unknown"
)

// DisplayName is how a slot type reads in a sentence written for someone who
// may not know what a port is (R-005, R-105). "This app needs a redis" is the
// kind of phrasing that tells a reader they are not the audience.
func (t SlotType) DisplayName() string {
	switch t {
	case SlotPostgres:
		return "PostgreSQL database"
	case SlotMySQL:
		return "MySQL database"
	case SlotRedis:
		return "Redis"
	case SlotS3:
		return "file storage"
	case SlotSMTP:
		return "mail server"
	default:
		return "connection"
	}
}

// Slot is a declared, typed dependency awaiting resolution.
type Slot struct {
	Key      string   `json:"key"`
	Type     SlotType `json:"type"`
	Required bool     `json:"required"`
	Evidence []string `json:"evidence,omitempty"`

	// Resolution nil means unfilled. A required unfilled slot blocks deploy
	// (R-132) at plan time, not at deploy time.
	Resolution *Resolution `json:"resolution,omitempty"`
}

// ResolutionMode is how a slot is filled (R-131).
type ResolutionMode string

const (
	ResolutionProvisioned ResolutionMode = "provisioned"
	ResolutionBound       ResolutionMode = "bound"
	ResolutionLiteral     ResolutionMode = "literal"
)

// Resolution fills a slot.
type Resolution struct {
	Mode       ResolutionMode `json:"mode"`
	ServiceRef string         `json:"service_ref,omitempty"`
	Target     string         `json:"target,omitempty"`

	// SecretRef holds a literal value. A literal is stored as a secret rather
	// than inline, which keeps "export is safe to hand to someone" true without
	// a special case.
	SecretRef string `json:"secret_ref,omitempty"`
}

// RoutingMode is how traffic reaches the app.
type RoutingMode string

const (
	RoutingSubdomain RoutingMode = "subdomain"
	RoutingPath      RoutingMode = "path"
	RoutingPort      RoutingMode = "port"
)

// ModeSource records whether a routing mode was inherited or chosen.
// Relevant when host policy later restricts overrides (R-274, O-10).
type ModeSource string

const (
	ModeFromAdapterDefault ModeSource = "adapter_default"
	ModeFromUserOverride   ModeSource = "user_override"
)

// Routing places the app behind Pando's proxy.
type Routing struct {
	AdapterRef string      `json:"adapter_ref"`
	Mode       RoutingMode `json:"mode"`
	ModeSource ModeSource  `json:"mode_source"`
	Hostname   string      `json:"hostname,omitempty"`
	PathPrefix string      `json:"path_prefix,omitempty"`
	Port       int         `json:"port,omitempty"`
}

// RuntimeRef names the runtime adapter and its isolation floor.
type RuntimeRef struct {
	AdapterRef     string         `json:"adapter_ref"`
	IsolationFloor IsolationClass `json:"isolation_floor"`
}

// DeployStrategy is how a new version replaces the old.
type DeployStrategy string

const (
	// DeployRecreate is the default (R-144). The app is down during the swap,
	// which is the accepted cost.
	DeployRecreate DeployStrategy = "recreate"

	// DeployStartThenSwap runs two copies at once and is opt-in only (R-145).
	DeployStartThenSwap DeployStrategy = "start_then_swap"
)

// AutoDeployTrigger is what causes an automatic deploy.
type AutoDeployTrigger string

const (
	TriggerBranchUpdated AutoDeployTrigger = "branch_updated"
	TriggerReleaseTagged AutoDeployTrigger = "release_tagged"
)

// AutoDeploy configures automatic deploys. Off by default (R-141).
type AutoDeploy struct {
	Enabled bool              `json:"enabled"`
	Trigger AutoDeployTrigger `json:"trigger,omitempty"`
	Branch  string            `json:"branch,omitempty"`
}

// Deploy configures deployment behavior.
type Deploy struct {
	Strategy   DeployStrategy `json:"strategy"`
	AutoDeploy AutoDeploy     `json:"auto_deploy"`

	// AutoRollback is off by default (R-147), because an app with no health
	// signal is running rather than unhealthy, and rolling back on a signal
	// that does not exist would be worse than leaving it alone.
	AutoRollback bool `json:"auto_rollback"`
}

// HealthSource is where a health signal comes from, in precedence order
// (R-221).
type HealthSource string

const (
	HealthFromCompose HealthSource = "compose"
	HealthFromHTTP    HealthSource = "http"
	HealthFromTCP     HealthSource = "tcp"
	HealthFromProcess HealthSource = "process"
)

// Health configures health checking.
type Health struct {
	Source          HealthSource `json:"source"`
	Path            string       `json:"path,omitempty"`
	Port            int          `json:"port,omitempty"`
	IntervalSeconds int          `json:"interval_seconds"`
	TimeoutSeconds  int          `json:"timeout_seconds"`
	Retries         int          `json:"retries"`
}

// Healthcheck is a per-workload override.
type Healthcheck struct {
	Command         []string `json:"command,omitempty"`
	Path            string   `json:"path,omitempty"`
	Port            int      `json:"port,omitempty"`
	IntervalSeconds int      `json:"interval_seconds,omitempty"`
	TimeoutSeconds  int      `json:"timeout_seconds,omitempty"`
	Retries         int      `json:"retries,omitempty"`
}

// ResourceLimits caps one workload.
type ResourceLimits struct {
	CPUMillis   int   `json:"cpu_millis,omitempty"`
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
}

// Resources are the app's limits, inherited from host defaults (R-240).
type Resources struct {
	CPUMillis   int   `json:"cpu_millis"`
	MemoryBytes int64 `json:"memory_bytes"`
	DiskBytes   int64 `json:"disk_bytes"`

	// Overridden requires app.resources.override (R-241).
	Overridden bool `json:"overridden"`
}

// Egress controls outbound access.
//
// Mode == allowlist REPLACES the install-wide list rather than intersecting it
// (R-182). "Allowlist" reads like narrowing and it is not, which is why
// defining one is gated by app.egress.override (R-184).
type Egress struct {
	Mode      EgressMode `json:"mode"`
	Allowlist []string   `json:"allowlist,omitempty"`
}

// Retention caps what is kept.
type Retention struct {
	LogBytes         int64 `json:"log_bytes"`
	BackupDailyCount int   `json:"backup_daily_count"`
	SpecRevisions    int   `json:"spec_revisions"`
}

// Warning codes that must exist (design 01 §2.8).
const (
	WarnNoPersistentVolume        = "WARN_NO_PERSISTENT_VOLUME"
	WarnPathRoutingIncompatible   = "WARN_PATH_ROUTING_INCOMPATIBLE"
	WarnComposeConstructRewritten = "WARN_COMPOSE_CONSTRUCT_REWRITTEN"
	WarnUndeclaredDependency      = "WARN_UNDECLARED_DEPENDENCY_SUSPECTED"
)

// Warning is advisory. It lives in the spec and survives revisions until
// dismissed. A warning is never a blocker.
type Warning struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Dismissed   bool   `json:"dismissed"`
	DismissedBy string `json:"dismissed_by,omitempty"`
}

// PrimaryWorkload returns the workload the app's URL resolves to.
func (s *AppSpec) PrimaryWorkload() (Workload, bool) {
	for _, w := range s.Workloads {
		if w.Primary {
			return w, true
		}
	}
	return Workload{}, false
}

// Volume returns a declared volume by ID.
func (s *AppSpec) Volume(id string) (Volume, bool) {
	for _, v := range s.Volumes {
		if v.ID == id {
			return v, true
		}
	}
	return Volume{}, false
}

// Slot returns a declared slot by key.
func (s *AppSpec) Slot(key string) (Slot, bool) {
	for _, sl := range s.Slots {
		if sl.Key == key {
			return sl, true
		}
	}
	return Slot{}, false
}
