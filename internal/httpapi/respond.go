package httpapi

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/log"
)

// JSON writes a successful response.
func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

// Error writes the error envelope (design 00 §3.2).
//
// An error arriving without an envelope becomes INTERNAL and its detail is
// logged rather than returned: an unenveloped error is by definition one nobody
// wrote a user-facing message for, and guessing one would leak implementation
// detail into a response body.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()

	e := errs.As(err)
	if e == nil {
		log.From(ctx).Error("unenveloped error reached the API boundary", zap.Error(err))
		e = errs.New(errs.Internal, "Something went wrong. The error has been logged.")
	}
	e = e.WithRequestID(RequestIDFrom(ctx))

	if e.Status() >= 500 {
		log.From(ctx).Error("request failed", zap.String("code", string(e.Code)), zap.Error(err))
	} else {
		log.From(ctx).Info("request rejected", zap.String("code", string(e.Code)))
	}

	JSON(w, e.Status(), e)
}
