package detect

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// ImportCompose turns a compose file into a draft spec.
//
// R-096: a compose file is a complete answer, not a hint. The author already
// said what runs, in what order, with which volumes and healthchecks — so this
// imports it rather than treating it as evidence for a guess.
//
// R-099 is the other half. Some compose constructs cannot survive the boundary
// Pando puts around an app, and this returns one of two outcomes for each:
//
//	rejected  — PLAN_COMPOSE_CONSTRUCT_REJECTED, naming the service and the
//	            construct. There is nothing to rewrite it to.
//	rewritten — imported with different mechanism and the same behavior, and
//	            a WARN_COMPOSE_CONSTRUCT_REWRITTEN saying exactly what changed.
//
// The split is not about severity. It is about whether the app still does what
// its author meant afterwards. Publishing a host port is a mechanism Pando
// replaces with its own routing and the app is still reachable, so it is
// rewritten. `privileged: true` has no replacement that keeps the isolation the
// rest of the system depends on, so it is refused.
func ImportCompose(src api.SourceView, name string) (Draft, error) {
	f, err := src.Open(name)
	if err != nil {
		return Draft{}, errs.Wrap(errs.ValidInvalid, "Could not read "+name+".", err)
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(io.LimitReader(f, 4<<20))
	if err != nil {
		return Draft{}, errs.Wrap(errs.ValidInvalid, "Could not read "+name+".", err)
	}

	var file composeFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return Draft{}, errs.Wrap(errs.ValidInvalid,
			"Could not understand "+name+" — it is not valid compose YAML.", err).
			WithRemedy("Check the file with `docker compose config` and fix what it reports.")
	}
	if len(file.Services) == 0 {
		return Draft{}, errs.Newf(errs.ValidInvalid,
			"%s declares no services, so there is nothing to run.", name).
			WithRemedy("Add a `services:` section naming at least one service.")
	}

	imp := &composeImport{file: file, source: name, src: src}
	if err := imp.rejectIncompatible(); err != nil {
		return Draft{}, err
	}
	return imp.draft(), nil
}

// --- the compose file, as much of it as Pando reads -------------------------

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]any            `yaml:"volumes"`
}

type composeService struct {
	Image       string         `yaml:"image"`
	Build       any            `yaml:"build"`
	Command     any            `yaml:"command"`
	Entrypoint  any            `yaml:"entrypoint"`
	WorkingDir  string         `yaml:"working_dir"`
	Ports       []any          `yaml:"ports"`
	Expose      []any          `yaml:"expose"`
	Volumes     []any          `yaml:"volumes"`
	Environment any            `yaml:"environment"`
	DependsOn   any            `yaml:"depends_on"`
	Healthcheck *composeHealth `yaml:"healthcheck"`

	// Constructs that are rejected or rewritten (R-099).
	Restart       string         `yaml:"restart"`
	ContainerName string         `yaml:"container_name"`
	NetworkMode   string         `yaml:"network_mode"`
	Privileged    bool           `yaml:"privileged"`
	PID           string         `yaml:"pid"`
	IPC           string         `yaml:"ipc"`
	Devices       []string       `yaml:"devices"`
	CapAdd        []string       `yaml:"cap_add"`
	Deploy        *composeDeploy `yaml:"deploy"`
}

