// Package deploy runs the deployment pipeline: everything after the plan
// boundary (design 05 §3, steps 8-16).
//
// Steps 1-7 belong to the planner and create nothing. Everything here has side
// effects, which is why the split exists: a plan-time failure means nothing was
// touched, and a failure in this package means something was.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/planner"
	"github.com/bemeek-io/pando/internal/core/source"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/log"
	"github.com/bemeek-io/pando/internal/secret"
)

// Secrets resolves an app's stored secrets into values.
type Secrets interface {
	// Resolve returns an app's secret values by key. Called once per deploy, at
	// step 11 — after the plan boundary, because reading a secret is a side
	// effect and an audited one.
	Resolve(ctx context.Context, appID string) (map[string]secret.Value, error)

	// Versions returns each secret's version without decrypting anything. It
	// feeds the environment fingerprint (R-193), which is compared on every
	// reconciliation and must never be a reason to read a value.
	Versions(ctx context.Context, appID string) (map[string]int, error)
}

// Reconciles records what an app was last applied with.
type Reconciles interface {
	SetAppliedEnvFingerprint(ctx context.Context, appID, fingerprint string) error
}

// Runner executes deployments.
type Runner struct {
	registry *api.Registry
	planner  *planner.Planner
	apps     *state.Apps
	deploys  *state.Deployments
	secrets  Secrets
	logs     *LogStore

	// reconciles records the environment fingerprint a deploy applied, which is
	// the only thing that makes a rotated secret visible later (R-193).
	reconciles Reconciles

	// volumes records the storage a deploy created, so Pando knows about the
	// data it is responsible for.
	volumes *state.Volumes

	// ProxyUpstream is where routing adapters must send traffic (R-023). It is
	// Pando's proxy, always, and it is passed to every Ensure so that no adapter
	// has to work it out.
	ProxyUpstream string
}

// PrepareRevision pins the commit a deploy will build.
//
// Resolving a ref is an explicit act that produces a revision, never something a
// deploy does on its own (design 01 §2.1, R-120). A revision that already names
// a commit is returned unchanged, so redeploying it builds the same code even
// after the branch has moved — which is the whole point of pinning.
func (r *Runner) PrepareRevision(ctx context.Context, rev state.Revision, by string) (state.Revision, error) {
	s := rev.Body
	if s.Source.Type != spec.SourceGit || s.Source.Commit != "" {
		return rev, nil
	}

	checkout, err := source.Fetch(ctx, s.Source)
	if err != nil {
		return state.Revision{}, err
	}
	defer checkout.Close()

	pinned := *s
	pinned.Source.Commit = checkout.Commit

	// A new revision, because spec_revisions is append-only and because the
	// pinned commit is a real change to how this app runs — one worth being
	// visible in the app's history rather than applied silently.
	return r.apps.CreateRevision(ctx, rev.AppID, &pinned, spec.OriginEdited, by)
}

func NewRunner(registry *api.Registry, p *planner.Planner, apps *state.Apps, deploys *state.Deployments, secrets Secrets, reconciles Reconciles, logs *LogStore, volumes *state.Volumes, proxyUpstream string) *Runner {
	return &Runner{
		registry: registry, planner: p, apps: apps, deploys: deploys,
		secrets: secrets, reconciles: reconciles, logs: logs, volumes: volumes,
		ProxyUpstream: proxyUpstream,
	}
}

