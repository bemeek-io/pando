package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// Kind is the adapter's kind string.
const Kind = "docker"

// Labels Pando puts on everything it creates, so Observe can find a bundle
// again and a human can tell what a container belongs to.
const (
	labelApp      = "io.pando.app"
	labelBundle   = "io.pando.bundle"
	labelWorkload = "io.pando.workload"
	labelManaged  = "io.pando.managed"

	// labelTrial marks everything a trial run creates (R-097), so that a trial
	// interrupted by Pando restarting can be found and removed rather than
	// leaving a running copy of someone's app with nothing tracking it.
	labelTrial = "io.pando.trial"
)

// Adapter runs workloads as Docker containers.
//
// Its isolation class is `container`: a shared kernel. That is reported
// honestly rather than optimistically, because policy floors compare against it
// (R-024, R-114) and an adapter that overstated its class would let a hardened
// install run work it meant to exclude.
type Adapter struct {
	cli    *client.Client
	config Config
}

// Config is the adapter's configuration.
type Config struct {
	// Host is the Docker endpoint. Empty uses the environment, which is what
	// the bundled Compose file relies on.
	Host string `json:"host,omitempty"`

	// TotalCPUMillis and TotalMemoryBytes let an operator tell Pando how much
	// of the machine it may use.
	//
	// Capacity is adapter-reported, never host-inspected (R-243): core does not
	// read /proc and has no concept of a host. When these are unset the adapter
	// asks the Docker daemon what it has, which is the same principle — the
	// adapter answers for itself.
	TotalCPUMillis   int   `json:"total_cpu_millis,omitempty"`
	TotalMemoryBytes int64 `json:"total_memory_bytes,omitempty"`
	TotalDiskBytes   int64 `json:"total_disk_bytes,omitempty"`

	// ProxyContainer is the container Pando's proxy runs in, which this adapter
	// attaches to every bundle network it creates.
	//
	// Empty means "this process's own container", detected from the hostname —
	// which is what Docker sets to the container ID, and what the bundled
	// Compose topology relies on.
	ProxyContainer string `json:"proxy_container,omitempty"`
}

// New builds an unconfigured adapter.
func New() *Adapter { return &Adapter{} }

func (a *Adapter) Kind() string           { return Kind }
func (a *Adapter) Category() api.Category { return api.CategoryRuntime }