type composeHealth struct {
	Test     any    `yaml:"test"`
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`
	Retries  int    `yaml:"retries"`
}

type composeDeploy struct {
	Replicas *int `yaml:"replicas"`
}

type composeImport struct {
	file   composeFile
	source string

	// src is the tree the compose file came out of, kept so the import can
	// ask it what a path is. A bind mount says nothing about whether its
	// source is a directory of data or a single configuration file, and the
	// two import differently — see the mount rejection below.
	src api.SourceView

	warnings []spec.Warning
}

// isFile reports whether a relative compose path names a regular file in the
// repository. An unreadable or missing path is not one: compose creates a
// directory for a bind mount that does not exist yet, and so a path Pando
// cannot see is a directory as far as this is concerned.
func (c *composeImport) isFile(hostPath string) bool {
	if c.src == nil {
		return false
	}
	clean := path.Clean(strings.TrimPrefix(hostPath, "./"))
	if clean == "." || strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "/") {
		return false
	}
	info, err := c.src.Stat(clean)
	return err == nil && !info.IsDir
}

// names returns the service names in a stable order.
//
// Compose services live in a map, and Go randomizes map iteration. Without
// this, which service Pando calls primary — and the order of the options in the
// question asking about it — would change between runs of the same repository.
func (c *composeImport) names() []string {
	names := make([]string, 0, len(c.file.Services))
	for name := range c.file.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// --- rejection (R-099) ------------------------------------------------------

// rejection is one construct that cannot be imported.
type rejection struct {
	Service   string `json:"service"`
	Construct string `json:"construct"`
	Reason    string `json:"reason"`
}

// rejectIncompatible reports every construct that cannot cross the boundary.
//
// Every service is checked before anything is reported, so someone fixing a
// compose file gets the whole list rather than discovering the next problem
// after fixing the last.
//
// None of these is overridable. R-099 says "host policy governs whether an
// admin may override", but R-272 is the general pattern and it says policy can
// only raise the floor — an admin can narrow what Pando accepts and cannot
// widen it. See docs/design/notes-compose-override-conflict.md.
func (c *composeImport) rejectIncompatible() error {
	var found []rejection

	for _, name := range c.names() {
		s := c.file.Services[name]

		if mode := strings.TrimSpace(s.NetworkMode); mode != "" && mode != "bridge" && mode != "none" {
			found = append(found, rejection{name, "network_mode: " + mode,
				"Pando puts each app on its own private network and reaches it through the proxy. " +
					"A service sharing the host's network stack is not inside that boundary, " +
					"and nothing else in the system would still hold."})
		}
		if s.Privileged {
			found = append(found, rejection{name, "privileged: true",
				"A privileged container can reach the host it runs on, which is the one thing " +
					"the isolation between apps exists to prevent."})
		}
		if strings.TrimSpace(s.PID) == "host" {
			found = append(found, rejection{name, "pid: host",
				"Sharing the host's process namespace lets the service see and signal every " +
					"other process on the machine, including Pando's."})
		}
		if strings.TrimSpace(s.IPC) == "host" {
			found = append(found, rejection{name, "ipc: host",
				"Sharing the host's IPC namespace reaches memory belonging to other apps."})
		}
		if len(s.Devices) > 0 {
			found = append(found, rejection{name, "devices: " + strings.Join(s.Devices, ", "),
				"Passing a host device through gives the app direct hardware access, which " +
					"crosses the same boundary as a privileged container."})
		}
		if s.Deploy != nil && s.Deploy.Replicas != nil && *s.Deploy.Replicas > 1 {
			found = append(found, rejection{name,
				"deploy.replicas: " + strconv.Itoa(*s.Deploy.Replicas),
				"Pando runs one instance of an app in one place (R-010). It is not an " +
					"orchestrator, and pretending to honor a replica count would be a lie " +
					"about where the app is running."})
		}
		for _, mount := range parseMounts(s.Volumes) {
			if mount.hostPath == "" {
				continue
			}
			if isAbsoluteHostPath(mount.hostPath) {
				found = append(found, rejection{name, "volume " + mount.raw,
					"This mounts a path from the host machine into the app. Pando has no way to " +
						"honor it: the app may not run on the machine holding that path, and if it " +
						"did, the mount would reach outside the app's own storage."})
				continue
			}

			// A bind mount of a single file out of the repository — a
			// Caddyfile, an nginx.conf, an init.sql. A directory of them
			// becomes a volume Pando manages (see mounts below); a file
			// cannot, twice over. Nothing is read from the repository at
			// deploy time (R-020), so there would be nothing to put in that
			// volume, and Docker refuses to mount a directory over a file in
			// the image anyway — which is how this used to surface: an app
			// that imported and scanned and built, and then failed at the
			// last step with `source /var/lib/docker/... is not directory`.
			if c.isFile(mount.hostPath) {
				reason := "This mounts the single file " + mount.hostPath + " into the container at " +
					mount.containerPath + ". Pando's storage is a directory, and nothing is " +
					"read from the repository when an app is deployed, so there is nothing " +
					"for that file to come from. Copy it into the image instead — a `COPY " +
					path.Base(mount.hostPath) + " " + mount.containerPath + "` line in the " +
					"service's Dockerfile does what this mount was doing."

				// The common case for a mounted config file is a reverse proxy
				// in front of the app, and under Pando that service has no work
				// left to do: Pando terminates TLS, gives the app an address
				// and routes to it (R-023). Saying so turns a file somebody has
				// to relocate into a service they can delete.
				if isReverseProxy(s.Image) {
					reason += " This service is a reverse proxy, and Pando is already one: it " +
						"gives this app an address, terminates TLS and routes traffic to it. " +
						"Removing the service from the compose file is the other way out."
				}

				found = append(found, rejection{name, "volume " + mount.raw, reason})
			}
		}
	}

	if len(found) == 0 {
		return nil
	}

	details := make([]map[string]any, 0, len(found))
	summary := make([]string, 0, len(found))
	for _, r := range found {
		details = append(details, map[string]any{
			"service": r.Service, "construct": r.Construct, "reason": r.Reason,
		})
		summary = append(summary, r.Service+": "+r.Construct)
	}

	// "1 construct(s)" is how a message written for the plural case reads in
	// the single one, which is the common one.
	count := fmt.Sprintf("%d constructs", len(found))
	if len(found) == 1 {
		count = "a construct"
	}

	return errs.Newf(errs.PlanComposeConstructRejected,
		"%s uses %s that cannot run inside the boundary Pando puts around an app: %s.",
		c.source, count, strings.Join(summary, "; ")).
		WithDetail("rejected", details).
		WithRemedy("Each entry above says why. Remove or change those lines in " + c.source +
			", or deploy without the compose file by supplying an image and a command instead.")
}