// Run executes a deployment to completion.
//
// The app's state moves to deploying at the start and to running or degraded at
// the end. A build failure is the exception: it leaves the app's state
// untouched, because nothing about the running app changed (R-146). The
// deployment is failed; the app is not.
func (r *Runner) Run(ctx context.Context, dep state.Deployment, rev state.Revision) error {
	l := log.From(ctx).With(zap.String("deployment_id", dep.ID), zap.String("app_id", dep.AppID))
	sink := r.logs.Writer(dep.ID)
	defer sink.Close()

	fail := func(step string, err error) error {
		code := string(errs.CodeOf(err))
		message := "The deploy failed."
		if e := errs.As(err); e != nil {
			message = e.Message
		}
		fmt.Fprintf(sink, "\n!! %s failed: %s\n", step, message)
		l.Warn("deployment failed", zap.String("step", step), zap.Error(err))
		_ = r.deploys.Finish(ctx, dep.ID, state.DeployFailed, code, message)
		return err
	}

	appSpec := rev.Body

	// A deploy never resolves a ref implicitly at runtime (design 01 §2.1).
	// The commit is pinned by PrepareRevision before the deployment exists, so
	// reaching here without one means the caller skipped that step — and a
	// deploy that silently built whatever the branch points at now would be
	// unreproducible in exactly the way R-120 exists to prevent.
	//
	// An upload is the exception, and it does not weaken the rule. There is no
	// revision to resolve: the archive on the server IS the pinned thing, and
	// it cannot change under the deploy because the next upload writes a new
	// one. Demanding a commit here would mean inventing one, which is the
	// failure R-120 actually names.
	if needsBuild(appSpec) && appSpec.Source.Commit == "" && appSpec.Source.Type != spec.SourceUpload {
		return fail("fetch", errs.New(errs.StateInvalid,
			"This app's spec does not say which commit to build.").
			WithRemedy("Pin a commit for this revision before deploying it."))
	}

	// Step 8: fetch the pinned commit.
	if appSpec.Source.Type == spec.SourceUpload {
		fmt.Fprintln(sink, "=> Using the uploaded source")
	} else {
		fmt.Fprintf(sink, "=> Fetching source at %s\n", short(appSpec.Source.Commit))
	}
	checkout, err := source.Fetch(ctx, appSpec.Source)
	if err != nil {
		return fail("fetch", err)
	}
	defer checkout.Close()

	// Step 9: build. A failure here leaves the running app alone (R-146).
	image := appSpec.Source.Image
	if needsBuild(appSpec) {
		if err := r.deploys.SetStatus(ctx, dep.ID, state.DeployBuilding); err != nil {
			return fail("build", err)
		}
		built, err := r.build(ctx, appSpec, checkout, sink)
		if err != nil {
			// The reason goes into the log the user is watching, not only into
			// the server's own log. A build that fails without saying why is
			// the failure mode R-105 exists to prevent.
			fmt.Fprintf(sink, "\n!! %s\n", messageOf(err))
			if detail := detailOf(err); detail != "" {
				fmt.Fprintf(sink, "   %s\n", detail)
			}
			fmt.Fprintf(sink, "   The running version of this app was not touched.\n")
			_ = r.deploys.Finish(ctx, dep.ID, state.DeployFailed, string(errs.CodeOf(err)), messageOf(err))
			l.Warn("build failed; app state unchanged (R-146)", zap.Error(err))
			return err
		}
		image = built
	}

	if err := r.deploys.SetStatus(ctx, dep.ID, state.DeployApplying); err != nil {
		return fail("apply", err)
	}

	// Step 11: resolve secrets and materialize the environment.
	//
	// This is where slot and secret references become values, and it is the
	// last moment before the plan leaves core. Adapters receive the result
	// fully resolved and never learn which entries were sensitive.
	fmt.Fprintf(sink, "=> Preparing configuration\n")
	secrets, err := r.secrets.Resolve(ctx, dep.AppID)
	if err != nil {
		return fail("secrets", err)
	}

	bundle, err := r.bundlePlan(appSpec, image, secrets)
	if err != nil {
		return fail("apply", err)
	}

	runtime, ok := r.registry.Runtime(appSpec.Runtime.AdapterRef)
	if !ok {
		return fail("apply", errs.Newf(errs.PlanAdapterNotConfigured,
			"The runtime %q is not configured.", appSpec.Runtime.AdapterRef))
	}

	// Steps 12-13: volumes, then apply. Recreate is the default (R-144) and the
	// app is down during the swap, which is the accepted cost.
	fmt.Fprintf(sink, "=> Starting the app\n")
	if _, err := runtime.Apply(ctx, bundle); err != nil {
		return fail("apply", err)
	}

	// Record the storage Pando now owns.
	//
	// Not bookkeeping. Until this existed the `volumes` table stayed empty
	// however many volumes an app declared, and three things quietly did
	// nothing: R-204's ON DELETE RESTRICT guarded no rows, deleting an app
	// never offered to keep its data because it counted none, and a DR bundle
	// contained the database and no app data whatsoever.
	//
	// The handles come from Observe rather than being assembled here, because
	// how a volume is named is the provider vocabulary core must never learn
	// (R-251).
	if r.volumes != nil && len(bundle.Volumes) > 0 {
		if err := r.recordVolumes(ctx, runtime, appSpec, bundle); err != nil {
			// Not fatal to the deploy: the app is running and refusing to say
			// so would be worse. But it is logged loudly, because an app whose
			// storage Pando does not know about is an app whose storage will
			// not be backed up.
			fmt.Fprintf(sink, "!! Could not record this app's storage: %v\n", err)
		}
	}

	// Step 14: route. Traffic goes to PANDO'S PROXY, never to the workload.
	fmt.Fprintf(sink, "=> Routing traffic\n")
	routing, ok := r.registry.Routing(appSpec.Routing.AdapterRef)
	if !ok {
		return fail("route", errs.Newf(errs.PlanAdapterNotConfigured,
			"The routing option %q is not configured.", appSpec.Routing.AdapterRef))
	}
	if _, err := routing.Ensure(ctx, api.RouteRequest{
		AppID:      dep.AppID,
		Mode:       appSpec.Routing.Mode,
		Hostname:   appSpec.Routing.Hostname,
		PathPrefix: appSpec.Routing.PathPrefix,
		Port:       appSpec.Routing.Port,

		// R-023 has no exceptions. Every route points here.
		ProxyUpstream: r.ProxyUpstream,

		TLS: api.TLSRequest{Enabled: appSpec.Routing.Mode != spec.RoutingPort, Hostname: appSpec.Routing.Hostname},
	}); err != nil {
		return fail("route", err)
	}

	// Step 15: wait for health.
	fmt.Fprintf(sink, "=> Waiting for the app to be ready\n")
	healthy, err := r.waitForHealth(ctx, runtime, dep.AppID, bundle)
	if err != nil {
		return fail("health", err)
	}

	// Step 16: commit.
	newState := state.StateRunning
	if !healthy {
		// Degraded, not failed: the workloads are up and the health signal has
		// not passed yet. Auto-rollback happens only if opted in (R-147).
		newState = state.StateDegraded
		fmt.Fprintf(sink, "\n!! The app started but has not reported healthy yet.\n")
	}

	if err := r.apps.Pin(ctx, dep.AppID, rev.ID, newState, dep.CreatedBy); err != nil {
		return fail("commit", err)
	}
	if err := r.apps.SetDesiredState(ctx, dep.AppID, "running"); err != nil {
		return fail("commit", err)
	}

	// What ran, and what it ran with. Both exist for the reconciler: it
	// restores a missing workload from the recorded image rather than
	// rebuilding, and it detects a rotated secret (R-193) by comparing this
	// fingerprint, because Observe returns no environment and never will.
	if err := r.deploys.SetImageRef(ctx, dep.ID, image, primaryDigest(ctx, runtime, dep.AppID)); err != nil {
		return fail("commit", err)
	}
	if r.reconciles != nil {
		versions, err := r.secrets.Versions(ctx, dep.AppID)
		if err != nil {
			return fail("commit", err)
		}
		if err := r.reconciles.SetAppliedEnvFingerprint(ctx, dep.AppID,
			EnvFingerprint(rev.Body, versions)); err != nil {
			return fail("commit", err)
		}
	}
	if err := r.deploys.Finish(ctx, dep.ID, state.DeploySucceeded, "", ""); err != nil {
		return err
	}

	fmt.Fprintf(sink, "\n== Deployed. The app is %s.\n", newState)
	l.Info("deployment succeeded", zap.String("state", newState))
	return nil
}