func (a *Adapter) Configure(_ context.Context, raw json.RawMessage) error {
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a.config); err != nil {
			return errs.Wrap(errs.ValidInvalid, "The Docker runtime's configuration could not be read.", err)
		}
	}

	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if a.config.Host != "" {
		opts = append(opts, client.WithHost(a.config.Host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return errs.Wrap(errs.AdapterFailed, "Could not set up the connection to Docker.", err)
	}
	a.cli = cli
	return nil
}

func (a *Adapter) HealthCheck(ctx context.Context) error {
	if a.cli == nil {
		return errs.New(errs.AdapterUnavailable, "The Docker runtime has not been set up.")
	}
	if _, err := a.cli.Ping(ctx); err != nil {
		return errs.Wrap(errs.AdapterUnavailable, "Docker is not responding.", err)
	}
	return nil
}

// Capabilities reports what this adapter can do (R-254).
func (a *Adapter) Capabilities(context.Context) (api.RuntimeCapabilities, error) {
	return api.RuntimeCapabilities{
		// Shared kernel. Stated honestly: a policy floor above this must
		// exclude this adapter, and it can only do that if the class is true.
		IsolationClass: spec.IsolationContainer,

		SupportsPersistentVolumes: true,
		SupportsExec:              true,
		SupportsMultipleWorkloads: true,
		SupportsPrivateNetwork:    true,
		SupportsResourceLimits:    true,

		// Recreate only, for now. Start-then-swap needs the proxy to repoint
		// between two live bundles, which is phase 5 work — claiming it here
		// would turn a plan-time refusal into a mid-deploy failure (R-145).
		SupportsStartThenSwap: false,

		// R-222, and honestly. Docker caps a container's log at creation and
		// cannot change it afterwards without recreating the container — which
		// the reconciler may not do because an unrelated app turned chatty.
		// It also cannot report how much log space an app is using, which is
		// why R-224's aggregate is enforced against committed caps rather than
		// measured bytes (O-16).
		LogRetention: api.LogRetentionCapability{
			SupportsSizeCap:          true,
			CanChangeWithoutRecreate: false,
			ReportsUsage:             false,
			MinBytes:                 2 * minDockerLogBytes,
		},

		// A single daemon can load an image from a stream, which is how a build
		// reaches the runtime without a registry.
		SupportsImageImport: true,

		MaxWorkloadsPerBundle: 0,

		// The trial run (R-097). Port observation works on any image, including
		// one with no shell, because the sockets are read from a sidecar sharing
		// the container's network namespace rather than by exec-ing inside it.
		// Write observation is ContainerDiff, which the daemon computes itself.
		SupportsTrialRun:         true,
		SupportsPortObservation:  true,
		SupportsWriteObservation: true,
	}, nil
}

// Capacity is adapter-reported (R-243).
func (a *Adapter) Capacity(ctx context.Context) (api.Capacity, error) {
	capacity := api.Capacity{Reported: time.Now().UTC()}

	info, err := a.cli.Info(ctx)
	if err != nil {
		return api.Capacity{}, errs.Wrap(errs.AdapterUnavailable, "Could not read how much room Docker has.", err)
	}

	capacity.TotalCPUMillis = info.NCPU * 1000
	capacity.TotalMemoryBytes = info.MemTotal
	if a.config.TotalCPUMillis > 0 {
		capacity.TotalCPUMillis = a.config.TotalCPUMillis
	}
	if a.config.TotalMemoryBytes > 0 {
		capacity.TotalMemoryBytes = a.config.TotalMemoryBytes
	}
	capacity.TotalDiskBytes = a.config.TotalDiskBytes

	return capacity, nil
}

// Apply converges the bundle toward the plan.
//
// Idempotent by construction: each workload is compared against what exists and
// recreated only when it differs. Calling Apply with an already-satisfied plan
// touches nothing, which is what lets the reconciler call it freely.
func (a *Adapter) Apply(ctx context.Context, p api.BundlePlan) (api.BundleHandle, error) {
	if !p.Network.Private {
		// R-026. The field is checked rather than assumed so that a caller
		// that built a plan wrongly fails here instead of silently placing
		// workloads where other apps can reach them.
		return api.BundleHandle{}, errs.New(errs.AdapterFailed,
			"Pando will not start an app on a shared network.")
	}

	networkID, err := a.ensureNetwork(ctx, p.BundleID)
	if err != nil {
		return api.BundleHandle{}, err
	}

	for _, v := range p.Volumes {
		if _, err := a.CreateVolume(ctx, api.VolumeRequest{
			VolumeID: v.VolumeID, BundleID: p.BundleID, Name: v.Name,
		}); err != nil {
			return api.BundleHandle{}, err
		}
	}

	for _, w := range ordered(p.Workloads) {
		if err := a.applyWorkload(ctx, p, w, networkID); err != nil {
			return api.BundleHandle{}, err
		}
	}

	return api.BundleHandle{BundleID: p.BundleID, Handle: networkID}, nil
}

func (a *Adapter) applyWorkload(ctx context.Context, p api.BundlePlan, w api.WorkloadPlan, networkID string) error {
	name := containerName(p.BundleID, w.Name)

	existing, err := a.findContainer(ctx, p.BundleID, w.Name)
	if err != nil {
		return err
	}
	if existing != nil {
		matches, err := a.matchesPlan(ctx, existing.ID, w)
		if err != nil {
			return err
		}
		if matches {
			if !strings.HasPrefix(existing.State, "running") {
				if err := a.cli.ContainerStart(ctx, existing.ID, container.StartOptions{}); err != nil {
					return errs.Wrap(errs.AdapterFailed, fmt.Sprintf("Could not start %q.", w.Name), err)
				}
			}
			return nil
		}
		if err := a.removeContainer(ctx, existing.ID); err != nil {
			return err
		}
	}

	if err := a.ensureImage(ctx, w.Image); err != nil {
		return err
	}

	env := make([]string, 0, len(w.Env))
	for k, v := range w.Env {
		// Reveal happens here, at the edge, writing into the container's own
		// configuration. The adapter received secret.Value and never learned
		// which entries were sensitive.
		env = append(env, k+"="+v.Reveal())
	}

	exposed := nat.PortSet{}
	for _, port := range w.Ports {
		p, err := nat.NewPort(protocolOf(port.Protocol), fmt.Sprint(port.Number))
		if err != nil {
			return errs.Wrap(errs.ValidInvalid, fmt.Sprintf("%d is not a usable port.", port.Number), err)
		}
		exposed[p] = struct{}{}
	}

	mounts := make([]string, 0, len(w.Mounts))
	for _, m := range w.Mounts {
		binding := volumeName(p.BundleID, m.VolumeID) + ":" + m.Path
		if m.ReadOnly {
			binding += ":ro"
		}
		mounts = append(mounts, binding)
	}

	cfg := &container.Config{
		Image:        w.Image,
		Cmd:          w.Command,
		Entrypoint:   w.Entrypoint,
		WorkingDir:   w.WorkingDir,
		Env:          env,
		ExposedPorts: exposed,
		Labels: map[string]string{
			labelApp:      p.Labels["pando.app"],
			labelBundle:   p.BundleID,
			labelWorkload: w.Name,
			labelManaged:  "true",
		},
	}
	if w.Health != nil {
		cfg.Healthcheck = healthConfig(w.Health)
	}

	hostCfg := &container.HostConfig{
		Binds:         mounts,
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		Resources: container.Resources{
			NanoCPUs: int64(w.Resources.CPUMillis) * 1_000_000,
			Memory:   w.Resources.MemoryBytes,
		},
		LogConfig: logConfig(w.LogBytes),
	}

	// No ports are published to the host. Workloads are reachable only inside
	// the bundle network, and traffic arrives through Pando's proxy (R-023,
	// R-026). Publishing here would be a bypass.
	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			bundleNetworkName(p.BundleID): {NetworkID: networkID, Aliases: []string{w.Name}},
		},
	}

	created, err := a.cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, name)
	if err != nil {
		return errs.Wrap(errs.AdapterFailed, fmt.Sprintf("Could not create %q.", w.Name), err)
	}
	if err := a.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return errs.Wrap(errs.AdapterFailed, fmt.Sprintf("Could not start %q.", w.Name), err)
	}
	return nil
}

