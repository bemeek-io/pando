package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/httpapi"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func newServer(dbErr error) http.Handler {
	return (&httpapi.Server{Logger: zap.NewNop(), DB: fakeDB{err: dbErr}}).Routes()
}

func TestHealthzRespondsWithoutTouchingTheDatabase(t *testing.T) {
	// Liveness must not depend on Postgres: a check that fails when the database
	// blips gets the process killed just as it was about to recover.
	rec := httptest.NewRecorder()
	newServer(errors.New("database is down")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestReadyzReflectsTheDatabase(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	newServer(errors.New("connection refused")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// The reason is a category, not the driver's error text.
	require.NotContains(t, rec.Body.String(), "connection refused")
}

func TestEveryResponseCarriesARequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	rid := rec.Header().Get(httpapi.RequestIDHeader)
	require.NotEmpty(t, rid)
	require.Regexp(t, `^req_[0-9A-HJKMNP-TV-Z]{26}$`, rid)
}

func TestErrorEnvelopeCarriesCodeStatusAndRequestID(t *testing.T) {
	handler := httpapi.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(w, r, errs.New(errs.PlanSlotUnfilled, "This app needs a Redis, and one hasn't been chosen yet.").
			WithRemedy("Choose how to fill the REDIS_URL slot.").
			WithDetail("slots", []string{"REDIS_URL"}))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusConflict, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "PLAN_SLOT_UNFILLED", body["code"])
	require.NotEmpty(t, body["remedy"])
	require.Equal(t, rec.Header().Get(httpapi.RequestIDHeader), body["request_id"])
}

// An error with no envelope must become INTERNAL, and its detail must not reach
// the response body.
func TestUnenvelopedErrorDoesNotLeakDetail(t *testing.T) {
	handler := httpapi.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(w, r, errors.New("pq: relation \"users\" does not exist"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "relation")

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "INTERNAL", body["code"])
}
