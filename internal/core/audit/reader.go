package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bemeek-io/pando/internal/errs"
)

// Reader queries the audit log (R-227, R-229).
//
// A separate type from Writer, because they are separate privileges: writing is
// something every code path does, and reading is gated by install.audit.read.
// Neither can update or delete — the database refuses both to the application
// role (R-027), so this type could not offer it even if someone added a method.
type Reader struct {
	pool *pgxpool.Pool
}

func NewReader(pool *pgxpool.Pool) *Reader { return &Reader{pool: pool} }

// Record is one stored event, as read back.
type Record struct {
	ID            int64          `json:"id"`
	OccurredAt    time.Time      `json:"occurred_at"`
	PrincipalKind string         `json:"principal_kind"`
	PrincipalID   string         `json:"principal_id,omitempty"`
	OnBehalfOf    string         `json:"on_behalf_of,omitempty"`
	Action        string         `json:"action"`
	AppID         string         `json:"app_id,omitempty"`
	TargetKind    string         `json:"target_kind,omitempty"`
	TargetID      string         `json:"target_id,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	Detail        map[string]any `json:"detail,omitempty"`
}

// Query narrows a read. Every field is optional; the zero value reads the whole
// log, newest first.
type Query struct {
	// Action matches a prefix, so "app." finds every app event and
	// "session.denied" finds exactly one kind. A prefix rather than a substring
	// because actions are dotted namespaces — a substring match would make
	// "grant.delete" a result for a search for "delete".
	Action string

	AppID       string
	PrincipalID string

	// Limit defaults to 100, capped at 500.
	Limit int

	// Cursor is the ID of the last record on the previous page. The log is
	// append-only with monotonic IDs, so "before this ID" is a stable page
	// boundary in a way an offset is not — with an offset, events arriving
	// between requests shift every later page.
	Cursor int64
}

const (
	defaultAuditLimit = 100
	maxAuditLimit     = 500
)

// List reads the log, newest first.
func (r *Reader) List(ctx context.Context, q Query) ([]Record, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultAuditLimit
	}
	if limit > maxAuditLimit {
		limit = maxAuditLimit
	}

	var where []string
	var args []any

	// arg appends a value and returns its placeholder, so the clauses below
	// cannot drift out of step with the argument list.
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if q.Action != "" {
		// The prefix is escaped: an action containing % or _ would otherwise
		// widen the filter rather than narrow it.
		where = append(where, "action LIKE "+arg(escapeLike(q.Action))+" || '%' ESCAPE '\\'")
	}
	if q.AppID != "" {
		where = append(where, "app_id = "+arg(q.AppID))
	}
	if q.PrincipalID != "" {
		// Either column: a delegated token records both itself and its owner
		// (R-229), and "what did this person do" must find both.
		p := arg(q.PrincipalID)
		where = append(where, "(principal_id = "+p+" OR on_behalf_of = "+p+")")
	}
	if q.Cursor > 0 {
		where = append(where, "id < "+arg(q.Cursor))
	}

	sql := `SELECT id, occurred_at, principal_kind, coalesce(principal_id, ''),
	               coalesce(on_behalf_of, ''), action, coalesce(app_id, ''),
	               coalesce(target_kind, ''), coalesce(target_id, ''),
	               coalesce(request_id, ''), detail
	        FROM audit_events`
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	sql += " ORDER BY id DESC LIMIT " + arg(limit)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read the audit log.", err)
	}
	defer rows.Close()

	out := make([]Record, 0, limit)
	for rows.Next() {
		var rec Record
		var detail []byte
		if err := rows.Scan(&rec.ID, &rec.OccurredAt, &rec.PrincipalKind, &rec.PrincipalID,
			&rec.OnBehalfOf, &rec.Action, &rec.AppID, &rec.TargetKind, &rec.TargetID,
			&rec.RequestID, &detail); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read the audit log.", err)
		}
		if len(detail) > 0 {
			if err := json.Unmarshal(detail, &rec.Detail); err != nil {
				// A detail blob that will not parse is a damaged row, not a
				// reason to withhold the event. The who/what/when is the part
				// an investigation needs first.
				rec.Detail = map[string]any{"unreadable": true}
			}
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// escapeLike neutralizes the wildcards in a user-supplied prefix.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