// build resolves how the app is built, from the compose file, into the spec.
//
// This used to emit `strategy: compose` with a pointer to the file, and two
// things followed. No builder implements compose — BuildKit declares
// `dockerfile` and nothing else — so every compose app that needed building was
// refused at plan time with "bld_buildkit cannot build this app the way it is
// set up", naming the builder rather than the cause. And it left the build
// instructions in the repository to be re-read later, which R-020 forbids
// outright: the spec is the sole record of how an app runs, and a `build:`
// stanza that can change under the app between deploys is exactly what that
// requirement exists to prevent.
//
// The importer has already parsed the file. Resolving the build into the spec
// here is the same work it does for ports, volumes and environment, and it is
// what makes the result deployable by the builder that exists.
func (c *composeImport) build() spec.Build {
	// The service that builds. A compose file whose services all carry an
	// `image:` needs no builder at all.
	var building string
	for _, name := range c.names() {
		if c.file.Services[name].Build != nil && building == "" {
			building = name
		}
	}

	if building == "" {
		// Nothing to build: every service names an image that already exists.
		return spec.Build{Strategy: spec.BuildPrebuilt, ComposeFile: c.source}
	}

	// A compose app is built as a compose app. Which services build, and from
	// where, is recorded per workload — see Workload.Build — because a file with
	// two buildable services needs two images and one Build block cannot say so.
	return spec.Build{Strategy: spec.BuildCompose, ComposeFile: c.source}
}

// buildFields reads compose's two spellings of `build:`.
//
// A string is the context directory. A map carries context, dockerfile and
// target separately. Anything else is treated as absent rather than guessed at.
func buildFields(raw any) (context, dockerfile, target string) {
	switch b := raw.(type) {
	case string:
		return b, "", ""
	case map[string]any:
		context, _ = b["context"].(string)
		dockerfile, _ = b["dockerfile"].(string)
		target, _ = b["target"].(string)
		return context, dockerfile, target
	}
	return "", "", ""
}

// --- import -----------------------------------------------------------------

