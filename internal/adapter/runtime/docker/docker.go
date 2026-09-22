package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
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

	// labelFiles digests the configuration files carried into this container
	// (spec.File). They are copied in after create and leave no trace in the
	// container's own configuration, so this is what makes a changed file a
	// container that no longer matches its plan.
	labelFiles = "io.pando.files"

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

	// Volume sizes, reused briefly across usage readings (usage.go).
	usageMu          sync.Mutex
	volumeSizesCache map[string]int64
	volumeSizesAt    time.Time
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
		SupportsCarriedFiles:      true,
		SupportsExec:              true,
		SupportsMultipleWorkloads: true,
		SupportsPrivateNetwork:    true,
		SupportsResourceLimits:    true,

		// CPU and memory from the daemon's stats, disk from the container's
		// own layer and its volumes (R-245).
		ReportsUsage: true,

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

			// What the carried files were, so that changing one is a change
			// this container does not match. They go in after create and
			// leave no trace in the container's configuration, so without
			// this a spec whose only edit was a Caddyfile would converge to
			// "already running" and the edit would never ship.
			labelFiles: fileDigest(w.Files),
		},
	}
	if w.Health != nil {
		cfg.Healthcheck = healthConfig(w.Health)
	}

	hostCfg := &container.HostConfig{
		Binds: mounts,

		// No restart policy. Restarting a workload that stopped is the
		// reconciler's job, and it is the only thing that can do it to Pando's
		// rules: back off between attempts (R-149), give up at the threshold
		// (R-150), and then leave the app alone (R-151).
		//
		// `unless-stopped` put Docker in that seat instead, with no backoff and
		// no end. An app that could not start was restarted every two seconds
		// forever; Pando gave up on it, said "Pando has stopped trying to start
		// this app", and the restart count kept climbing underneath the
		// message. The loop also hid itself — a container restarted that often
		// reports Running at almost every instant the reconciler looks.
		//
		// A container that exits is started again by the next tick, which is
		// fifteen seconds rather than instant, and that is the trade: a paced
		// restart Pando knows about beats an instant one it does not.
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyDisabled},
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
		return createFailure(w, err)
	}

	// Configuration files, placed before the workload runs.
	//
	// Between create and start, which is the only moment they can go in: the
	// container's filesystem exists and nothing has read it yet. A volume
	// cannot do this — Docker will not mount a directory over a file in the
	// image — and a bind mount from the host would mean reading the repository
	// at deploy time, which R-020 forbids. The bytes come from the spec.
	for _, f := range w.Files {
		if err := a.placeFile(ctx, created.ID, f); err != nil {
			return errs.Wrap(errs.AdapterFailed,
				fmt.Sprintf("Could not put %s into %q.", f.Path, w.Name), err)
		}
	}

	if err := a.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return errs.Wrap(errs.AdapterFailed, fmt.Sprintf("Could not start %q.", w.Name), err)
	}
	return nil
}