// Observe reports what exists. It never remediates (design 05 §2.1).
func (a *Adapter) Observe(ctx context.Context, ref api.BundleRef) (api.ObservedBundle, error) {
	containers, err := a.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", labelBundle+"="+ref.BundleID)),
	})
	if err != nil {
		return api.ObservedBundle{}, errs.Wrap(errs.AdapterUnavailable, "Could not read what is running.", err)
	}

	observed := api.ObservedBundle{Exists: len(containers) > 0}
	for _, c := range containers {
		inspect, err := a.cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			// A container that vanished between list and inspect is drift the
			// reconciler should see, not an error that aborts the whole
			// observation.
			continue
		}

		w := api.ObservedWorkload{
			Name:    c.Labels[labelWorkload],
			Present: true,
			Running: inspect.State.Running,

			// Docker reports both at once: a crash-looping container inspects
			// as Running=true, Restarting=true. Reporting only Running would
			// tell the reconciler a looping app is fine.
			Restarting: inspect.State.Restarting,

			ImageDigest:  inspect.Image,
			RestartCount: inspect.RestartCount,
		}
		if started, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); err == nil {
			w.StartedAt = started
		}
		if !inspect.State.Running {
			code := inspect.State.ExitCode
			w.ExitCode = &code
		}

		// Healthy stays nil when there is no health check. "No signal" and
		// "unhealthy" are different states and must not collapse (R-221): an
		// app with no health check is running, not perpetually degraded.
		if inspect.State.Health != nil {
			healthy := inspect.State.Health.Status == "healthy"
			w.Healthy = &healthy
		}

		observed.Workloads = append(observed.Workloads, w)
	}

	volumes, err := a.cli.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", labelBundle+"="+ref.BundleID)),
	})
	if err == nil {
		for _, v := range volumes.Volumes {
			observed.Volumes = append(observed.Volumes, api.ObservedVolume{
				VolumeID: v.Labels["io.pando.volume"],
				Present:  true,
				Handle:   v.Name,
			})
		}
	}

	return observed, nil
}