// build runs the builder and hands the image to the runtime.
//
// The image streams from one adapter to the other through a pipe: it is never
// held in memory or staged on disk, and neither adapter learns anything about
// the other.
func (r *Runner) build(ctx context.Context, s *spec.AppSpec, checkout *source.Checkout, sink io.Writer) (string, error) {
	builder, ok := r.registry.Builder(s.Build.AdapterRef)
	if !ok {
		return "", errs.Newf(errs.PlanAdapterNotConfigured,
			"The builder %q is not configured.", s.Build.AdapterRef)
	}
	runtime, ok := r.registry.Runtime(s.Runtime.AdapterRef)
	if !ok {
		return "", errs.Newf(errs.PlanAdapterNotConfigured,
			"The runtime %q is not configured.", s.Runtime.AdapterRef)
	}

	caps, err := runtime.Capabilities(ctx)
	if err != nil {
		return "", err
	}
	if !caps.SupportsImageImport {
		return "", errs.Newf(errs.PlanCapabilityUnsupported,
			"%q cannot take an image built here.", s.Runtime.AdapterRef).
			WithRemedy("Use a runtime that can import a built image, or point this app at a prebuilt image.")
	}

	args := map[string]string{}
	for _, kv := range s.Build.Args {
		args[kv.Key] = kv.Value
	}

	pr, pw := io.Pipe()

	var (
		wg         sync.WaitGroup
		importedID string
		importErr  error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		importedID, importErr = runtime.ImportImage(ctx, pr)
		// Draining matters: if the import fails early, the builder would block
		// writing into a pipe nobody reads.
		_, _ = io.Copy(io.Discard, pr)
	}()

	fmt.Fprintf(sink, "=> Building\n")
	result, buildErr := builder.Build(ctx, api.BuildRequest{
		Source:     checkout.View(s.Source.Subdir),
		Strategy:   s.Build.Strategy,
		Dockerfile: s.Build.Dockerfile,
		Context:    s.Build.Context,
		Args:       args,

		IsolationFloor: s.Build.IsolationFloor,
		Timeout:        time.Duration(s.Build.TimeoutSeconds) * time.Second,
		EgressMode:     s.Build.EgressMode,
		EgressAllow:    s.Build.EgressAllow,

		// R-117: per-app cache namespace, so one app's build cannot read
		// layers produced by another's.
		CacheNamespace: s.AppID,

		LogSink:   sink,
		ImageSink: pw,
	})
	_ = pw.CloseWithError(buildErr)
	wg.Wait()

	if buildErr != nil {
		return "", buildErr
	}
	if importErr != nil {
		return "", importErr
	}
	if importedID != "" {
		return importedID, nil
	}
	return result.ImageRef, nil
}

