//go:build integration

package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
	identitylocal "github.com/bemeek-io/pando/internal/adapter/identity/local"
	secretslocal "github.com/bemeek-io/pando/internal/adapter/secrets/local"
	"github.com/bemeek-io/pando/internal/core/assertion"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/bootstrap"
	"github.com/bemeek-io/pando/internal/core/clock"
	"github.com/bemeek-io/pando/internal/core/deploy"
	"github.com/bemeek-io/pando/internal/core/detection"
	"github.com/bemeek-io/pando/internal/core/planner"
	corepolicy "github.com/bemeek-io/pando/internal/core/policy"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/httpapi"
	"github.com/bemeek-io/pando/internal/secret"
)

// The API's own tests, against a real Postgres.
//
// The handlers hold concrete *state.X rather than interfaces, so there is no
// seam to fake: a handler test either runs against the real store or tests
// nothing. Postgres starts once for the package and each test gets its own
// schema-fresh database, which is cheap next to starting a container per test.

// ownerURL is the connection string for the shared container, resolved once.
var (
	containerOnce sync.Once
	containerURL  string
	containerErr  error
)

func sharedPostgres(t *testing.T) string {
	t.Helper()
	containerOnce.Do(func() {
		ctx := context.Background()
		container, err := postgres.Run(ctx, "postgres:17-alpine",
			postgres.WithDatabase("pando"),
			postgres.WithUsername("pando"),
			postgres.WithPassword("test-password"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(90*time.Second)),
		)
		if err != nil {
			containerErr = err
			return
		}
		// Deliberately not terminated per test: the container lives for the
		// package run and Ryuk reaps it afterwards.
		containerURL, containerErr = container.ConnectionString(ctx, "sslmode=disable")
	})
	require.NoError(t, containerErr)
	return containerURL
}

// install is one Pando installation: a database, a wired API, and an admin.
type install struct {
	t       *testing.T
	handler http.Handler
	db      *state.DB

	Apps        *state.Apps
	Users       *state.Users
	Grants      *state.Grants
	Sessions    *state.Sessions
	Tokens      *state.Tokens
	Secrets     *state.Secrets
	Volumes     *state.Volumes
	Adapters    *state.Adapters
	PolicyStore *state.Policy

	// AdminID and adminPassword are the first-run account (R-046).
	AdminID       string
	adminPassword string
}

// newInstall brings up a fresh installation wired the way main wires it.
//
// Everything real except the runtime and builder adapters: those need Docker,
// and the handlers under test never reach them — a plan or a deploy fails at
// the planner with an adapter error, which is itself worth asserting.
func newInstall(t *testing.T) *install {
	t.Helper()
	ctx := context.Background()

	db, err := state.Connect(ctx, state.ConnectOptions{OwnerURL: freshDatabase(t)})
	require.NoError(t, err)
	t.Cleanup(db.Close)

	logger := zap.NewNop()
	auditor := audit.New(db.Pool)

	users := state.NewUsers(db)
	sessions := state.NewSessions(db)
	tokens := state.NewTokens(db)
	apps := state.NewApps(db)
	volumes := state.NewVolumes(db)
	adapters := state.NewAdapters(db)
	allocations := state.NewAllocations(db)
	authzStore := state.NewAuthzStore(db)
	grants := state.NewGrants(db)
	policyStore := state.NewPolicy(db)
	hostPolicy := corepolicy.New(policyStore.Load)

	const adminPassword = "correct-horse-battery-staple"
	first, err := bootstrap.Run(ctx, users, grants, db, auditor, secret.New(adminPassword))
	require.NoError(t, err)
	require.True(t, first.Created, "a fresh install creates its administrator")

	identity := identitylocal.New(users)

	registry := adapterapi.NewRegistry()
	secretsAdapter := secretslocal.New()
	require.NoError(t, secretsAdapter.Configure(ctx,
		json.RawMessage(`{"key_path":`+quote(t, t.TempDir()+"/secrets.key")+`}`)))
	require.NoError(t, registry.Register("sec_local", secretsAdapter))
	require.NoError(t, registry.SetDefault(adapterapi.CategorySecrets, "sec_local"))

	secrets := state.NewSecrets(db, secretsAdapter, "sec_local")
	deployments := state.NewDeployments(db)
	logStore := deploy.NewLogStore()
	appPlanner := planner.New(registry, hostPolicy, allocations).WithInventory(apps)
	reconciles := state.NewReconciles(db)

	deployer := deploy.NewRunner(registry, appPlanner, apps, deployments, secrets, reconciles,
		logStore, volumes, "http://pando:8080").
		WithServices(state.NewServices(db), secrets)

	minter, err := assertion.NewMinter("https://pando.test", clock.System{})
	require.NoError(t, err)

	authenticator := &httpapi.Authenticator{
		Sessions: sessions, Tokens: tokens, Users: users, Groups: authzStore,
	}
	authorizer := authz.New(authzStore, hostPolicy, nil)

	srv := &httpapi.Server{
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
		Detections:  state.NewDetections(db),

		Authz:   authorizer,
		Authent: authenticator,
		Minter:  minter,

		Grants:     grants,
		HostPolicy: hostPolicy,
		Verbs:      authzStore,
		Defaults:   detection.NewInstallation(registry, "apps.test"),

		PolicyStore: policyStore,
		AuditLog:    audit.NewReader(db.Pool),

		Groups:       state.NewGroups(db),
		Roles:        state.NewRoles(db),
		Backups:      state.NewBackups(db),
		BundleSource: state.NewBundleSource(db),
		Idempotency:  state.NewIdempotency(db),
	}

	return &install{
		t: t, handler: srv.Routes(), db: db,
		Apps: apps, Users: users, Grants: grants, Sessions: sessions, Tokens: tokens,
		Secrets: secrets, Volumes: volumes, Adapters: adapters, PolicyStore: policyStore,
		AdminID: first.User.ID, adminPassword: adminPassword,
	}
}