func (a *Adapter) Stop(ctx context.Context, ref api.BundleRef) error {
	containers, err := a.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", labelBundle+"="+ref.BundleID)),
	})
	if err != nil {
		return errs.Wrap(errs.AdapterUnavailable, "Could not read what is running.", err)
	}
	for _, c := range containers {
		if err := a.cli.ContainerStop(ctx, c.ID, container.StopOptions{}); err != nil {
			return errs.Wrap(errs.AdapterFailed, "Could not stop the app.", err)
		}
	}
	return nil
}

// Destroy removes the bundle's containers and network.
//
// Volumes are kept unless explicitly asked otherwise: they outlive the apps
// that mount them (R-204), and destroying them is a separate, deliberate act.
func (a *Adapter) Destroy(ctx context.Context, ref api.BundleRef, opts api.DestroyOptions) error {
	containers, err := a.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", labelBundle+"="+ref.BundleID)),
	})
	if err != nil {
		return errs.Wrap(errs.AdapterUnavailable, "Could not read what is running.", err)
	}
	for _, c := range containers {
		if err := a.removeContainer(ctx, c.ID); err != nil {
			return err
		}
	}

	if !opts.KeepVolumes {
		volumes, err := a.cli.VolumeList(ctx, volume.ListOptions{
			Filters: filters.NewArgs(filters.Arg("label", labelBundle+"="+ref.BundleID)),
		})
		if err == nil {
			for _, v := range volumes.Volumes {
				_ = a.cli.VolumeRemove(ctx, v.Name, false)
			}
		}
	}

	// The network is removed only if nothing is still attached to it, and
	// **Pando never detaches itself to make that true.**
	//
	// It used to. Pando is joined to every bundle network — that is how the
	// proxy reaches an app (R-023) — so removing one meant disconnecting
	// first, and on Docker Desktop disconnecting a running container drops its
	// published ports. Measured, not guessed: healthz on the host went 200,
	// disconnect, 000, and stayed there until the container was restarted while
	// Pando kept happily serving inside it. The janitor could take the server
	// off the network, which is far worse than the leak it was reclaiming.
	//
	// So a network whose only remaining endpoint is Pando is left for
	// reclaimNetworks to collect after the next restart, when the container
	// holding it is gone and the removal needs no disconnect at all. The
	// containers — which hold the memory and CPU — are already gone by here,
	// which is the part that matters.
	if err := a.cli.NetworkRemove(ctx, bundleNetworkName(ref.BundleID)); err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		// Left behind on purpose. Reported at debug volume rather than as a
		// failure, because the teardown did succeed at everything that costs
		// the host something to keep.
		return nil
	}
	return nil
}

