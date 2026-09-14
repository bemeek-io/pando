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
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
	backuplocal "github.com/bemeek-io/pando/internal/adapter/backup/local"
	buildkitadapter "github.com/bemeek-io/pando/internal/adapter/builder/buildkit"
	"github.com/bemeek-io/pando/internal/adapter/identity/local"
	notifyconsole "github.com/bemeek-io/pando/internal/adapter/notify/console"
	"github.com/bemeek-io/pando/internal/adapter/registry/ociprobe"
	"github.com/bemeek-io/pando/internal/adapter/routing/loopback"
	"github.com/bemeek-io/pando/internal/adapter/routing/traefik"
	dockerruntime "github.com/bemeek-io/pando/internal/adapter/runtime/docker"
	secretslocal "github.com/bemeek-io/pando/internal/adapter/secrets/local"
	servicesdocker "github.com/bemeek-io/pando/internal/adapter/services/docker"
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

	root.AddCommand(serveCmd(&configPath), migrateCmd(&configPath), adminCmd(&configPath), versionCmd())

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
			out := cmd.OutOrStdout()
			if buildVersion == "dev" {
				fmt.Fprintln(out, "pando (development build)")
				return
			}
			fmt.Fprintf(out, "pando %s\n", buildVersion)
			if buildCommit != "" {
				fmt.Fprintf(out, "commit %s\n", buildCommit)
			}
			if buildDate != "" {
				fmt.Fprintf(out, "built %s\n", buildDate)
			}
		},
	}
}