func (c *composeImport) draft() Draft {
	names := c.names()

	// The services Pando supplies rather than runs.
	//
	// `db: image: postgres:16` is the author saying this app needs a
	// PostgreSQL beside it. That is a dependency, and a dependency is a slot
	// (R-131) — so Pando provisions one, manages its storage and backs it up
	// with the app, and the service itself is not imported as a workload.
	//
	// Importing both produced two databases: the compose container, and the
	// one Pando provisioned for the slot it had also created. The app connected
	// to the first with whatever password the compose file interpolated from a
	// shell that does not exist here, which is how `DATABASE_URL is required`
	// became the whole of an app's logs.
	backing := map[string]spec.SlotType{}
	for _, name := range names {
		if slotType, ok := backingService(name, c.file.Services[name].Image); ok {
			backing[name] = slotType
		}
	}

	var running []string
	for _, name := range names {
		if _, provided := backing[name]; !provided {
			running = append(running, name)
		}
	}

	d := Draft{
		Build:    c.build(),
		Warnings: nil, // filled at the end, after every rewrite is known
	}

	for _, name := range running {
		d.Workloads = append(d.Workloads, c.workload(name, len(running) == 1))
	}

	d.Slots = c.wire(backing, d.Workloads)
	d.Volumes = c.volumes(running)

	// A service that is no longer a workload is no longer something to wait
	// for. Pando starts a provisioned service before the app either way.
	for i := range d.Workloads {
		var kept []string
		for _, on := range d.Workloads[i].DependsOn {
			if _, provided := backing[on]; !provided {
				kept = append(kept, on)
			}
		}
		d.Workloads[i].DependsOn = kept
	}

	d.Warnings = c.warnings
	return d
}

// wire turns each service Pando supplies into a slot, and points the variables
// that named it at that slot instead.
//
// The compose file already says which variable reaches the database: the app's
// own `DATABASE_URL: postgres://…@db:5432/…`. Pando fills that variable from
// the service it provisions, so the app is wired the way its author wired it
// and nobody types a connection string.
//
// The slot takes that variable's name, because a name somebody reading the app
// will recognize beats one Pando made up. A service nothing references keeps
// the generated `<SERVICE>_URL` — the dependency is real either way, and an app
// that reads it from somewhere Pando cannot see is still an app that needs a
// database.
func (c *composeImport) wire(backing map[string]spec.SlotType, workloads []spec.Workload) []spec.Slot {
	services := make([]string, 0, len(backing))
	for name := range backing {
		services = append(services, name)
	}
	sort.Strings(services)

	var slots []spec.Slot
	for _, service := range services {
		image := c.file.Services[service].Image
		key := strings.ToUpper(service) + "_URL"

		// The variables whose value points at this service.
		var filled []string
		for i := range workloads {
			for j := range workloads[i].Env {
				e := &workloads[i].Env[j]
				if e.Value == nil || !pointsAt(*e.Value, service) {
					continue
				}
				if len(filled) == 0 {
					key = e.Key
				}
				filled = append(filled, e.Key)
				ref := key
				e.Value = nil
				e.SlotRef = &ref
			}
		}

		evidence := []string{fmt.Sprintf("compose service %q runs %s", service, orUnknownImage(image))}
		if len(filled) > 0 {
			evidence = append(evidence, strings.Join(filled, ", ")+" pointed at it")
		}

		slots = append(slots, spec.Slot{
			Key:      key,
			Type:     backing[service],
			Required: true,
			Evidence: evidence,

			// Filled, because the compose file already answered it: it runs a
			// database in a container beside the app, and "Pando runs one
			// inside this app" is that same sentence in Pando's vocabulary.
			// Leaving it empty turned an app that worked under
			// `docker compose up` into one that was accepted and then refused
			// at deploy — asking a question whose answer was in the file being
			// imported.
			Resolution: &spec.Resolution{Mode: spec.ResolutionProvisioned},
		})

		c.rewrote(service, "service "+service,
			"Pando runs the "+backing[service].DisplayName()+" this app needs, rather than the "+
				"container the compose file describes: it manages the storage, backs it up with "+
				"the app, and fills "+key+" with the address. Bind it to a database you already "+
				"run instead on the app's dependencies.")
	}
	return slots
}

// isReverseProxy reports whether an image is one of the proxies people put in
// front of an app. Named images only — there is no guessing at what an
// unfamiliar image does.
func isReverseProxy(image string) bool {
	name := strings.ToLower(image)
	for _, known := range []string{"caddy", "nginx", "traefik", "haproxy", "envoyproxy/envoy", "httpd"} {
		if strings.Contains(name, known) {
			return true
		}
	}
	return false
}

