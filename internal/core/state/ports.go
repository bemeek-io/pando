package state

import (
	"context"

	"github.com/bemeek-io/pando/internal/errs"
)

// Ports allocates the host ports that port-mode routing hands out.
//
// [P] — unspecified in the requirements. R-166 prefers subdomain and falls back
// to path, and the loopback adapter that ships as the laptop default supports
// neither: it does port mode only. So an install using it has to get a port
// from somewhere, and nothing said where. See docs/plan/open-decisions.md O-15.
//
// The rule implemented is the boring one: the lowest free port in a configured
// range. Not random, so the same app tends to keep the same port across a
// rebuild and a bookmark keeps working; not sequential-forever, so a deleted
// app's port is reused rather than the range slowly filling up.
type Ports struct{ db *DB }

// NewPorts returns a port allocator over db.
func NewPorts(db *DB) *Ports { return &Ports{db: db} }

// NextFree returns the lowest unused port in [from, to] for an adapter.
//
// Both pinned specs and outstanding detection proposals are counted. Counting
// only pinned specs would hand the same port to two apps detected before either
// was accepted — which is the normal case when someone adds a few at once, and
// a collision that surfaces as one app silently taking over another's address.
func (p *Ports) NextFree(ctx context.Context, adapterRef string, from, to int) (int, error) {
	if from <= 0 || to < from {
		return 0, errs.Newf(errs.Internal,
			"The configured port range %d-%d is not usable.", from, to)
	}

	rows, err := p.db.Query(ctx, `
		SELECT (r.body->'routing'->>'port')::int AS port
		FROM apps a
		JOIN spec_revisions r ON r.id = a.pinned_spec_id
		WHERE a.deleted_at IS NULL
		  AND r.body->'routing'->>'adapter_ref' = $1
		  AND r.body->'routing'->>'mode' = 'port'
		  AND (r.body->'routing'->>'port') ~ '^[0-9]+$'
		UNION
		SELECT (d.body->'draft_spec'->'routing'->>'port')::int AS port
		FROM detections d
		JOIN apps a2 ON a2.id = d.app_id
		WHERE a2.deleted_at IS NULL
		  AND d.body->'draft_spec'->'routing'->>'adapter_ref' = $1
		  AND d.body->'draft_spec'->'routing'->>'mode' = 'port'
		  AND (d.body->'draft_spec'->'routing'->>'port') ~ '^[0-9]+$'
	`, adapterRef)
	if err != nil {
		return 0, errs.Wrap(errs.Internal, "Could not read which ports are already in use.", err)
	}
	defer rows.Close()

	taken := map[int]bool{}
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err != nil {
			return 0, errs.Wrap(errs.Internal, "Could not read which ports are already in use.", err)
		}
		taken[port] = true
	}
	if err := rows.Err(); err != nil {
		return 0, errs.Wrap(errs.Internal, "Could not read which ports are already in use.", err)
	}

	for port := from; port <= to; port++ {
		if !taken[port] {
			return port, nil
		}
	}

	return 0, errs.Newf(errs.CapacityNoFreePort,
		"Every port between %d and %d is already assigned to an app.", from, to).
		WithDetail("range_start", from).
		WithDetail("range_end", to).
		WithDetail("in_use", len(taken)).
		WithRemedy("Delete an app that is no longer needed, or widen the port range with " +
			"PANDO_SERVER_PORT_RANGE_START and PANDO_SERVER_PORT_RANGE_END.")
}
