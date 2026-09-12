package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/bemeek-io/pando/internal/errs"
)

// Idempotency for infrastructure-creating requests.
//
// A key can arrive in the `Idempotency-Key` header or in the body as
// `idempotency_key`. The header is the convention; the body is there because an
// MCP tool's arguments are a JSON object and making an agent set a header means
// giving it a header-setting tool, which is a worse surface than one more
// field.

// IdempotencyHeader is the canonical place to put a key.
const IdempotencyHeader = "Idempotency-Key"

// idempotencyKeyFrom reads a key from the header, or from an already-decoded
// body map. Empty means the caller did not ask for idempotency and gets none —
// making it the default would silently deduplicate two deliberate deploys.
func idempotencyKeyFrom(r *http.Request, body map[string]any) string {
	if key := r.Header.Get(IdempotencyHeader); key != "" {
		return key
	}
	if body != nil {
		if key, ok := body["idempotency_key"].(string); ok {
			return key
		}
	}
	return ""
}

// replayed writes a stored response and reports whether it did.
//
// The replayed status code is the original's, so a retry of a 202 is a 202.
// Returning 200 for a replay would make a client think something different
// happened the second time, which is the opposite of the point.
func (s *Server) replayed(w http.ResponseWriter, r *http.Request, key, endpoint string) bool {
	if key == "" || s.Idempotency == nil {
		return false
	}
	p := PrincipalFrom(r.Context())

	prior, found, err := s.Idempotency.Lookup(r.Context(), key, p.ID, endpoint)
	if err != nil {
		// A lookup failure must not fail the request: the cost is a possible
		// duplicate, and refusing to act because the deduplication table is
		// unreadable is a worse outcome than acting twice.
		return false
	}
	if !found {
		return false
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Idempotent-Replay", "true")
	w.WriteHeader(prior.StatusCode)
	_, _ = w.Write(prior.Body)
	return true
}

// remember stores a response for replay. Failures are ignored for the same
// reason lookup failures are.
func (s *Server) remember(r *http.Request, key, endpoint string, status int, body any) {
	if key == "" || s.Idempotency == nil {
		return
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return
	}
	p := PrincipalFrom(r.Context())
	_ = s.Idempotency.Remember(r.Context(), key, p.ID, endpoint, status, encoded)
}

// decodeWithKey reads a JSON body into out and returns any idempotency key in
// it, leaving the body usable by the caller.
func decodeWithKey(r *http.Request, out any) (string, error) {
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(r.Body); err != nil {
		return "", errs.New(errs.ValidInvalid, "The request body could not be read.")
	}

	var generic map[string]any
	if raw.Len() > 0 {
		if err := json.Unmarshal(raw.Bytes(), &generic); err != nil {
			return "", errs.New(errs.ValidInvalid, "The request body could not be read.")
		}
		if out != nil {
			if err := json.Unmarshal(raw.Bytes(), out); err != nil {
				return "", errs.New(errs.ValidInvalid, "The request body could not be read.")
			}
		}
	}
	return idempotencyKeyFrom(r, generic), nil
}