// pointsAt reports whether a value names a compose service as a host.
//
// The shapes that matter are the ones a connection string takes:
// `postgres://user:pw@db:5432/app`, `redis://cache:6379`, and a value that is
// just the service name. Matching a bare substring would catch a password that
// happens to contain "db".
func pointsAt(value, service string) bool {
	if value == service {
		return true
	}
	for _, shape := range []string{"@" + service + ":", "@" + service + "/", "//" + service + ":", "//" + service + "/", "=" + service + ":"} {
		if strings.Contains(value, shape) {
			return true
		}
	}
	return strings.HasSuffix(value, "@"+service) || strings.HasSuffix(value, "//"+service)
}

func (c *composeImport) workload(name string, only bool) spec.Workload {
	s := c.file.Services[name]

	w := spec.Workload{
		Name:       name,
		Image:      s.Image,
		Command:    stringList(s.Command),
		Entrypoint: stringList(s.Entrypoint),
		WorkingDir: s.WorkingDir,
		Env:        c.env(name, s),
		Ports:      c.ports(name, s),
		Mounts:     c.mounts(name, s),
		DependsOn:  dependsOn(s.DependsOn),
		Health:     health(s.Healthcheck),

		// Which service is the app's one canonical endpoint is not something a
		// compose file says (R-026). With one service there is no question;
		// with several, the detector asks rather than picking.
		Primary: only,
		Exposed: only,
	}

	// A service that builds says so here rather than at the app level, so a
	// compose file with two of them produces two images instead of one and a
	// warning.
	if s.Build != nil {
		context, dockerfile, target := buildFields(s.Build)
		w.Build = &spec.WorkloadBuild{Context: context, Dockerfile: dockerfile, Target: target}
	}

	// Pando names containers itself, because two apps importing compose files
	// that both say `container_name: web` would otherwise collide install-wide.
	if s.ContainerName != "" {
		c.rewrote(name, "container_name: "+s.ContainerName,
			"Pando names containers itself so that two apps importing compose files "+
				"cannot collide over the same name.")
	}

	// Restart policy is configuration, not a question (R-104). Pando applies
	// its own, changeable in settings afterwards.
	if s.Restart != "" && s.Restart != "no" {
		c.rewrote(name, "restart: "+s.Restart,
			"Pando restarts apps according to its own policy, which you can change in settings.")
	}

	// cap_add is not rejected — a capability is narrower than privileged and
	// some are ordinary (NET_BIND_SERVICE). But it is not silently honored
	// either, because the runtime adapter decides what it grants.
	if len(s.CapAdd) > 0 {
		c.rewrote(name, "cap_add: "+strings.Join(s.CapAdd, ", "),
			"Added capabilities are not carried over. If the app needs one, the runtime's "+
				"isolation settings are where to grant it.")
	}

	return w
}

// ports imports the container port and drops the host port.
//
// A compose file publishing "8080:80" is saying two things: the service listens
// on 80, and it should be reachable on the host at 8080. The first is imported.
// The second is replaced by Pando's own routing — the app gets an address
// through the proxy (R-026), which is why nothing is lost by dropping it.
func (c *composeImport) ports(name string, s composeService) []spec.Port {
	var ports []spec.Port
	seen := map[int]bool{}

	add := func(n int, source spec.PortSource) {
		if n <= 0 || seen[n] {
			return
		}
		seen[n] = true
		ports = append(ports, spec.Port{Number: n, Protocol: "http", Source: source})
	}

	for _, raw := range s.Ports {
		text := scalar(raw)
		if text == "" {
			// The long form: { target: 80, published: 8080 }.
			if m, ok := raw.(map[string]any); ok {
				add(toInt(m["target"]), spec.PortCompose)
				if published := toInt(m["published"]); published != 0 {
					c.rewrotePublishedPort(name, fmt.Sprintf("%d:%d", published, toInt(m["target"])))
				}
			}
			continue
		}

		// "80", "8080:80", "127.0.0.1:8080:80", "8080:80/tcp"
		text = strings.SplitN(text, "/", 2)[0]
		parts := strings.Split(text, ":")
		container := parts[len(parts)-1]
		add(atoi(container), spec.PortCompose)
		if len(parts) > 1 {
			c.rewrotePublishedPort(name, scalar(raw))
		}
	}

	for _, raw := range s.Expose {
		add(atoi(scalar(raw)), spec.PortCompose)
	}
	return ports
}

