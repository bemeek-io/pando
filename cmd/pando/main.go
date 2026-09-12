// Command pando is both the CLI and the server entrypoint (design 00 §2).
//
// Adapter registration happens here, in main, from compiled-in packages (R-253).
// Everything ships as one binary: server, CLI, and the embedded console.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
	backuplocal "github.com/bemeek-io/pando/internal/adapter/backup/local"
	buildkitadapter "github.com/bemeek-io/pando/internal/adapter/builder/buildkit"
	"github.com/bemeek-io/pando/internal/adapter/identity/local"
	"github.com/bemeek-io/pando/internal/adapter/registry/ociprobe"
	"github.com/bemeek-io/pando/internal/adapter/routing/loopback"
	dockerruntime "github.com/bemeek-io/pando/internal/adapter/runtime/docker"
	secretslocal "github.com/bemeek-io/pando/internal/adapter/secrets/local"
	"github.com/bemeek-io/pando/internal/cli"
	"github.com/bemeek-io/pando/internal/config"
	"github.com/bemeek-io/pando/internal/console"
	"github.com/bemeek-io/pando/internal/core/assertion"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/backup"
	"github.com/bemeek-io/pando/internal/core/bootstrap"
	"github.com/bemeek-io/pando/internal/core/clock"
	"github.com/bemeek-io/pando/internal/core/deploy"
	"github.com/bemeek-io/pando/internal/core/detection"
	"github.com/bemeek-io/pando/internal/core/planner"
	corepolicy "github.com/bemeek-io/pando/internal/core/policy"
	"github.com/bemeek-io/pando/internal/core/reconciler"
	"github.com/bemeek-io/pando/internal/core/source"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/httpapi"
	"github.com/bemeek-io/pando/internal/log"
	"github.com/bemeek-io/pando/internal/proxy"
	"github.com/bemeek-io/pando/internal/secret"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		// Cobra has already printed the error.
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var configPath string

	root := &cobra.Command{
		Use:           "pando",
		Short:         "Host your apps without setting up a deployment pipeline",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to a config file")

	root.AddCommand(serveCmd(&configPath), migrateCmd(&configPath), versionCmd())

	// The client half (design 04 §4). In the same binary because Pando ships as
	// one, and a client of the API like any other (R-261) — internal/cli
	// imports no core package, so a command that needed something the API
	// cannot do would not compile rather than quietly growing a shortcut.
	root.AddCommand(cli.Commands()...)
	return root
}

func serveCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the Pando server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serve(cmd.Context(), *configPath)
		},
	}
}

func migrateCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations and exit",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, logger, err := setup(*configPath)
			if err != nil {
				return err
			}
			defer func() { _ = logger.Sync() }()

			ctx := log.Into(cmd.Context(), logger)
			return state.Migrate(ctx, cfg.Database.URL)
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "pando (development build)")
		},
	}
}

func setup(configPath string) (*config.Config, *zap.Logger, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}
	logger, err := log.New(cfg.Log.Level, cfg.Log.Development)
	if err != nil {
		return nil, nil, err
	}
	return cfg, logger, nil
}

