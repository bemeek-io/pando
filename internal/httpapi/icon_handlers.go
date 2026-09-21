package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// An app's tile image (R-340).
//
// Setting it is app.spec.edit, the verb that already governs renaming: it is
// how the app presents itself, and there is no narrower verb for that. Built-in
// roles change only by migration (R-081), and a new verb for one picture is not
// worth that.
//
// Reading it is the data plane. The image is shown on the launcher, and the
// launcher lists the apps a person can open (R-264) — so whoever sees the tile
// can load its picture, and nobody else learns the app exists. An administrator
// who manages an app but may not open it gets the not-found and a placeholder;
// falling back to app.view here would write a denied app.use to the audit log
// on every look at the admin screen.

// maxIconBytes caps an upload. A tile is drawn at a few hundred pixels at
// most, and this is stored in Postgres and read on every launcher load.
const maxIconBytes = 256 << 10

// iconTypes are the formats accepted, by sniffed content type. Not SVG: an SVG
// is a document that can carry script, served from Pando's own origin.
var iconTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

func (s *Server) handleSetAppIcon(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}

	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxIconBytes))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		Error(w, r, errs.New(errs.ValidInvalid, "This image is larger than 256 KB, the most Pando accepts for an app's image.").
			WithRemedy("Resize or compress it and upload it again. A square image a few hundred pixels across is plenty."))
		return
	}
	if err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if len(data) == 0 {
		Error(w, r, errs.New(errs.ValidInvalid, "The request had no image in it.").
			WithRemedy("Send the image file itself as the request body. To remove an app's image, use DELETE instead."))
		return
	}

	// The bytes decide, not the Content-Type header. A header is whatever the
	// client said; what is served back is what the file is.
	contentType := http.DetectContentType(data)
	if !iconTypes[contentType] {
		Error(w, r, errs.Newf(errs.ValidInvalid, "An app's image must be a PNG, JPEG, WebP or GIF file. This file looks like %s.", contentType).
			WithRemedy("Export the image as a PNG and upload that."))
		return
	}

	if err := s.Apps.SetIcon(r.Context(), app.ID, contentType, data); err != nil {
		Error(w, r, err)
		return
	}
	s.auditApp(r, app.ID, "app.icon.set")
	s.respondApp(w, r, app.ID)
}

func (s *Server) handleClearAppIcon(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppSpecEdit)
	if !ok {
		return
	}
	if err := s.Apps.ClearIcon(r.Context(), app.ID); err != nil {
		Error(w, r, err)
		return
	}
	s.auditApp(r, app.ID, "app.icon.clear")
	s.respondApp(w, r, app.ID)
}

func (s *Server) handleGetAppIcon(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")
	notFound := errs.New(errs.NotFound, "There is no image for that app.")
	if !id.Is(id.App, appID) {
		Error(w, r, notFound)
		return
	}
	if _, found, err := s.Apps.ByID(r.Context(), appID); err != nil {
		Error(w, r, err)
		return
	} else if !found {
		Error(w, r, notFound)
		return
	}
	// Not found rather than forbidden, for the same reason as requireControl:
	// answering "forbidden" confirms the app exists.
	if err := s.Authz.CheckData(r.Context(), PrincipalFrom(r.Context()), appID); err != nil {
		if errs.CodeOf(err) == errs.Internal {
			Error(w, r, err)
			return
		}
		Error(w, r, notFound)
		return
	}

	icon, found, err := s.Apps.IconOf(r.Context(), appID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		Error(w, r, notFound)
		return
	}

	h := w.Header()
	h.Set("Content-Type", icon.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(icon.Data)))
	// Served from Pando's origin, so it is held to being an image and nothing
	// else: no sniffing it into something executable, and no scripts or
	// subresources if a browser is ever talked into rendering it as a page.
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// Private, because who may see it depends on the session. Cacheable,
	// because clients put icon_updated_at in the URL and a new image is a new
	// URL.
	h.Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(icon.Data)
}

// respondApp answers with the app as it now is, as PATCH does.
func (s *Server) respondApp(w http.ResponseWriter, r *http.Request, appID string) {
	updated, _, err := s.Apps.ByID(r.Context(), appID)
	if err != nil {
		Error(w, r, err)
		return
	}
	updated.Address = spec.Address(r.Host, updated.Slug, updated.Routing)
	JSON(w, http.StatusOK, updated)
}