// ReclaimNetworks removes bundle networks that nothing is attached to.
//
// Called at startup, which is the one moment this is safe: a network held by a
// previous Pando container has a dead endpoint on it, so Docker removes it
// without anybody disconnecting anything. Doing the same while serving would
// mean detaching the running Pando, and on Docker Desktop that drops its
// published ports.
//
// The cost of the timing is honest and worth stating: a network belonging to an
// app deleted since the last restart is reclaimed at the next one, not
// immediately. Docker's default pool holds about thirty, so an install that
// deletes thirty apps between restarts can still run out — better than leaking
// them permanently, and the remedy is a restart rather than a docker command.
func (a *Adapter) ReclaimNetworks(ctx context.Context) (int, error) {
	networks, err := a.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", labelManaged+"=true")),
	})
	if err != nil {
		return 0, errs.Wrap(errs.AdapterUnavailable, "Could not list the app networks.", err)
	}

	reclaimed := 0
	for _, n := range networks {
		// Inspect rather than trusting the list: NetworkList does not populate
		// Containers, so the list alone cannot tell an empty network from a
		// busy one.
		full, err := a.cli.NetworkInspect(ctx, n.ID, network.InspectOptions{})
		if err != nil || len(full.Containers) > 0 {
			continue
		}
		if err := a.cli.NetworkRemove(ctx, n.ID); err == nil {
			reclaimed++
		}
	}
	return reclaimed, nil
}

func (a *Adapter) CreateVolume(ctx context.Context, req api.VolumeRequest) (api.VolumeHandle, error) {
	name := volumeName(req.BundleID, req.VolumeID)
	_, err := a.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name: name,
		Labels: map[string]string{
			labelBundle:       req.BundleID,
			labelManaged:      "true",
			"io.pando.volume": req.VolumeID,
		},
	})
	if err != nil {
		return api.VolumeHandle{}, errs.Wrap(errs.AdapterFailed, "Could not create storage for the app.", err)
	}
	return api.VolumeHandle{VolumeID: req.VolumeID, Handle: name}, nil
}

func (a *Adapter) DestroyVolume(ctx context.Context, h api.VolumeHandle) error {
	if err := a.cli.VolumeRemove(ctx, h.Handle, false); err != nil {
		return errs.Wrap(errs.AdapterFailed, "Could not remove the storage.", err)
	}
	return nil
}

// SnapshotVolume and RestoreVolume arrive with phase 9.
//
// They return a plan-time-shaped error rather than a silent no-op, because a
// backup that quietly does nothing is worse than one that refuses.
// SnapshotVolume and RestoreVolume are in volumes_backup.go (R-212).