func (c *composeImport) rewrotePublishedPort(service, mapping string) {
	c.rewrote(service, "ports: "+mapping,
		"Pando gives the app its own address and routes to it, so the app does not need "+
			"a port published on the host. The container's port is kept; the host's is not.")
}

// volumes imports named volumes. R-200: persistence declared in a compose file
// is imported and honored, and nothing special happens.
//
// Only what the imported workloads mount. A `pgdata` volume belonging to a
// compose database Pando now provisions itself is storage for a container that
// no longer exists — the provisioned service brings its own, and an app
// carrying an empty volume nothing writes to still has to answer for it at
// delete time (R-204).
func (c *composeImport) volumes(running []string) []spec.Volume {
	mounted := map[string]bool{}
	for _, service := range running {
		for _, m := range parseMounts(c.file.Services[service].Volumes) {
			if m.volumeName != "" {
				mounted[m.volumeName] = true
			}
		}
	}

	var named []string
	for name := range c.file.Volumes {
		if mounted[name] || !c.mountedAnywhere(name) {
			named = append(named, name)
		}
	}

	// Anonymous and relative-bind mounts also become volumes, so that data an
	// author meant to keep is kept. Collected across services first so a volume
	// used by two of them is declared once.
	for _, service := range running {
		for _, m := range parseMounts(c.file.Services[service].Volumes) {
			if m.volumeName != "" && !containsString(named, m.volumeName) {
				named = append(named, m.volumeName)
			}
		}
	}
	sort.Strings(named)

	volumes := make([]spec.Volume, 0, len(named))
	for _, name := range named {
		volumes = append(volumes, spec.Volume{
			ID: name, Name: name, Declared: spec.VolumeFromCompose,
		})
	}
	return volumes
}

// mountedAnywhere reports whether any service in the file mounts this volume.
// A declared volume nothing mounts is the author's, and is kept as it always
// was; one mounted only by a service Pando now provisions is not.
func (c *composeImport) mountedAnywhere(volume string) bool {
	for _, service := range c.names() {
		for _, m := range parseMounts(c.file.Services[service].Volumes) {
			if m.volumeName == volume {
				return true
			}
		}
	}
	return false
}

func (c *composeImport) mounts(name string, s composeService) []spec.Mount {
	var mounts []spec.Mount
	for _, m := range parseMounts(s.Volumes) {
		if m.volumeName == "" {
			continue
		}
		mounts = append(mounts, spec.Mount{
			VolumeID: m.volumeName, Path: m.containerPath, ReadOnly: m.readOnly,
		})

		// A relative bind mount is the author saying "this directory matters",
		// without saying whether it holds data or source. Pando keeps the data
		// reading, because that is the one where being wrong is silent: a
		// source mount that turns up empty fails loudly on the trial run, while
		// discarded data looks like a healthy deploy until the second one
		// (R-203).
		if m.hostPath != "" {
			c.rewrote(name, "volume "+m.raw,
				"The host directory "+m.hostPath+" became a volume Pando manages, mounted at "+
					m.containerPath+". Pando cannot mount a path from the host machine, and "+
					"discarding the data instead is the failure that looks like success.")
		}
	}
	return mounts
}

// env imports a service's environment, resolving compose substitutions the
// same way mount sources already were.
//
// `APP_DOMAIN: ${APP_DOMAIN:-localhost}` used to arrive at the container
// verbatim, as the fourteen characters "${APP_DOMAIN:-localhost}" — a value no
// app can use and nothing explains, from a file whose own semantics say an
// unset variable takes its default.
//
// A substitution with no default has no value to resolve to, so it keeps its
// spelling and says so: somebody has to supply it on the app's settings, and a
// variable that silently became empty would be an app misconfigured with no
// sign of it (R-102).
func (c *composeImport) env(name string, s composeService) []spec.EnvEntry {
	var entries []spec.EnvEntry

	add := func(key, raw string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		value := interpolate(raw)
		if strings.Contains(value, "${") {
			c.unresolved(name, key, value)
		}
		entries = append(entries, spec.EnvEntry{Key: key, Value: &value, Source: spec.EnvFromCompose})
	}

	switch v := s.Environment.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			add(k, scalar(v[k]))
		}
	case []any:
		for _, raw := range v {
			k, v, found := strings.Cut(scalar(raw), "=")
			if !found {
				continue
			}
			add(k, v)
		}
	}
	return entries
}