// buildPlanner returns the configured builder if it can make a build plan.
//
// A capability discovered by type assertion, which R-254 forbids for anything
// the planner decides on — and this is not that. Whether a build plan can be
// shown before it runs changes nothing about whether the app can be built: the
// builder plans at build time regardless. It changes only whether detection has
// something to show, which is why a missing planner degrades to the behavior
// every install had before and not to a plan-time refusal.
func buildPlanner(registry *adapterapi.Registry) detect.BuildPlanner {
	ref, ok := registry.Default(adapterapi.CategoryBuilder)
	if !ok {
		return nil
	}
	builder, ok := registry.Builder(ref)
	if !ok {
		return nil
	}
	planner, ok := builder.(detect.BuildPlanner)
	if !ok {
		return nil
	}
	return planner
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
	first, err := bootstrap.Run(ctx, users, grants, db, auditor,
		secret.New(cfg.Bootstrap.AdminPassword))
	if err != nil {
		return err
	}
	switch {
	case first.Created && first.Supplied:
		// No password field. The operator supplied it and already has it;
		// printing it would copy a credential into a log for nobody's benefit
		// (R-194).
		logger.Warn("first run: created an administrator account",
			zap.String("username", bootstrap.AdminUsername),
			zap.String("note", "using the password from PANDO_ADMIN_PASSWORD; it must still be changed on first login"))

	case first.Created:
		logger.Warn("first run: created an administrator account",
			zap.String("username", bootstrap.AdminUsername),
			zap.String("password", first.Password.Reveal()),
			zap.String("note", "this is shown once and must be changed on first login"))

	case cfg.Bootstrap.AdminPassword != "":
		// Said out loud, because the alternative is an operator who set it,
		// cannot sign in with it, and has no reason to suspect it was never
		// read.
		logger.Info("PANDO_ADMIN_PASSWORD was set and ignored",
			zap.String("reason", "this installation already has accounts"),
			zap.String("remedy", "run `pando admin reset-password` to set one"))
	}

	// Adapter registration happens here, in main, from compiled-in packages
	// (R-253). There is no plugin protocol and none is planned.
	identity := local.New(users)

	notifications := state.NewNotifications(db)

	registry, err := registerAdapters(ctx, adapters, notifications, logger)
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
	bundleSource := state.NewBundleSource(db)
	backupService := &backup.Service{
		Registry:    registry,
		DatabaseURL: secret.New(cfg.Database.URL),

		// The local secrets adapter's key. Without it in the bundle a restored
		// install holds every app's ciphertext and nothing that opens it
		// (R-212) — which looks like a successful restore until an app starts.
		// Read from the adapter's own configuration rather than duplicated into
		// server config, so there is one place that decides where the key lives.
		SecretsKeyPath: secretsKeyPath(ctx, adapters, logger),

		State:         bundleSource,
		Version:       buildVersion,
		SchemaVersion: db.SchemaVersion(),
		WorkDir:       cfg.Server.WorkDir,
	}

	deployments := state.NewDeployments(db)
	logStore := deploy.NewLogStore()
	appPlanner := planner.New(registry, hostPolicy, allocations).WithInventory(apps)

	// Every route points here (R-023). The proxy is phase 5; until it exists
	// this is the address routing adapters are told to use, and it is already
	// Pando's own rather than any workload's.
	proxyUpstream := cfg.Server.ProxyUpstream
	if proxyUpstream == "" {
		proxyUpstream = "http://pando:8080"
	}
	reconciles := state.NewReconciles(db)
	deployer := deploy.NewRunner(registry, appPlanner, apps, deployments, secrets, reconciles, logStore, volumes, proxyUpstream).
		WithServices(state.NewServices(db), secrets)

	// Detection (Sequence A). Every detector bids; the runtime supplies the
	// trial run (R-097), and a registry probe would supply R-094's top tier.
	// Both are optional here, and missing either degrades to a question rather
	// than to a failure.
	// The install's defaults: its adapters, its routing shape, and the
	// retention caps R-211 and R-223 set. Shared between detection and the
	// spec endpoint, because a hand-written spec needs them just as much — and
	// used to get none of them.
	installDefaults := detection.NewInstallation(registry, cfg.Server.BaseDomain)

	detections := state.NewDetections(db)
	detector := &detection.Runner{
		Apps:       apps,
		Detections: detections,
		Policy:     hostPolicy,

		// R-104: everything a repository cannot say about itself is
		// configuration, not a question. Without this a detected spec describes
		// the app and says nothing about where it runs, which is a spec the
		// planner refuses.
		Install: installDefaults,

		Ports:          state.NewPorts(db),
		PortRangeStart: cfg.Server.PortRangeStart,
		PortRangeEnd:   cfg.Server.PortRangeEnd,
		Job: &detect.Job{
			Auction: detect.NewAuction(
				detect.DockerfileDetector{},
				detect.ComposeDetector{},
				detect.StaticDetector{},
				// The planner comes from the builder: core asks "how would you
				// build this" and stores the answer without interpreting it
				// (R-251). Nil on an install with no builder configured, where
				// detection still recognizes the language and asks its
				// questions — it just cannot show the plan.
				detect.BuildpackDetector{Planner: buildPlanner(registry)},
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

	// One resolver, used by the proxy to route and by the router to tell an
	// app's hostname from Pando's own.
	appResolver := proxy.NewStateResolver(apps)

	// The single enforcement point for every request to every app (R-023).

	appProxy := &proxy.Proxy{
		Resolver:      appResolver,
		Authenticator: authenticator,
		Authz:         authorizer,
		Minter:        minter,
		Upstreams:     proxy.NewRuntimeUpstreams(registry),
		Auditor:       auditor,
		Metrics:       proxy.NewCounters(),
		Logger:        logger,
		LoginPath:     httpapi.LoginPath,
		Mode:          cfg.Server.RoutingMode,
	}

	// Built once and used twice: as the front door, and as what a port-mode
	// app's own listener falls back to for Pando's reserved path (R-172).
	apiHandler := (&httpapi.Server{
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
		Authz:    authorizer,
		Authent:  authenticator,
		Minter:   minter,
		AppProxy: appProxy,

		// So the console does not answer on an app's own hostname. Without
		// this the console's "/" route shadows every subdomain app's root.
		AppHosts:   appResolver,
		Grants:     grants,
		HostPolicy: hostPolicy,
		Verbs:      authzStore,
		Defaults:   installDefaults,

		// The policy *document* and the policy *evaluator* are different
		// things and both are wired: one endpoint edits the document, every
		// authorization check consults the evaluator, and the evaluator
		// reads the document per evaluation rather than caching it (R-274).
		PolicyStore: policyStore,
		AuditLog:    audit.NewReader(db.Pool),

		Groups:       state.NewGroups(db),
		Roles:        state.NewRoles(db),
		Backups:      backups,
		Backup:       backupService,
		BundleSource: bundleSource,

		// Retried deploys replay rather than repeat (R-262). An agent
		// retries on a timeout, and a deploy that clones regularly outlasts
		// a client's patience.
		Idempotency: state.NewIdempotency(db),
	}).Routes()

	srv := &http.Server{
		Addr:    cfg.Server.Addr,
		Handler: apiHandler,

		// A client that opens a connection and dribbles header bytes holds a
		// goroutine open indefinitely without this. Matches what the proxy's
		// per-app listeners already do (internal/proxy/ports.go).
		//
		// ReadHeaderTimeout and IdleTimeout only. A ReadTimeout or WriteTimeout
		// would cut the responses this API exists to stream — the deploy log's
		// SSE feed, `pando logs --follow`, and the exec websocket — at a fixed
		// wall-clock deadline, which is the wrong tool for a slow client.
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// The reconciler. Apps that kept running while Pando was away are converged
	// to, not restarted for tidiness — an app that was running and is still
	// running needs nothing done to it (design 05 §2.1.1).
	backoffSchedule, err := cfg.Reconciler.BackoffSchedule()
	if err != nil {
		return err
	}
	warnIfRetriesAreFast(logger, backoffSchedule)

	loop := &reconciler.Reconciler{
		Apps:          apps,
		Reconciles:    reconciles,
		Secrets:       secrets,
		Volumes:       volumes,
		Services:      deployer,
		Registry:      registryAdapters{registry},
		Auditor:       reconcilerAuditor{auditor},
		Logger:        logger,
		Clock:         clock.System{},
		ProxyUpstream: proxyUpstream,

		// Unset in production: the zero values mean R-149 and R-150's defaults.
		Backoff:          backoffSchedule,
		FailureThreshold: cfg.Reconciler.FailureThreshold,
		FailureWindow:    cfg.Reconciler.FailureWindow,
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

	// Port-mode apps answer at the root of their own port (design 03 §4.2).
	//
	// The allocation was already being made and shown to people; nothing
	// listened on it, so every laptop install advertised an address that
	// refused the connection and the apps were reachable only under the path
	// prefix — where an app that writes "/assets/app.js" into its own HTML
	// comes up blank, and Pando will not rewrite the page to hide that
	// (R-167, R-028). Same proxy, same enforcement, one extra way in (R-023).
	go (&proxy.PortListeners{
		Ports: state.NewPorts(db),
		// Not the bare proxy: a port listener is a front door of its own, and
		// somebody arriving at it unauthenticated has to have somewhere to
		// sign in (R-172). Everything else on that socket is the app's.
		Handler: httpapi.ReservedOrApp(apiHandler, appProxy),
		Logger:  logger,
	}).Run(loopCtx)

	// Hourly garbage collection, on a much slower clock because nothing here is
	// urgent and all of it is destructive.
	// Reclaim the networks of apps deleted since the last run.
	//
	// At startup and only at startup. A bundle network still held by the
	// *previous* Pando container has a dead endpoint on it, so Docker removes
	// it with nothing to disconnect — whereas doing this while serving would
	// mean detaching the running container, and on Docker Desktop that drops
	// its published ports. Measured, not assumed.
	if ref, ok := registry.Default(adapterapi.CategoryRuntime); ok {
		rt, _ := registry.Runtime(ref)
		if reclaimer, ok := rt.(interface {
			ReclaimNetworks(context.Context) (int, error)
		}); ok {
			if n, err := reclaimer.ReclaimNetworks(ctx); err != nil {
				logger.Warn("could not reclaim app networks", zap.Error(err))
			} else if n > 0 {
				logger.Info("reclaimed app networks left by deleted apps", zap.Int("count", n))
			}
		}

		// Then rejoin what is left, which is the other half of the same
		// problem: this process is running in a *new* container, and the
		// networks of every already-running app were joined by the old one.
		// Nothing redeploys those apps, so without this they stay up and
		// unreachable — a Pando upgrade would 502 every app on the install
		// until each was deployed again by hand. After reclaim, never before:
		// reclaim recognizes a dead app's network by its being empty.
		if rejoiner, ok := rt.(interface {
			RejoinNetworks(context.Context) (int, error)
		}); ok {
			if n, err := rejoiner.RejoinNetworks(ctx); err != nil {
				logger.Warn("could not rejoin app networks", zap.Error(err))
			} else if n > 0 {
				logger.Info("rejoined the networks of running apps", zap.Int("count", n))
			}
		}
	}

	// The GC also tears down the bundles of deleted apps, which nothing used to
	// do: Destroy was never called, so every deleted app left containers and a
	// private network behind. Registry and Auditor are what make that possible
	// — and the teardown is audited, because destruction is destruction whoever
	// does it.
	go (&reconciler.GC{
		Apps:     apps,
		Logger:   logger,
		Registry: registryAdapters{registry},
		Auditor:  reconcilerAuditor{auditor},
		Interval: cfg.Reconciler.GCInterval,
		Clock:    clock.System{},

		// R-211's rolling backups, which had a column, a default and an expiry
		// query and nothing that ever took one.
		Backups:      backups,
		Backup:       backupService,
		BundleSource: bundleSource,
	}).Run(loopCtx)

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
func registerAdapters(ctx context.Context, store *state.Adapters, notifications *state.Notifications, logger *zap.Logger) (*adapterapi.Registry, error) {
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
		case c.Category == string(adapterapi.CategoryServices) && c.Kind == servicesdocker.Kind:
			adapter = servicesdocker.New()
		case c.Category == string(adapterapi.CategoryRouting) && c.Kind == traefik.Kind:
			adapter = traefik.New()
		case c.Category == string(adapterapi.CategoryNotify) && c.Kind == notifyconsole.Kind:
			// The sink is supplied by core. The adapter stores nothing itself,
			// which is R-027 — an adapter never touches state.
			adapter = notifyconsole.New(notifications)
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

	// Seeded per category, not once per install.
	//
	// The original rule was "if any adapter exists, do nothing", which is right
	// on the first run and wrong on every upgrade: an adapter category added in
	// a later version would never be seeded on an install that already had
	// others, so the feature would ship and silently not exist. That is exactly
	// what happened to notifications.
	//
	// The tradeoff is that deleting the *last* adapter in a category brings the
	// built-in default back on the next start. That is the better failure: a
	// category with nothing in it does nothing, and an install with no
	// notification adapter is not a considered posture — it is a gap.
	filled := map[string]bool{}
	for _, c := range existing {
		filled[c.Category] = true
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

		// Provisioned slots, the hobbyist default in R-131. A Postgres, MySQL
		// or Redis stood up inside the app's own bundle, reachable from
		// nowhere else (R-134).
		{ID: "svcs_docker", Category: string(adapterapi.CategoryServices), Kind: servicesdocker.Kind,
			Name: "Inside the app", IsDefault: true, Enabled: true},

		// Console-only notifications (R-231). Nothing is sent anywhere; a
		// message waits in Pando for the next time the recipient looks.
		{ID: "ntf_console", Category: string(adapterapi.CategoryNotify), Kind: notifyconsole.Kind,
			Name: "In the console", IsDefault: true, Enabled: true},
	} {
		if filled[c.Category] {
			continue
		}
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

// buildVersion is what the manifest records. Stamped by the release build
// (.goreleaser.yaml); "dev" in every other build, which is honest rather than a
// version number nobody set.
//
// Variables rather than constants because -ldflags -X cannot write to a const.
var (
	buildVersion = "dev"
	buildCommit  = ""
	buildDate    = ""
)

// warnIfRetriesAreFast says so when the retry schedule is configured faster than
// R-149's default.
//
// Not refused, because a floor would make the schedule untestable end to end
// and that is the whole reason it is configurable. But an install retrying a
// broken app every couple of seconds forever is a real way to melt a host, and
// nobody should be able to do that without being told — especially since the
// setting exists for tests and is exactly the kind of thing that gets copied
// out of a test compose file into a real one.
func warnIfRetriesAreFast(logger *zap.Logger, schedule []time.Duration) {
	if len(schedule) == 0 {
		return
	}
	cap := schedule[len(schedule)-1]
	if cap >= reconciler.MinProductionCap {
		return
	}
	logger.Warn("retry backoff is configured faster than the shipped default",
		zap.Duration("cap", cap),
		zap.Duration("default_cap", reconciler.DefaultBackoff[len(reconciler.DefaultBackoff)-1]),
		zap.String("note", "this is a testing setting (R-149). An app that cannot start will be retried this often, forever."))
}

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
