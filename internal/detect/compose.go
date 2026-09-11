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
//	rewritten — imported with different mechanism and the same behaviour, and
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

	imp := &composeImport{file: file, source: name}
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
	file     composeFile
	source   string
	warnings []spec.Warning
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
			if mount.hostPath == "" || !isAbsoluteHostPath(mount.hostPath) {
				continue
			}
			found = append(found, rejection{name, "volume " + mount.raw,
				"This mounts a path from the host machine into the app. Pando has no way to " +
					"honor it: the app may not run on the machine holding that path, and if it " +
					"did, the mount would reach outside the app's own storage."})
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

	return errs.Newf(errs.PlanComposeConstructRejected,
		"%s uses %d construct(s) that cannot run inside the boundary Pando puts around an app: %s.",
		c.source, len(found), strings.Join(summary, "; ")).
		WithDetail("rejected", details).
		WithRemedy("Each entry above says why. Remove or change those lines in " + c.source +
			", or deploy without the compose file by supplying an image and a command instead.")
}

// --- import -----------------------------------------------------------------

func (c *composeImport) draft() Draft {
	names := c.names()

	d := Draft{
		Build:    spec.Build{Strategy: spec.BuildCompose, ComposeFile: c.source},
		Volumes:  c.volumes(),
		Warnings: nil, // filled at the end, after every rewrite is known
	}

	for _, name := range names {
		d.Workloads = append(d.Workloads, c.workload(name, len(names) == 1))
	}
	d.Slots = c.slots()
	d.Warnings = c.warnings
	return d
}

func (c *composeImport) workload(name string, only bool) spec.Workload {
	s := c.file.Services[name]

	w := spec.Workload{
		Name:       name,
		Image:      s.Image,
		Command:    stringList(s.Command),
		Entrypoint: stringList(s.Entrypoint),
		WorkingDir: s.WorkingDir,
		Env:        c.env(s),
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
func (c *composeImport) volumes() []spec.Volume {
	var named []string
	for name := range c.file.Volumes {
		named = append(named, name)
	}

	// Anonymous and relative-bind mounts also become volumes, so that data an
	// author meant to keep is kept. Collected across services first so a volume
	// used by two of them is declared once.
	for _, service := range c.names() {
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

func (c *composeImport) env(s composeService) []spec.EnvEntry {
	var entries []spec.EnvEntry

	switch v := s.Environment.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			value := scalar(v[k])
			entries = append(entries, spec.EnvEntry{Key: k, Value: &value})
		}
	case []any:
		for _, raw := range v {
			k, v, found := strings.Cut(scalar(raw), "=")
			if !found {
				continue
			}
			value := v
			entries = append(entries, spec.EnvEntry{Key: strings.TrimSpace(k), Value: &value})
		}
	}
	return entries
}

// slots turns recognizable backing services into slots (R-131).
//
// A compose service running postgres is a dependency the app declares, which is
// what makes it fillable — with the ad-hoc container the compose file describes,
// or with a real database the user binds instead (R-100).
func (c *composeImport) slots() []spec.Slot {
	images := map[string]string{}
	for _, name := range c.names() {
		images[name] = c.file.Services[name].Image
	}
	return slotsFromComposeServices(c.names(), images)
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
		source, target := scalar(m["source"]), scalar(m["target"])
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
	parts := strings.Split(text, ":")
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