// unresolved warns about a variable whose value the compose file does not hold.
func (c *composeImport) unresolved(service, key, value string) {
	c.warnings = append(c.warnings, spec.Warning{
		Code: spec.WarnComposeConstructRewritten,
		Message: fmt.Sprintf("In the compose service %q, %s is set to %s, which takes its value "+
			"from the shell running `docker compose`. Pando has no such shell. Set %s on this "+
			"app's variables, or it reaches the app as written.",
			service, key, value, key),
	})
}

func (c *composeImport) rewrote(service, construct, why string) {
	c.warnings = append(c.warnings, spec.Warning{
		Code: spec.WarnComposeConstructRewritten,
		Message: fmt.Sprintf("In the compose service %q, `%s` was not carried over as written. %s",
			service, construct, why),
	})
}

// --- compose value shapes ---------------------------------------------------

// mount is one entry in a service's `volumes:` list.
type mount struct {
	raw           string
	volumeName    string // named volume, or a name Pando made for a bind mount
	hostPath      string // non-empty when the entry was a bind mount
	containerPath string
	readOnly      bool
}

// parseMounts reads the short and long forms of a compose volume entry.
func parseMounts(entries []any) []mount {
	var mounts []mount

	for _, entry := range entries {
		if text := scalar(entry); text != "" {
			mounts = append(mounts, parseShortMount(text))
			continue
		}
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		source, target := interpolate(scalar(m["source"])), scalar(m["target"])
		out := mount{
			raw:           source + ":" + target,
			containerPath: target,
			readOnly:      m["read_only"] == true,
		}
		if scalar(m["type"]) == "bind" || strings.HasPrefix(source, ".") || isAbsoluteHostPath(source) {
			out.hostPath = source
			out.volumeName = volumeNameFor(source, target)
		} else {
			out.volumeName = source
		}
		mounts = append(mounts, out)
	}
	return mounts
}

func parseShortMount(text string) mount {
	parts := splitMountSpec(interpolate(text))
	switch len(parts) {
	case 1:
		// An anonymous volume: "/var/lib/data".
		return mount{raw: text, containerPath: parts[0], volumeName: volumeNameFor("", parts[0])}
	default:
		m := mount{raw: text, containerPath: parts[1]}
		if len(parts) > 2 {
			m.readOnly = parts[2] == "ro"
		}
		if strings.HasPrefix(parts[0], ".") || isAbsoluteHostPath(parts[0]) || strings.HasPrefix(parts[0], "~") {
			m.hostPath = parts[0]
			m.volumeName = volumeNameFor(parts[0], parts[1])
		} else {
			m.volumeName = parts[0]
		}
		return m
	}
}

// splitMountSpec splits a compose volume entry on ":", ignoring colons inside
// a ${...} substitution.
//
// "${CONFIG_DIR:-./config}:/app/config:ro" has five colons and three fields.
// Splitting naively produces a volume named "${CONFIG_DIR" mounted at
// "-./config}", which is not a parse error anywhere — it is a bundle that comes
// up with a garbage volume attached to a nonsense path, and an app that cannot
// find its configuration for reasons nothing explains.
func splitMountSpec(text string) []string {
	var parts []string
	var current strings.Builder
	depth := 0

	for i := 0; i < len(text); i++ {
		switch {
		case text[i] == '$' && i+1 < len(text) && text[i+1] == '{':
			depth++
			current.WriteByte(text[i])
		case text[i] == '}' && depth > 0:
			depth--
			current.WriteByte(text[i])
		case text[i] == ':' && depth == 0:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteByte(text[i])
		}
	}
	parts = append(parts, current.String())
	return parts
}