func quote(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return string(b)
}

// freshDatabase creates an empty database for one install and returns its URL.
//
// A database rather than truncated tables. Migrations seed rows — the built-in
// roles (R-081) and the host policy singleton (R-015) — so emptying every table
// between tests leaves an install whose administrator role does not exist, and
// the next bootstrap fails somewhere far from the cause. Which tables are
// seeded is also a thing migrations may change, and a test harness that has to
// be updated when they do is one that will not be.
func freshDatabase(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	base := sharedPostgres(t)
	owner, err := pgx.Connect(ctx, base)
	require.NoError(t, err)
	defer func() { _ = owner.Close(ctx) }()

	name := fmt.Sprintf("pando_test_%d", nextDatabase.Add(1))
	_, err = owner.Exec(ctx, `CREATE DATABASE "`+name+`"`)
	require.NoError(t, err)

	u, err := url.Parse(base)
	require.NoError(t, err)
	u.Path = "/" + name
	return u.String()
}

var nextDatabase atomic.Int64

// --- requests --------------------------------------------------------------

// session is an authenticated caller: a cookie, or a bearer token.
type session struct {
	cookie string
	token  string
}

type reply struct {
	Code int
	Body []byte
	Hdr  http.Header
}

// JSON decodes the body into v.
func (r reply) JSON(t *testing.T, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(r.Body, v), string(r.Body))
}

// Code returned by the error envelope, or "" when the body is not one.
func (r reply) ErrorCode() string {
	var env struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(r.Body, &env)
	return env.Code
}

func (r reply) String() string { return string(r.Body) }

func (i *install) do(s *session, method, path string, body any) reply {
	i.t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(i.t, err)
		reader = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(method, "/api/v1"+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s != nil && s.cookie != "" {
		req.Header.Set("Cookie", httpapi.SessionCookie+"="+s.cookie)
	}
	if s != nil && s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}

	rec := httptest.NewRecorder()
	i.handler.ServeHTTP(rec, req)
	return reply{Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

// anon makes a request with no credential at all. Anonymous is a principal,
// not an absence (R-075).
func (i *install) anon(method, path string, body any) reply {
	return i.do(nil, method, path, body)
}

// signIn returns a session for a username and password.
func (i *install) signIn(username, password string) (*session, reply) {
	i.t.Helper()
	got := i.do(nil, http.MethodPost, "/sessions",
		map[string]string{"username": username, "password": password})

	s := &session{}
	for _, c := range (&http.Response{Header: got.Hdr}).Cookies() {
		if c.Name == httpapi.SessionCookie {
			s.cookie = c.Value
		}
	}
	return s, got
}

// admin is the first-run administrator, signed in.
func (i *install) admin() *session {
	i.t.Helper()
	s, got := i.signIn(bootstrap.AdminUsername, i.adminPassword)
	require.Equal(i.t, http.StatusOK, got.Code, got.String())
	require.NotEmpty(i.t, s.cookie)
	return s
}

// user creates an ordinary account and signs in as it. It holds no
// install-scoped verb and owns no app (R-087).
func (i *install) user(username string) *session {
	i.t.Helper()
	const password = "another-correct-horse-staple"

	created := i.do(i.admin(), http.MethodPost, "/users", map[string]any{
		"username": username, "display_name": username, "password": password,
	})
	require.Equal(i.t, http.StatusCreated, created.Code, created.String())

	s, got := i.signIn(username, password)
	require.Equal(i.t, http.StatusOK, got.Code, got.String())
	return s
}

// tokenFor mints a delegated token for the signed-in caller (R-058).
func (i *install) tokenFor(s *session) *session {
	i.t.Helper()
	got := i.do(s, http.MethodPost, "/tokens", map[string]any{"name": "test token"})
	require.Equal(i.t, http.StatusCreated, got.Code, got.String())

	var issued struct {
		Secret string `json:"secret"`
	}
	got.JSON(i.t, &issued)
	require.NotEmpty(i.t, issued.Secret)
	return &session{token: issued.Secret}
}

// createApp makes an app owned by the caller and returns its ID.
func (i *install) createApp(s *session, name string) string {
	i.t.Helper()
	got := i.do(s, http.MethodPost, "/apps", map[string]any{
		"name":   name,
		"source": map[string]string{"type": "git", "url": "https://github.com/acme/" + name},
	})
	require.Contains(i.t, []int{http.StatusCreated, http.StatusAccepted}, got.Code, got.String())

	var app struct {
		ID string `json:"id"`
	}
	got.JSON(i.t, &app)
	require.NotEmpty(i.t, app.ID)
	return app.ID
}