// fileDigest summarizes a workload's carried files.
//
// Sorted by path, and covering the mode as well as the content: a file made
// executable is a different container from the same file that is not.
func fileDigest(files []api.FilePlan) string {
	if len(files) == 0 {
		return ""
	}

	sorted := append([]api.FilePlan(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	h := sha256.New()
	for _, f := range sorted {
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00", f.Path, f.Mode, len(f.Content))
		h.Write([]byte(f.Content))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// placeFile copies one file into a created container.
//
// CopyToContainer extracts a tar stream at a path in the container, so the
// archive is rooted at / and carries the file at its full path. The directories
// above it go in as entries of their own: Docker does not create a missing
// parent, and an image whose /etc/caddy does not exist yet is an ordinary
// image, not a broken one. An entry for a directory that already exists is a
// no-op at the mode these are written with.
func (a *Adapter) placeFile(ctx context.Context, containerID string, f api.FilePlan) error {
	mode := f.Mode
	if mode == 0 {
		mode = 0o644
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	clean := path.Clean(f.Path)
	for _, dir := range ancestors(path.Dir(clean)) {
		if err := tw.WriteHeader(&tar.Header{
			Name:     strings.TrimPrefix(dir, "/") + "/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
			ModTime:  time.Now(),
		}); err != nil {
			return err
		}
	}

	if err := tw.WriteHeader(&tar.Header{
		Name:    strings.TrimPrefix(clean, "/"),
		Mode:    int64(mode),
		Size:    int64(len(f.Content)),
		ModTime: time.Now(),
	}); err != nil {
		return err
	}
	if _, err := tw.Write([]byte(f.Content)); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}

	return a.cli.CopyToContainer(ctx, containerID, "/", &buf, container.CopyToContainerOptions{})
}

// ancestors lists a directory and everything above it, outermost first, so a
// tar stream creates them in an order extraction can follow.
func ancestors(dir string) []string {
	var out []string
	for d := path.Clean(dir); d != "/" && d != "." && d != ""; d = path.Dir(d) {
		out = append([]string{d}, out...)
	}
	return out
}

// createFailure says why the daemon refused, in the app's own terms.
//
// One refusal is worth naming. Storage Pando manages is a directory, and Docker
// will not mount a directory over a file that exists in the image, so a mount
// whose path inside the container is a file fails at create with
// "source /var/lib/docker/rootfs/overlayfs/a083.../etc/caddy/Caddyfile is not
// directory" — a path on the host that appears in nothing the person
// configured. Naming the mount instead gives them the line to remove (R-105).
//
// The compose importer now refuses such a mount at discovery, which is where it
// belongs. This is for the apps that already carry one, and for a mount typed
// in by hand.
func createFailure(w api.WorkloadPlan, err error) error {
	if text := err.Error(); strings.Contains(text, "not directory") ||
		strings.Contains(text, "not a directory") {
		// Which mount, read out of the daemon's own path: it is the
		// container's rootfs with the mount's path on the end, so the mount
		// that appears in it is the one that failed. Guessing from the path's
		// shape instead does not work — "Caddyfile" has no extension.
		var files []string
		for _, m := range w.Mounts {
			if strings.Contains(text, m.Path) {
				files = append(files, m.Path)
			}
		}
		if len(files) > 0 {
			return errs.Wrap(errs.AdapterFailed, fmt.Sprintf(
				"Could not create %q: its storage is mounted at %s, which is a file inside the image.",
				w.Name, strings.Join(files, " and ")), err).
				WithRemedy("Storage Pando manages is a directory and cannot stand in for a single " +
					"file. Remove that mount in the app's storage settings, and copy the file into " +
					"the image in its Dockerfile instead.")
		}
	}

	return errs.Wrap(errs.AdapterFailed, fmt.Sprintf("Could not create %q.", w.Name), err)
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

// RejoinNetworks puts this Pando container back on the network of every app
// that is still running (R-023, R-025).
//
// The set of networks Pando's container belongs to *is* the set of apps its
// proxy can reach, and that membership belongs to a container, not to an
// install. Replacing the Pando container — an upgrade, a `compose up --build`,
// any recreate — therefore starts one that is on none of them, while every app
// container carries on running perfectly. The apps are up; nothing can reach
// them; the reconciler sees a converged world and does nothing, because from
// its side the world *is* converged. Every app answers 502 until something
// happens to redeploy it.
//
// ensureNetwork already re-attaches, but only on the way through a deploy,
// which is the one thing that is not going to happen to an app that is already
// running the spec it is pinned to. So the attachment has to be restored at the
// moment it was lost: startup.
//
// Ordered after ReclaimNetworks deliberately. Reclaim removes the networks of
// apps that no longer exist, and it recognizes them by their being empty —
// joining first would put an endpoint on every one of them and make each look
// busy, turning a reclaim into a leak.
func (a *Adapter) RejoinNetworks(ctx context.Context) (int, error) {
	networks, err := a.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", labelManaged+"=true")),
	})
	if err != nil {
		return 0, errs.Wrap(errs.AdapterUnavailable, "Could not list the app networks.", err)
	}

	joined := 0
	for _, n := range networks {
		// NetworkList does not populate Containers, so an inspect is the only
		// way to tell a network with workloads on it from an empty one left by
		// a stopped app. An empty one is not worth an endpoint: the app's next
		// deploy attaches us, and until then there is nothing to reach.
		full, err := a.cli.NetworkInspect(ctx, n.ID, network.InspectOptions{})
		if err != nil || len(full.Containers) == 0 {
			continue
		}
		if err := a.attachProxy(ctx, n.ID); err != nil {
			// One unreachable app is not a reason to leave the rest
			// unreachable, and the app's own next deploy will try again.
			continue
		}
		joined++
	}
	return joined, nil
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

	// Docker frames the output of a container that has no TTY: an 8-byte header
	// before every chunk, saying which stream it came from and how long it is.
	// Pando creates every workload without one (see apply), so this stream
	// always carries that framing, and a caller copying it to a response body —
	// which is exactly what the logs endpoint does — puts control bytes through
	// the middle of the log somebody is reading.
	//
	// trial.go has a strip-it-from-a-buffer version of this for crash capture.
	// This is the streaming one: stdcopy unpicks the frames as they arrive, so
	// a followed log stays live.
	pr, pw := io.Pipe()
	go func() {
		_, err := stdcopy.StdCopy(pw, pw, rc)
		_ = rc.Close()
		// A closed reader ends the copy with an error that is not one: the
		// caller hung up, which is how following a log always ends.
		_ = pw.CloseWithError(err)
	}()
	return demuxed{PipeReader: pr, source: rc}, nil
}

// demuxed is the unframed stream, and closes the framed one behind it.
//
// Closing only the pipe would leave the connection to the daemon open and the
// goroutine copying into a reader nobody is holding — on a followed log, for as
// long as the container keeps printing.
type demuxed struct {
	*io.PipeReader
	source io.Closer
}

func (d demuxed) Close() error {
	_ = d.source.Close()
	return d.PipeReader.Close()
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
	if inspect.Config.Labels[labelFiles] != fileDigest(w.Files) {
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
		cfg.Test = []string{"CMD-SHELL", httpProbe(h.Port, h.Path)}
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

// httpProbe asks the app whether it is serving, with whatever the image has.
//
// It ran `wget` alone, which is not in every image — and an image without it
// answered "/bin/sh: 1: wget: not found" every thirty seconds until the deploy
// gave up, for an app that was serving perfectly well. The image belongs to
// the person who wrote the app, not to Pando, so the probe asks what is there
// rather than assuming: curl, then wget, then a TCP connection, which says
// less than an HTTP status but says it without needing anything installed.
//
// The last rung is bash's /dev/tcp, so it costs no package either. An image
// with none of the three is one this cannot probe, and it reports unhealthy —
// which is a worse answer than "unknown" and is why the rungs come first.
func httpProbe(port int, path string) string {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	return fmt.Sprintf(
		"if command -v curl >/dev/null 2>&1; then curl -fsS -o /dev/null %s; "+
			"elif command -v wget >/dev/null 2>&1; then wget --spider -q %s; "+
			"elif command -v nc >/dev/null 2>&1; then nc -z 127.0.0.1 %d; "+
			"else (exec 3<>/dev/tcp/127.0.0.1/%d) 2>/dev/null; fi || exit 1",
		url, url, port, port)
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

// Info describes this kind of adapter for the forms that configure one
// (api.KindInfo, R-261).
func Info() api.KindInfo {
	return api.KindInfo{
		Category:    api.CategoryRuntime,
		Kind:        Kind,
		Name:        "Docker",
		Description: "Runs apps as containers on a Docker host.",
		IDPrefix:    "rt_",
		Fields: []api.Field{
			{Key: "host", Label: "Docker host", Type: "string", Help: "The Docker endpoint. Empty uses the environment, which the bundled Compose file relies on.", Default: "DOCKER_HOST, or the local socket"},
			{Key: "total_cpu_millis", Label: "CPU available", Type: "int", Help: "Thousandths of a core Pando may allocate.", Default: "The whole machine"},
			{Key: "total_memory_bytes", Label: "Memory available", Type: "int", Help: "Bytes Pando may allocate.", Default: "The whole machine"},
			{Key: "total_disk_bytes", Label: "Disk available", Type: "int", Help: "Bytes of disk Pando may allocate."},
		},
	}
}
