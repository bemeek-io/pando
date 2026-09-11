package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/log"
)

// Health reports whether a dependency is reachable.
type Health interface {
	Ping(ctx context.Context) error
}

// Server holds what the HTTP surface needs.
//
// Handlers contain no business logic (R-261). Everything lives in a service
// layer under internal/core that both this package and internal/mcp call — a
// capability in one surface and not the other means logic leaked into a
// handler.
type Server struct {
	Logger *zap.Logger
	DB     Health

	Identity api.IdentityAdapter
	Users    *state.Users
	Sessions *state.Sessions
	Tokens   *state.Tokens
	Authz    *authz.Authorizer
	Auditor  *audit.Writer
	Authent  *Authenticator
}

// audit records an event, logging rather than failing the request if the write
// does not land. An audit failure must not become a denial of service on the
// action being audited — but it must never pass silently either.
func (s *Server) audit(r *http.Request, e audit.Event) {
	if s.Auditor == nil {
		return
	}
	e.RequestID = RequestIDFrom(r.Context())
	if err := s.Auditor.Write(r.Context(), e); err != nil {
		log.From(r.Context()).Error("audit write failed", zap.String("action", e.Action), zap.Error(err))
	}
}

// Routes builds the router.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(RequestID)
	r.Use(Logger(s.Logger))

	// Liveness. Deliberately does not touch the database: a health check that
	// fails when Postgres blips causes an orchestrator to kill a process that
	// was about to recover on its own.
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Readiness. This one does check, because "ready to serve traffic" is
	// exactly the question it answers.
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB.Ping(r.Context()); err != nil {
			JSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not ready",
				"reason": "state store unreachable",
			})
			return
		}
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(Authenticate(s.Authent))

		r.Post("/sessions", s.handleLogin)
		r.Delete("/sessions", s.handleLogout)
		r.Get("/me", s.handleMe)

		// Phases 2 onward mount resources here.
	})

	return r
}