// interpolate resolves compose variable substitutions to their defaults.
//
// Pando has no environment to interpolate from — the compose file is being read
// out of a repository, not run from a shell — so the default is the only value
// available, and compose's own semantics say that is what an unset variable
// takes. "${CONFIG_DIR:-./config}" becomes "./config", which then travels the
// ordinary relative-bind path and becomes a managed volume.
//
// A variable with no default is left as written. It is not a path, so it fails
// the host-path checks and ends up named as-is rather than silently becoming an
// empty string — which would mount the repository root.
func interpolate(text string) string {
	var out strings.Builder

	for i := 0; i < len(text); {
		if text[i] != '$' || i+1 >= len(text) || text[i+1] != '{' {
			out.WriteByte(text[i])
			i++
			continue
		}

		end := strings.IndexByte(text[i:], '}')
		if end < 0 {
			out.WriteString(text[i:])
			break
		}
		inner := text[i+2 : i+end]
		out.WriteString(defaultOf(inner))
		i += end + 1
	}
	return out.String()
}

// defaultOf returns the default from a compose substitution body.
//
// The forms with a usable default are "VAR:-default" and "VAR-default". The
// error forms — "VAR:?message" and "VAR?message" — have no default by
// definition, and neither does a bare "VAR"; all three keep their original
// spelling so that what Pando could not resolve stays visible.
func defaultOf(inner string) string {
	if name, fallback, found := strings.Cut(inner, ":-"); found && name != "" {
		return fallback
	}
	if name, fallback, found := strings.Cut(inner, "-"); found && name != "" && !strings.Contains(name, ":") {
		return fallback
	}
	return "${" + inner + "}"
}

// isAbsoluteHostPath reports whether a mount source names a path on the host.
//
// A leading "." is relative to the compose file and handled separately: it is
// rewritten into a managed volume rather than refused, because it is usually
// the app's own data directory. An absolute path is refused — there is nothing
// to rewrite it to, and it reaches outside the app's storage entirely.
func isAbsoluteHostPath(source string) bool {
	return strings.HasPrefix(source, "/") || strings.HasPrefix(source, "~")
}

// volumeNameFor derives a stable volume name for a mount that had none.
func volumeNameFor(hostPath, containerPath string) string {
	base := path.Base(strings.TrimSuffix(containerPath, "/"))
	if base == "" || base == "." || base == "/" {
		base = path.Base(strings.TrimSuffix(hostPath, "/"))
	}
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, base)
	name = strings.Trim(name, "-")
	if name == "" {
		return "data"
	}
	return name
}

func dependsOn(raw any) []string {
	var names []string
	switch v := raw.(type) {
	case []any:
		for _, entry := range v {
			if name := scalar(entry); name != "" {
				names = append(names, name)
			}
		}
	case map[string]any:
		// The long form: { db: { condition: service_healthy } }. The condition
		// is the healthcheck's job; the ordering is what is imported here.
		for name := range v {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	return names
}

func health(h *composeHealth) *spec.Healthcheck {
	if h == nil {
		return nil
	}
	command := stringList(h.Test)

	// Compose's test is ["CMD", "curl", ...] or ["CMD-SHELL", "..."]. The first
	// element says how to run the rest, not what to run.
	if len(command) > 0 && (command[0] == "CMD" || command[0] == "CMD-SHELL" || command[0] == "NONE") {
		if command[0] == "NONE" {
			return nil
		}
		command = command[1:]
	}
	if len(command) == 0 {
		return nil
	}

	return &spec.Healthcheck{
		Command:         command,
		IntervalSeconds: int(seconds(h.Interval)),
		TimeoutSeconds:  int(seconds(h.Timeout)),
		Retries:         h.Retries,
	}
}

// seconds reads a compose duration: "30s", "1m30s", "500ms".
func seconds(text string) float64 {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}

	var total float64
	var number strings.Builder
	for _, r := range text {
		if (r >= '0' && r <= '9') || r == '.' {
			number.WriteRune(r)
			continue
		}
		n, _ := strconv.ParseFloat(number.String(), 64)
		number.Reset()
		switch r {
		case 'h':
			total += n * 3600
		case 'm':
			total += n * 60
		case 's':
			total += n
		}
	}
	return total
}

// scalar renders a YAML scalar as a string, and anything else as "".
func scalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return ""
	}
}

// stringList reads compose's "either a string or a list" shape.
func stringList(v any) []string {
	switch t := v.(type) {
	case string:
		return strings.Fields(t)
	case []any:
		var out []string
		for _, entry := range t {
			if s := scalar(entry); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func toInt(v any) int { return atoi(scalar(v)) }

func atoi(text string) int {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0
	}
	return n
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
