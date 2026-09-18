package httpapi

import (
	"context"
	"net/http"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/security"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
)

// The security score (R-310 – R-320, design 09 §6).
//
// Reading it needs `app.view`, because it is a fact about an app somebody can
// already see. Asking for a new one needs `app.deploy`: a scan changes what the
// next deploy will do, which makes it a write however much it looks like a
// refresh.

func (s *Server) handleAppSecurity(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	if s.Security == nil {
		Error(w, r, errs.New(errs.AdapterUnavailable, "This installation does not scan apps."))
		return
	}

	report, err := s.Security.Report(r.Context(), app.ID, app.PinnedSpecID)
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, report)
}

// handleScanApp takes a new scan now (R-312).
//
// Synchronous, unlike a deploy. A scan is a minute at the outside, the caller
// asked for it and is waiting for the number — and an endpoint that returned
// 202 would need somewhere to report a scanner failure, which is the case this
// exists to surface.
func (s *Server) handleScanApp(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppDeploy)
	if !ok {
		return
	}
	if s.Security == nil {
		Error(w, r, errs.New(errs.AdapterUnavailable, "This installation does not scan apps."))
		return
	}

	// What is running, which is what somebody rescanning wants to know about.
	// An app that has never deployed has no image, and the scanner says so
	// rather than this endpoint guessing at one.
	image, err := s.lastDeployedImage(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}

	p := PrincipalFrom(r.Context())
	scan, err := s.Security.Scan(r.Context(), api.ScanRequest{
		AppID:  app.ID,
		SpecID: app.PinnedSpecID,
		Image:  image,
	}, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
	})
	if err != nil {
		Error(w, r, err)
		return
	}

	report, err := s.Security.Report(r.Context(), app.ID, app.PinnedSpecID)
	if err != nil {
		Error(w, r, err)
		return
	}
	_ = scan
	JSON(w, http.StatusOK, report)
}

// lastDeployedImage is the image the app is running.
//
// Empty for an app that has never deployed: the scanner then reports that there
// is nothing to look at, which is the honest answer and a better one than
// scanning whatever image happens to share the app's name.
func (s *Server) lastDeployedImage(ctx context.Context, appID string) (string, error) {
	if s.Deployments == nil {
		return "", nil
	}
	return s.Deployments.LastImage(ctx, appID)
}

// Security is what the API needs from the security service. Three calls, so a
// handler cannot reach past them into policy or the scan store.
type Security interface {
	Report(ctx context.Context, appID, specID string) (security.Report, error)
	Scan(ctx context.Context, req api.ScanRequest, principal audit.Event) (state.Scan, error)
	Place(ctx context.Context, scores map[string]*int) (map[string]security.Verdict, error)
}

// withVerdicts places each app's score against host policy.
//
// Here rather than in the store for the same reason addresses are: it depends
// on a policy document and on whether a scanner is configured, and neither is
// the store's to know. One policy load for the whole list.
//
// A failure is not fatal. The verdict decides what color a badge is; the list
// of apps is what the caller asked for, and losing the second is a worse answer
// than losing the first.
func (s *Server) withVerdicts(ctx context.Context, apps []state.App) []state.App {
	if s.Security == nil || len(apps) == 0 {
		return apps
	}

	scores := make(map[string]*int, len(apps))
	for _, app := range apps {
		scores[app.ID] = app.SecurityScore
	}

	verdicts, err := s.Security.Place(ctx, scores)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("could not place apps against the security threshold", zap.Error(err))
		}
		return apps
	}

	for i := range apps {
		apps[i].SecurityVerdict = string(verdicts[apps[i].ID])
	}
	return apps
}