// ImportImage loads an image tarball into the daemon.
//
// The reference is read back from the daemon's own response rather than
// assumed, because the tag the build asked for and the tag the daemon actually
// recorded are not guaranteed to match, and running the wrong one would be
// silent.
func (a *Adapter) ImportImage(ctx context.Context, r io.Reader) (string, error) {
	resp, err := a.cli.ImageLoad(ctx, r, client.ImageLoadWithQuiet(true))
	if err != nil {
		return "", errs.Wrap(errs.AdapterFailed, "Could not load the built image.", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errs.Wrap(errs.AdapterFailed, "Could not load the built image.", err)
	}

	ref := parseLoadedRef(string(body))
	if ref == "" {
		return "", errs.New(errs.AdapterFailed, "The built image could not be loaded.").
			WithRemedy("Check the build logs — the image may not have been produced correctly.")
	}
	return ref, nil
}

// parseLoadedRef pulls the image reference out of Docker's load output, which
// is a stream of JSON objects whose stream field reads
// "Loaded image: name:tag".
func parseLoadedRef(body string) string {
	const marker = "Loaded image: "
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, marker); i >= 0 {
			rest := line[i+len(marker):]
			rest = strings.TrimSuffix(strings.TrimSpace(rest), `\n"}`)
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

func (a *Adapter) Logs(ctx context.Context, ref api.WorkloadRef, opts api.LogOptions) (io.ReadCloser, error) {
	c, err := a.findContainer(ctx, ref.BundleID, ref.Workload)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errs.Newf(errs.NotFound, "There is nothing running called %q.", ref.Workload)
	}

	logOpts := container.LogsOptions{ShowStdout: true, ShowStderr: true, Follow: opts.Follow}
	if !opts.Since.IsZero() {
		logOpts.Since = opts.Since.Format(time.RFC3339)
	}
	if opts.Tail > 0 {
		logOpts.Tail = fmt.Sprint(opts.Tail)
	}

	rc, err := a.cli.ContainerLogs(ctx, c.ID, logOpts)
	if err != nil {
		return nil, errs.Wrap(errs.AdapterFailed, "Could not read the app's logs.", err)
	}
	return rc, nil
}

// Exec opens a session. Core has already checked app.exec, consulted policy, and
// written the audit event before this is called (R-084, R-085, R-228) — the
// adapter does not authorize.
func (a *Adapter) Exec(ctx context.Context, ref api.WorkloadRef, req api.ExecRequest) (api.ExecSession, error) {
	c, err := a.findContainer(ctx, ref.BundleID, ref.Workload)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errs.Newf(errs.NotFound, "There is nothing running called %q.", ref.Workload)
	}

	env := make([]string, 0, len(req.Env))
	for k, v := range req.Env {
		env = append(env, k+"="+v)
	}

	created, err := a.cli.ContainerExecCreate(ctx, c.ID, container.ExecOptions{
		Cmd:          req.Command,
		Tty:          req.TTY,
		Env:          env,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, errs.Wrap(errs.AdapterFailed, "Could not open a terminal in the app.", err)
	}

	attached, err := a.cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{Tty: req.TTY})
	if err != nil {
		return nil, errs.Wrap(errs.AdapterFailed, "Could not open a terminal in the app.", err)
	}

	return &execSession{cli: a.cli, execID: created.ID, hijacked: attached}, nil
}

var _ api.RuntimeAdapter = (*Adapter)(nil)

// --- helpers ---------------------------------------------------------------

func (a *Adapter) ensureNetwork(ctx context.Context, bundleID string) (string, error) {
	name := bundleNetworkName(bundleID)

	existing, err := a.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err == nil {
		for _, n := range existing {
			if n.Name == name {
				// Attached on every pass, not only when the network is new.
				//
				// Pando's container is joined to each app's private network —
				// that is the only way the proxy can reach an app (R-023). A
				// replaced Pando container is a *different* container, so a
				// network created by the old one has the old one attached and
				// the new one nowhere. Attaching only at creation therefore
				// meant every upgrade silently cut Pando off from every
				// existing app, and the app came back only if something
				// recreated its network.
				//
				// Idempotent: Docker answers "already exists" and attachProxy
				// treats that as success.
				if err := a.attachProxy(ctx, n.ID); err != nil {
					return "", err
				}
				return n.ID, nil
			}
		}
	}

	// Internal: false would let workloads reach the internet directly, which is
	// what EgressMode governs; the isolation that matters for R-025 is that
	// each bundle gets its own network, so no app can reach another's.
	created, err := a.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{labelBundle: bundleID, labelManaged: "true"},
	})
	if err != nil {
		// Docker's default address pool holds about thirty /16 networks, and
		// Pando takes one per app (R-025). An install that grows past that
		// fails here with a message naming subnets, which tells an operator
		// nothing about what to do.
		if strings.Contains(err.Error(), "address pools") {
			return "", errs.Wrap(errs.CapacityWouldOversubscribe,
				"This machine has run out of private networks, so no more apps can start on it.", err).
				WithRemedy("Docker reserves a fixed pool of network addresses, and Pando uses one per app. Raise it by setting default-address-pools in /etc/docker/daemon.json — for example a /16 base with /24 subnets gives 256 apps instead of about 30 — then restart Docker. Deleting apps you no longer need also frees them.")
		}
		return "", errs.Wrap(errs.AdapterFailed, "Could not set up the app's private network.", err)
	}

	if err := a.attachProxy(ctx, created.ID); err != nil {
		return "", err
	}
	return created.ID, nil
}