func serve(ctx context.Context, configPath string) error {
	cfg, logger, err := setup(configPath)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	ctx = log.Into(ctx, logger)

	// Interrupts cancel the context, which unwinds the connect retry as well as
	// the server. Ctrl-C during a sixty-second wait for Postgres should exit,
	// not be ignored until the wait expires.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Connect runs the whole bootstrap: wait for Postgres, migrate as the owner,
	// provision the restricted application role, apply grants, and verify that
	// the audit log cannot be rewritten. It refuses to return a usable database
	// if that last check fails (R-027).
	db, err := state.Connect(ctx, state.ConnectOptions{
		OwnerURL:       cfg.Database.URL,
		ConnectTimeout: cfg.Database.ConnectTimeout,
	})
	if err != nil {
		return err
	}
	defer db.Close()

	auditor := audit.New(db.Pool)

	// A background job is as visible as a person (design 06 §1).
	if err := auditor.Write(ctx, audit.Event{
		PrincipalKind: audit.KindSystem,
		PrincipalID:   "system",
		Action:        "server.start",
	}); err != nil {
		return err
	}

	users := state.NewUsers(db)
	sessions := state.NewSessions(db)
	tokens := state.NewTokens(db)
	apps := state.NewApps(db)
	volumes := state.NewVolumes(db)
	adapters := state.NewAdapters(db)
	allocations := state.NewAllocations(db)
	authzStore := state.NewAuthzStore(db)
	grants := state.NewGrants(db)

	// Host policy is loaded per evaluation, not cached: R-274 says policy
	// applies to a running install, and a cached document would keep allowing
	// or keep denying for as long as the cache lived.
	policyStore := state.NewPolicy(db)
	hostPolicy := corepolicy.New(policyStore.Load)

	// R-046: the first run creates one administrative account and shows its
	// password once. It is never stored in the clear, so an operator who misses
	// it resets rather than retrieves.
	first, err := bootstrap.Run(ctx, users, grants, db, auditor)
	if err != nil {
		return err
	}
	if first.Created {
		logger.Warn("first run: created an administrator account",
			zap.String("username", bootstrap.AdminUsername),
			zap.String("password", first.Password.Reveal()),
			zap.String("note", "this is shown once and must be changed on first login"))
	}

	// Adapter registration happens here, in main, from compiled-in packages
	// (R-253). There is no plugin protocol and none is planned.
	identity := local.New(users)

	registry, err := registerAdapters(ctx, adapters, logger)
	if err != nil {
		return err
	}

	// Secrets go through the configured secrets adapter. Core never encrypts —
	// it stores what the adapter hands back (R-190, R-191).
	secretsRef, _ := registry.Default(adapterapi.CategorySecrets)
	secretsAdapter, _ := registry.Secrets(secretsRef)
	secrets := state.NewSecrets(db, secretsAdapter, secretsRef)

	// Backup and disaster recovery (Sequence D). The service does the work; the
	// store records what it produced, and the record outlives the thing it
	// records (R-204).
	backups := state.NewBackups(db)
	backupService := &backup.Service{
		Registry:    registry,
		DatabaseURL: secret.New(cfg.Database.URL),

		// The local secrets adapter's key. Without it in the bundle a restored
		// install holds every app's ciphertext and nothing that opens it
		// (R-212) — which looks like a successful restore until an app starts.
		// Read from the adapter's own configuration rather than duplicated into
		// server config, so there is one place that decides where the key lives.
		SecretsKeyPath: secretsKeyPath(ctx, adapters, logger),

		State:         state.NewBundleSource(db),
		Version:       buildVersion,
		SchemaVersion: db.SchemaVersion(),
		WorkDir:       cfg.Server.WorkDir,
	}

	deployments := state.NewDeployments(db)
	logStore := deploy.NewLogStore()
	appPlanner := planner.New(registry, hostPolicy, allocations)

	// Every route points here (R-023). The proxy is phase 5; until it exists
	// this is the address routing adapters are told to use, and it is already
	// Pando's own rather than any workload's.
	proxyUpstream := cfg.Server.ProxyUpstream
	if proxyUpstream == "" {
		proxyUpstream = "http://pando:8080"
	}
	reconciles := state.NewReconciles(db)
	deployer := deploy.NewRunner(registry, appPlanner, apps, deployments, secrets, reconciles, logStore, proxyUpstream)

	// Detection (Sequence A). Every detector bids; the runtime supplies the
	// trial run (R-097), and a registry probe would supply R-094's top tier.
	// Both are optional here, and missing either degrades to a question rather
	// than to a failure.
	detections := state.NewDetections(db)
	detector := &detection.Runner{
		Apps:       apps,
		Detections: detections,
		Policy:     hostPolicy,

		// R-104: everything a repository cannot say about itself is
		// configuration, not a question. Without this a detected spec describes
		// the app and says nothing about where it runs, which is a spec the
		// planner refuses.
		Install: detection.NewInstallation(registry, cfg.Server.BaseDomain),

		Ports:          state.NewPorts(db),
		PortRangeStart: cfg.Server.PortRangeStart,
		PortRangeEnd:   cfg.Server.PortRangeEnd,
		Job: &detect.Job{
			Auction: detect.NewAuction(
				detect.DockerfileDetector{},
				detect.ComposeDetector{},
				detect.StaticDetector{},
				detect.BuildpackDetector{},
				detect.MonorepoDetector{},
			),
			Runtime: runtimeForTrial(registry),

			// R-094 tier 1. ghcr.io only by default — a Docker Hub username has
			// no relationship to the GitHub owner of the same name, so a match
			// there is not evidence that the image belongs to the project
			// (docs/design/notes-registry-tier-namespaces.md).
			Registry: ociprobe.New(),
		},
	}

	// Assertions are what an app can actually trust about a caller (R-051).
	// The signing key is generated per process for now; persisting it across
	// restarts is phase 9's concern, since the DR bundle carries it (R-212).
	minter, err := assertion.NewMinter(cfg.Server.Issuer, clock.System{})
	if err != nil {
		return err
	}

	authenticator := &httpapi.Authenticator{
		Sessions: sessions,
		Tokens:   tokens,
		Users:    users,
		Groups:   authzStore,
	}
	authorizer := authz.New(authzStore, hostPolicy, auditDenials{auditor})

	// The single enforcement point for every request to every app (R-023).
	appProxy := &proxy.Proxy{
		Resolver:      proxy.NewStateResolver(apps),
		Authenticator: authenticator,
		Authz:         authorizer,
		Minter:        minter,
		Upstreams:     proxy.NewRuntimeUpstreams(registry),
		Auditor:       auditor,
		Metrics:       proxy.NewCounters(),
		Logger:        logger,
		LoginPath:     "/login",
		Mode:          cfg.Server.RoutingMode,
	}

	srv := &http.Server{
		Addr: cfg.Server.Addr,
		Handler: (&httpapi.Server{
			Logger:   logger,
			DB:       db,
			Identity: identity,
			Users:    users,
			Sessions: sessions,
			Tokens:   tokens,
			Apps:     apps,
			Volumes:  volumes,
			Auditor:  auditor,
			Policy:   hostPolicy,

			Registry:    registry,
			Adapters:    adapters,
			Allocations: allocations,
			Planner:     appPlanner,
			Deployments: deployments,
			Deployer:    deployer,
			Logs:        logStore,
			Secrets:     secrets,
			Detections:  detections,
			Detector:    detector,
			Console:     consoleHandler(logger),

			// Policy is evaluated before grants, so it is wired into the
			// authorizer rather than checked alongside it (R-272).
			Authz:      authorizer,
			Authent:    authenticator,
			Minter:     minter,
			AppProxy:   appProxy,
			Grants:     grants,
			HostPolicy: hostPolicy,
			Verbs:      authzStore,

			// The policy *document* and the policy *evaluator* are different
			// things and both are wired: one endpoint edits the document, every
			// authorization check consults the evaluator, and the evaluator
			// reads the document per evaluation rather than caching it (R-274).
			PolicyStore: policyStore,
			AuditLog:    audit.NewReader(db.Pool),

			Backups: backups,
			Backup:  backupService,
		}).Routes(),
	}

	// The reconciler. Apps that kept running while Pando was away are converged
	// to, not restarted for tidiness — an app that was running and is still
	// running needs nothing done to it (design 05 §2.1.1).
	loop := &reconciler.Reconciler{
		Apps:          apps,
		Reconciles:    reconciles,
		Secrets:       secrets,
		Volumes:       volumes,
		Registry:      registryAdapters{registry},
		Auditor:       reconcilerAuditor{auditor},
		Logger:        logger,
		Clock:         clock.System{},
		ProxyUpstream: proxyUpstream,
	}
	loopCtx, stopLoop := context.WithCancel(ctx)
	defer stopLoop()
	go loop.Run(loopCtx)

	// Auto-deploy is a separate job on its own clock (R-141). It never modifies
	// a running app — it creates a revision and enqueues a deployment, and
	// everything flows through the normal path from there. Off unless an app's
	// pinned spec asks for it, so this is usually a query returning nothing.
	go (&reconciler.AutoDeploy{
		Apps:        apps,
		Deployments: deployments,
		Resolver:    refResolver{},
		Enqueue: func(ctx context.Context, dep state.Deployment, rev state.Revision) {
			go deployer.Run(context.WithoutCancel(ctx), dep, rev) //nolint:errcheck // recorded on the deployment
		},
		Logger: logger,
	}).Run(loopCtx)

	// Hourly garbage collection, on a much slower clock because nothing here is
	// urgent and all of it is destructive.
	go (&reconciler.GC{Apps: apps, Logger: logger}).Run(loopCtx)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", zap.String("addr", cfg.Server.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Server.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// registerAdapters configures the compiled-in adapters from adapter_configs,
// seeding the defaults on a fresh install.
//
// An adapter that fails to configure is logged and skipped rather than
// preventing startup: one broken adapter should not take the whole install
// offline, and the planner already refuses to plan against an adapter it cannot
// reach (R-254).
func registerAdapters(ctx context.Context, store *state.Adapters, logger *zap.Logger) (*adapterapi.Registry, error) {
	if err := seedDefaultAdapters(ctx, store); err != nil {
		return nil, err
	}

	configured, err := store.List(ctx)
	if err != nil {
		return nil, err
	}

	registry := adapterapi.NewRegistry()
	for _, c := range configured {
		if !c.Enabled {
			continue
		}

		var adapter adapterapi.Adapter
		switch {
		case c.Category == string(adapterapi.CategoryRuntime) && c.Kind == dockerruntime.Kind:
			adapter = dockerruntime.New()
		case c.Category == string(adapterapi.CategoryRouting) && c.Kind == loopback.Kind:
			adapter = loopback.New()
		case c.Category == string(adapterapi.CategorySecrets) && c.Kind == secretslocal.Kind:
			adapter = secretslocal.New()
		case c.Category == string(adapterapi.CategoryBuilder) && c.Kind == buildkitadapter.Kind:
			adapter = buildkitadapter.New()
		case c.Category == string(adapterapi.CategoryBackup) && c.Kind == backuplocal.Kind:
			adapter = backuplocal.New()
		default:
			logger.Warn("skipping adapter of unknown kind",
				zap.String("id", c.ID), zap.String("category", c.Category), zap.String("kind", c.Kind))
			continue
		}

		if err := adapter.Configure(ctx, c.Config); err != nil {
			logger.Error("adapter could not be configured and was skipped",
				zap.String("id", c.ID), zap.Error(err))
			continue
		}
		if err := registry.Register(c.ID, adapter); err != nil {
			return nil, err
		}
		if c.IsDefault {
			if err := registry.SetDefault(adapterapi.Category(c.Category), c.ID); err != nil {
				return nil, err
			}
		}
		logger.Info("adapter registered", zap.String("id", c.ID), zap.String("kind", c.Kind))
	}
	return registry, nil
}

// seedDefaultAdapters gives a fresh install a working set: Docker to run
// things, loopback to reach them, local storage for secrets. R-002 — setup cost
// is paid once, and this is part of not charging it twice.
func seedDefaultAdapters(ctx context.Context, store *state.Adapters) error {
	existing, err := store.List(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}

	for _, c := range []state.AdapterConfig{
		{ID: "rt_docker", Category: string(adapterapi.CategoryRuntime), Kind: dockerruntime.Kind,
			Name: "Docker", IsDefault: true, Enabled: true},
		{ID: "rte_loopback", Category: string(adapterapi.CategoryRouting), Kind: loopback.Kind,
			Name: "Localhost", IsDefault: true, Enabled: true},
		{ID: "sek_local", Category: string(adapterapi.CategorySecrets), Kind: secretslocal.Kind,
			Name: "Local storage", IsDefault: true, Enabled: true},
		{ID: "bld_buildkit", Category: string(adapterapi.CategoryBuilder), Kind: buildkitadapter.Kind,
			Name: "BuildKit", IsDefault: true, Enabled: true},

		// A local destination on a fresh install, so the DR path works out of
		// the box. R-217 is explicit that a bundle beside the install it backs
		// up does not survive the disk failing — this is the default that makes
		// backups testable, not the one an operator should keep.
		{ID: "bkp_local", Category: string(adapterapi.CategoryBackup), Kind: backuplocal.Kind,
			Name: "Local disk", IsDefault: true, Enabled: true},
	} {
		if err := store.Upsert(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// secretsKeyPath reads where the local secrets adapter keeps its key.
//
// From adapter_configs rather than server config: the adapter owns that choice,
// and a second copy in another file is a second copy to get wrong. An install
// using an external secrets adapter has no local key, and the empty string is
// the correct answer there — the bundle then carries no key because there is
// none to carry.
func secretsKeyPath(ctx context.Context, store *state.Adapters, logger *zap.Logger) string {
	configured, err := store.List(ctx)
	if err != nil {
		logger.Warn("could not read adapter configuration for backups", zap.Error(err))
		return ""
	}
	for _, c := range configured {
		if c.Category != string(adapterapi.CategorySecrets) || c.Kind != secretslocal.Kind {
			continue
		}
		var cfg struct {
			KeyPath string `json:"key_path"`
		}
		if len(c.Config) > 0 {
			_ = json.Unmarshal(c.Config, &cfg)
		}
		if cfg.KeyPath != "" {
			return cfg.KeyPath
		}
		return secretslocal.DefaultKeyPath
	}
	return ""
}

// buildVersion is what the manifest records. Stamped at build time once there
// is a release process; "dev" until then, which is honest rather than a version
// number nobody set.
const buildVersion = "dev"

// auditDenials writes an audit event for every authorization denial.
//
// Every denial, not only successes: a denial pattern is the signal that matters
// for detecting misuse, and it is the thing most commonly left out.
type auditDenials struct{ writer *audit.Writer }

func (a auditDenials) Denied(ctx context.Context, p authz.Principal, appID string, verb authz.Verb, code errs.Code) {
	_ = a.writer.Write(ctx, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        "authz.denied",
		AppID:         appID,
		Detail:        map[string]any{"verb": string(verb), "code": string(code)},
	})
}

// runtimeForTrial returns the configured runtime, for the trial run (R-097).
//
// Nil when none is configured, which detection treats as "no trial happened" —
// deferred questions become real ones and the proposal is still produced. An
// install with no runtime cannot deploy anything anyway; refusing to detect
// would just make the failure arrive earlier and less clearly.
func runtimeForTrial(registry *adapterapi.Registry) detect.TrialRunner {
	// The default runtime, because at detection time there is no spec yet to
	// name one. Nothing is guessed: Default is what the install was configured
	// with, and without one there is no trial.
	ref, ok := registry.Default(adapterapi.CategoryRuntime)
	if !ok {
		return nil
	}
	rt, ok := registry.Runtime(ref)
	if !ok {
		return nil
	}
	return rt
}

// registryAdapters adapts the adapter registry to what the reconciler needs.
//
// Two methods rather than the whole registry, so the reconciler cannot reach
// for anything else — it resolves the adapters an app's spec names and has no
// business with the rest.
type registryAdapters struct{ r *adapterapi.Registry }

func (a registryAdapters) Runtime(ref string) (adapterapi.RuntimeAdapter, bool) {
	return a.r.Runtime(ref)
}

func (a registryAdapters) Routing(ref string) (adapterapi.RoutingAdapter, bool) {
	return a.r.Routing(ref)
}

// reconcilerAuditor writes the reconciler's events to the audit log.
//
// The reconciler acts with no principal: nobody asked for a drift correction,
// which is the point of it. Recorded as a system action so that "who restarted
// this" has an answer, and the answer is Pando.
type reconcilerAuditor struct{ w *audit.Writer }

func (a reconcilerAuditor) Write(ctx context.Context, e reconciler.AuditEvent) error {
	return a.w.Write(ctx, audit.Event{
		PrincipalKind: audit.KindSystem,
		PrincipalID:   "reconciler",
		Action:        e.Action,
		AppID:         e.AppID,
		TargetKind:    "app",
		TargetID:      e.AppID,
		Detail:        e.Detail,
	})
}

// refResolver reads a remote's refs without cloning it.
type refResolver struct{}

func (refResolver) Resolve(ctx context.Context, src spec.Source) (string, error) {
	return source.ResolveRef(ctx, src)
}

// consoleHandler returns the embedded console, or nil when the binary was built
// without it.
//
// A developer running `go run ./cmd/pando` without `make console` gets an API
// and no UI, which is the honest outcome — better than a panic at startup or a
// blank page that looks like a broken console rather than an absent one.
func consoleHandler(logger *zap.Logger) http.Handler {
	handler, ok := console.Handler()
	if !ok {
		logger.Warn("console assets are not built into this binary; the UI will 404",
			zap.String("remedy", "run `make console`"))
		return nil
	}
	return handler
}