// bundlePlan resolves the spec into what the runtime is asked to apply.
// BundlePlanShape builds the bundle plan without resolving any environment.
//
// The reconciler compares what should be running against what is, on every tick
// for every app. That comparison needs the shape — workloads, images, ports,
// mounts, volumes — and must never be a reason to decrypt a secret, so this
// stops short of environment and R-193's fingerprint covers the rest.
func BundlePlanShape(s *spec.AppSpec, image string) (api.BundlePlan, error) {
	return bundlePlanFor(s, image, nil, false)
}

func (r *Runner) bundlePlan(s *spec.AppSpec, image string, secrets map[string]secret.Value) (api.BundlePlan, error) {
	return bundlePlanFor(s, image, secrets, true)
}

func bundlePlanFor(s *spec.AppSpec, image string, secrets map[string]secret.Value, withEnv bool) (api.BundlePlan, error) {
	plan := api.BundlePlan{
		BundleID: s.AppID,
		Network: api.NetworkPlan{
			Private:     true, // R-026, always.
			EgressMode:  s.Egress.Mode,
			EgressAllow: s.Egress.Allowlist,
		},
		Labels: map[string]string{"pando.app": s.AppID},
	}

	for _, v := range s.Volumes {
		plan.Volumes = append(plan.Volumes, api.VolumePlan{VolumeID: v.ID, Name: v.Name})
	}

	for _, w := range s.Workloads {
		var env map[string]secret.Value
		if withEnv {
			resolved, err := resolveEnv(s, w, secrets)
			if err != nil {
				return api.BundlePlan{}, err
			}
			env = resolved
		}

		wp := api.WorkloadPlan{
			Name: w.Name,
			// R-222: every workload is capped, so a chatty app cannot fill a
			// disk shared with twenty others.
			LogBytes:   s.Retention.LogBytes,
			Image:      firstNonEmpty(w.Image, image),
			Command:    w.Command,
			Entrypoint: w.Entrypoint,
			WorkingDir: w.WorkingDir,
			Env:        env,
			DependsOn:  w.DependsOn,
			Exposed:    w.Exposed,
			Resources: api.ResourcePlan{
				CPUMillis:   s.Resources.CPUMillis,
				MemoryBytes: s.Resources.MemoryBytes,
			},
		}
		if w.Resources != nil {
			wp.Resources = api.ResourcePlan{CPUMillis: w.Resources.CPUMillis, MemoryBytes: w.Resources.MemoryBytes}
		}
		for _, m := range w.Mounts {
			wp.Mounts = append(wp.Mounts, api.MountPlan{VolumeID: m.VolumeID, Path: m.Path, ReadOnly: m.ReadOnly})
		}
		for _, port := range w.Ports {
			wp.Ports = append(wp.Ports, api.PortPlan{Number: port.Number, Protocol: port.Protocol})
		}
		if h := healthPlan(s, w); h != nil {
			wp.Health = h
		}
		plan.Workloads = append(plan.Workloads, wp)
	}
	return plan, nil
}