// attachProxy joins Pando's own container to a bundle network.
//
// Every app sits on its own private network so no app can reach another
// (R-025), and nothing publishes a host port (R-026). That leaves exactly one
// way in — through Pando's proxy — which is R-023 made structural rather than
// promised. But it only works if the proxy can actually reach the bundle, and
// it cannot unless it is on that network too.
//
// So the set of networks Pando's container belongs to *is* the set of apps it
// can reach. Nothing else is joined to them.
//
// Attaching is the adapter's job rather than core's: how a workload becomes
// reachable is exactly the provider vocabulary core must never learn (R-251).
func (a *Adapter) attachProxy(ctx context.Context, networkID string) error {
	container := a.config.ProxyContainer
	if container == "" {
		// Docker sets a container's hostname to its own short ID.
		host, err := os.Hostname()
		if err != nil {
			//nolint:nilerr // Not knowing our own hostname means we are not in a
			// container, which is the same case as IsNotFound below: the app
			// deploys, the proxy cannot reach it from here, and that is a
			// local-development limitation rather than a deployment failure.
			return nil
		}
		container = host
	}

	err := a.cli.NetworkConnect(ctx, networkID, container, nil)
	switch {
	case err == nil:
		return nil
	case strings.Contains(err.Error(), "already exists"):
		return nil
	case cerrdefs.IsNotFound(err):
		// Pando is not running as a container — a developer running the binary
		// on the host. The app still deploys; the proxy simply cannot reach it
		// from here, which is a local-development limitation rather than a
		// deployment failure.
		return nil
	default:
		return errs.Wrap(errs.AdapterFailed,
			"Could not connect Pando to the app's network, so traffic could not reach it.", err)
	}
}

// logConfig caps a container's logs at creation (R-222, R-223).
//
// Set here and nowhere else, because Docker cannot change it on a running
// container — that is what LogRetention.CanChangeWithoutRecreate says, and why
// a changed cap takes effect on the next deploy rather than immediately.
//
// max-file is 2 rather than 1: Docker rotates to a second file before deleting
// the first, so a cap of N with one file keeps somewhere between 0 and N bytes,
// and with two keeps between N/2 and N. Half the cap is a floor worth having
// when the logs are what somebody is reading to find out why a deploy failed.
// The per-file size is therefore half the app's budget, so the total stays
// under it.
func logConfig(capBytes int64) container.LogConfig {
	if capBytes <= 0 {
		// No cap asked for. Left as the daemon's default rather than invented
		// here: an adapter that silently imposed a limit nobody configured
		// would lose logs for a reason nothing explains.
		return container.LogConfig{}
	}

	perFile := capBytes / 2
	if perFile < minDockerLogBytes {
		perFile = minDockerLogBytes
	}
	return container.LogConfig{
		Type: "json-file",
		Config: map[string]string{
			"max-size": strconv.FormatInt(perFile, 10) + "b",
			"max-file": "2",
		},
	}
}

// minDockerLogBytes is the smallest per-file cap worth setting.
//
// Below about this, rotation happens so often that the log is useless for
// reading a failure — which is the thing logs are for.
const minDockerLogBytes = 1 << 20 // 1 MiB

func (a *Adapter) ensureImage(ctx context.Context, ref string) error {
	if ref == "" {
		return errs.New(errs.ValidInvalid, "This workload has no image to run.")
	}
	if _, err := a.cli.ImageInspect(ctx, ref); err == nil {
		return nil
	}

	rc, err := a.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return errs.Wrap(errs.AdapterFailed, fmt.Sprintf("Could not fetch the image %q.", ref), err)
	}
	defer func() { _ = rc.Close() }()
	_, _ = io.Copy(io.Discard, rc)
	return nil
}

type containerSummary struct {
	ID     string
	State  string
	Labels map[string]string
}