// resolveEnv turns spec references into values.
//
// A reference that cannot be resolved is an error rather than an empty string:
// starting an app with a blank database password because a secret was missing is
// the kind of failure that looks like it worked.
func resolveEnv(s *spec.AppSpec, w spec.Workload, secrets map[string]secret.Value) (map[string]secret.Value, error) {
	env := make(map[string]secret.Value, len(w.Env))

	for _, e := range w.Env {
		switch {
		case e.Value != nil:
			env[e.Key] = secret.New(*e.Value)

		case e.SecretRef != nil:
			v, ok := secrets[*e.SecretRef]
			if !ok {
				return nil, errs.Newf(errs.StateInvalid,
					"%s needs a stored secret that this app does not have.", e.Key).
					WithDetail("secret_key", *e.SecretRef).
					WithRemedy("Set the value through the app's secrets, then deploy again.")
			}
			env[e.Key] = v

		case e.SlotRef != nil:
			slot, ok := s.Slot(*e.SlotRef)
			if !ok || slot.Resolution == nil {
				return nil, errs.Newf(errs.PlanSlotUnfilled,
					"%s comes from a slot that has not been filled.", e.Key).
					WithDetail("slot_key", *e.SlotRef)
			}
			switch slot.Resolution.Mode {
			case spec.ResolutionBound:
				env[e.Key] = secret.New(slot.Resolution.Target)
			case spec.ResolutionLiteral:
				v, ok := secrets[slot.Resolution.SecretRef]
				if !ok {
					return nil, errs.Newf(errs.StateInvalid,
						"%s comes from a stored value this app does not have.", e.Key).
						WithDetail("slot_key", slot.Key)
				}
				env[e.Key] = v
			default:
				return nil, errs.Newf(errs.PlanSlotUnfilled,
					"%s comes from a service Pando has not provisioned yet.", e.Key).
					WithDetail("slot_key", slot.Key)
			}
		}
	}
	return env, nil
}

// healthPlan applies the source precedence in R-221.
func healthPlan(s *spec.AppSpec, w spec.Workload) *api.HealthPlan {
	if w.Health != nil {
		return &api.HealthPlan{
			Command: w.Health.Command, Path: w.Health.Path, Port: w.Health.Port,
			IntervalSeconds: orDefault(w.Health.IntervalSeconds, 30),
			TimeoutSeconds:  orDefault(w.Health.TimeoutSeconds, 5),
			Retries:         orDefault(w.Health.Retries, 3),
		}
	}
	if s.Health.Source == "" || (s.Health.Path == "" && s.Health.Port == 0) {
		// No health signal configured. Not an error and not unhealthy: an app
		// with no health check is running, not perpetually degraded (R-221).
		return nil
	}
	if !w.Primary {
		return nil
	}
	return &api.HealthPlan{
		Path: s.Health.Path, Port: s.Health.Port,
		IntervalSeconds: orDefault(s.Health.IntervalSeconds, 30),
		TimeoutSeconds:  orDefault(s.Health.TimeoutSeconds, 5),
		Retries:         orDefault(s.Health.Retries, 3),
	}
}

// waitForHealth polls until every workload is running and, where a signal
// exists, healthy.
//
// Returns false rather than an error when health does not pass: the app is
// degraded, which is recoverable and still being worked, not failed.
func (r *Runner) waitForHealth(ctx context.Context, runtime api.RuntimeAdapter, appID string, bundle api.BundlePlan) (bool, error) {
	deadline := time.Now().Add(2 * time.Minute)

	for {
		observed, err := runtime.Observe(ctx, api.BundleRef{BundleID: appID})
		if err != nil {
			return false, err
		}

		allRunning := len(observed.Workloads) >= len(bundle.Workloads)
		healthy := true
		for _, w := range observed.Workloads {
			if !w.Running {
				allRunning = false
			}
			// nil means no signal, which is not unhealthy (R-221).
			if w.Healthy != nil && !*w.Healthy {
				healthy = false
			}
		}

		if allRunning && healthy {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}

		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func needsBuild(s *spec.AppSpec) bool {
	if s.Build.Strategy == spec.BuildPrebuilt {
		return false
	}
	return s.Source.Type != spec.SourceImage
}

// detailOf returns the underlying cause for the build log.
//
// Build failures are the one place an internal cause is worth showing: the
// caller is looking at their own build, and "the build failed" with nothing
// further is unactionable.
func detailOf(err error) string {
	e := errs.As(err)
	if e == nil {
		return err.Error()
	}
	if cause := errors.Unwrap(e); cause != nil {
		return cause.Error()
	}
	return ""
}

func messageOf(err error) string {
	if e := errs.As(err); e != nil {
		return e.Message
	}
	return "The deploy failed."
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func orDefault(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func short(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}

// primaryDigest asks the runtime what the primary workload actually resolved to.
//
// The runtime's own observation rather than anything Pando computed: a tag can
// move, a build can produce a digest nobody predicted, and the only thing worth
// comparing against a running container later is what was running now. Empty on
// any failure, which makes image drift undetectable rather than making every
// workload look wrong.
func primaryDigest(ctx context.Context, runtime api.RuntimeAdapter, appID string) string {
	observed, err := runtime.Observe(ctx, api.BundleRef{BundleID: appID})
	if err != nil {
		return ""
	}
	for _, w := range observed.Workloads {
		if w.Running && w.ImageDigest != "" {
			return w.ImageDigest
		}
	}
	return ""
}

// recordVolumes writes the volume rows for a deploy, reading handles back from
// the runtime.
//
// Observe rather than Apply's return value, because Apply reports a bundle
// handle and not the volumes inside it — and asking the runtime what exists is
// the honest question anyway. An adapter that created a volume under a name of
// its own choosing is answered here rather than guessed at.
func (r *Runner) recordVolumes(ctx context.Context, runtime api.RuntimeAdapter, s *spec.AppSpec, bundle api.BundlePlan) error {
	observed, err := runtime.Observe(ctx, api.BundleRef{BundleID: bundle.BundleID})
	if err != nil {
		return err
	}

	handles := make(map[string]string, len(observed.Volumes))
	for _, v := range observed.Volumes {
		handles[v.VolumeID] = v.Handle
	}

	records := make([]state.VolumeRecord, 0, len(s.Volumes))
	for _, v := range s.Volumes {
		records = append(records, state.VolumeRecord{
			VolumeID: v.ID, Name: v.Name, Handle: handles[v.ID],
		})
	}
	return r.volumes.RecordFromRuntime(ctx, s.AppID, s.Runtime.AdapterRef, records)
}