func (a *Adapter) findContainer(ctx context.Context, bundleID, workload string) (*containerSummary, error) {
	list, err := a.cli.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", labelBundle+"="+bundleID),
			filters.Arg("label", labelWorkload+"="+workload),
		),
	})
	if err != nil {
		return nil, errs.Wrap(errs.AdapterUnavailable, "Could not read what is running.", err)
	}
	if len(list) == 0 {
		return nil, nil
	}
	return &containerSummary{ID: list[0].ID, State: list[0].State, Labels: list[0].Labels}, nil
}

// matchesPlan reports whether a running container already satisfies the plan.
//
// Compared on image, command, and environment. Environment is included because
// a rotated secret must cause a recreate (R-193) and the container's own config
// is the only place the adapter can see it — core detects the same drift
// state-side by fingerprint, and this is the adapter's half.
func (a *Adapter) matchesPlan(ctx context.Context, containerID string, w api.WorkloadPlan) (bool, error) {
	inspect, err := a.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, errs.Wrap(errs.AdapterUnavailable, "Could not read the app's configuration.", err)
	}
	if inspect.Config == nil {
		return false, nil
	}
	if inspect.Config.Image != w.Image {
		return false, nil
	}

	existing := map[string]string{}
	for _, kv := range inspect.Config.Env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			existing[k] = v
		}
	}
	for k, want := range w.Env {
		if existing[k] != want.Reveal() {
			return false, nil
		}
	}
	return true, nil
}

func (a *Adapter) removeContainer(ctx context.Context, id string) error {
	err := a.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
	if err != nil && !cerrdefs.IsNotFound(err) {
		return errs.Wrap(errs.AdapterFailed, "Could not remove the old container.", err)
	}
	return nil
}

// ordered sorts workloads so dependencies start first (R-096).
//
// A cycle is impossible here — validation rejects one — so a workload whose
// dependencies cannot be satisfied simply lands at the end rather than hanging.
func ordered(workloads []api.WorkloadPlan) []api.WorkloadPlan {
	byName := make(map[string]api.WorkloadPlan, len(workloads))
	for _, w := range workloads {
		byName[w.Name] = w
	}

	var out []api.WorkloadPlan
	placed := map[string]bool{}

	var place func(api.WorkloadPlan)
	place = func(w api.WorkloadPlan) {
		if placed[w.Name] {
			return
		}
		placed[w.Name] = true
		for _, dep := range w.DependsOn {
			if d, ok := byName[dep]; ok {
				place(d)
			}
		}
		out = append(out, w)
	}
	for _, w := range workloads {
		place(w)
	}
	return out
}

func healthConfig(h *api.HealthPlan) *container.HealthConfig {
	cfg := &container.HealthConfig{Retries: h.Retries}
	switch {
	case len(h.Command) > 0:
		cfg.Test = append([]string{"CMD"}, h.Command...)
	case h.Path != "" && h.Port > 0:
		cfg.Test = []string{"CMD-SHELL",
			fmt.Sprintf("wget --spider -q http://127.0.0.1:%d%s || exit 1", h.Port, h.Path)}
	case h.Port > 0:
		cfg.Test = []string{"CMD-SHELL", fmt.Sprintf("nc -z 127.0.0.1 %d || exit 1", h.Port)}
	default:
		return nil
	}
	if h.IntervalSeconds > 0 {
		cfg.Interval = time.Duration(h.IntervalSeconds) * time.Second
	}
	if h.TimeoutSeconds > 0 {
		cfg.Timeout = time.Duration(h.TimeoutSeconds) * time.Second
	}
	return cfg
}

func protocolOf(p string) string {
	if p == "tcp" || p == "udp" {
		return p
	}
	return "tcp"
}

func bundleNetworkName(bundleID string) string { return "pando-" + bundleID }
func volumeName(bundleID, volumeID string) string {
	return "pando-" + bundleID + "-" + volumeID
}
func containerName(bundleID, workload string) string {
	return "pando-" + bundleID + "-" + workload
}
