package state

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bemeek-io/pando/internal/errs"
)

// Reconcilable is an app the loop should look at, with the bookkeeping it needs.
type Reconcilable struct {
	App

	ConsecutiveFailures int
	LastFailureAt       *time.Time
	NextAttemptAt       *time.Time
	UnobservableSince   *time.Time
	AppliedEnvHash      string

	// WorkloadImages is what each part of the app ran, for an app whose parts
	// are built separately. Empty for a single-image app, where ImageRef says
	// everything, and for a deployment from before it was recorded.
	//
	// The reconciler restores a missing workload from the recorded image, and
	// with only ImageRef to go on it restored every workload from one image —
	// which for a compose app meant replacing the application with a second
	// copy of whichever service happened to be primary.
	WorkloadImages map[string]WorkloadImage

	// ImageRef is what the last successful deployment ran. The reconciler
	// restores a missing workload with this rather than rebuilding: correcting
	// drift must not become "ship whatever is on the branch now".
	ImageRef string

	// ImageDigest is what the runtime resolved that reference to. A running
	// container reports a digest, not a reference, so this is the only thing
	// that can be compared against one — and a tag can point somewhere new
	// without the reference changing at all.
	ImageDigest string
}

// Reconciles reads and writes the reconciler's view of an app.
type Reconciles struct{ db *DB }

// NewReconciles returns a store over db.
func NewReconciles(db *DB) *Reconciles { return &Reconciles{db: db} }

// States the reconciler acts on.
//
// The exclusions are the design: `draft` and `proposed` have nothing running,
// `deploying` belongs to the deployment, `archived` is gone — and `failed` is
// absent because R-151 is true by there being no code path, not by a check.
// Adding it to this list is how that requirement gets broken.
var reconcilableStates = []string{StateRunning, StateDegraded, StateStopped}

// Due returns the apps the loop should reconcile now.
//
// Apps in backoff are filtered here rather than skipped in the loop, so an app
// waiting five minutes costs one row in a WHERE clause instead of a goroutine
// per tick that wakes up and does nothing.
func (r *Reconciles) Due(ctx context.Context, now time.Time, limit int) ([]Reconcilable, error) {
	rows, err := r.db.Query(ctx, `
		SELECT a.id, a.name, a.slug, a.owner_user_id, a.state, a.desired_state,
		       a.pinned_spec_id, a.source, a.created_at, a.updated_at,
		       a.consecutive_failures, a.last_failure_at,
		       a.next_attempt_at, a.unobservable_since,
		       coalesce(a.applied_env_fingerprint, ''),
		       coalesce((
		           SELECT d.image_ref FROM deployments d
		           WHERE d.app_id = a.id AND d.status = 'succeeded' AND d.image_ref IS NOT NULL
		           ORDER BY d.started_at DESC LIMIT 1
		       ), ''),
		       coalesce((
		           SELECT d.image_digest FROM deployments d
		           WHERE d.app_id = a.id AND d.status = 'succeeded' AND d.image_digest IS NOT NULL
		           ORDER BY d.started_at DESC LIMIT 1
		       ), ''),
		       (
		           SELECT d.workload_images FROM deployments d
		           WHERE d.app_id = a.id AND d.status = 'succeeded' AND d.workload_images IS NOT NULL
		           ORDER BY d.started_at DESC LIMIT 1
		       )
		FROM apps a
		WHERE a.deleted_at IS NULL
		  AND a.state = ANY($1)
		  AND a.pinned_spec_id IS NOT NULL
		  AND (a.next_attempt_at IS NULL OR a.next_attempt_at <= $2)
		ORDER BY a.updated_at
		LIMIT $3
	`, reconcilableStates, now, limit)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read which apps need attention.", err)
	}
	defer rows.Close()

	var out []Reconcilable
	for rows.Next() {
		var a Reconcilable
		var owner, pinned *string
		var source []byte

		var workloadImages []byte
		if err := rows.Scan(&a.ID, &a.Name, &a.Slug, &owner, &a.State, &a.DesiredState,
			&pinned, &source, &a.CreatedAt, &a.UpdatedAt,
			&a.ConsecutiveFailures, &a.LastFailureAt,
			&a.NextAttemptAt, &a.UnobservableSince,
			&a.AppliedEnvHash, &a.ImageRef, &a.ImageDigest, &workloadImages); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read which apps need attention.", err)
		}
		if len(workloadImages) > 0 {
			// A deployment written before this column, or by a version that
			// wrote something else into it, leaves the map empty: the
			// reconciler then knows nothing per workload, which is where it
			// started, rather than knowing something wrong.
			_ = json.Unmarshal(workloadImages, &a.WorkloadImages)
		}
		if owner != nil {
			a.OwnerUserID = *owner
		}
		if pinned != nil {
			a.PinnedSpecID = *pinned
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read which apps need attention.", err)
	}
	return out, nil
}

// Lock takes a per-app advisory lock for the duration of one reconciliation.
//
// Advisory rather than a row lock: two ticks must not overlap on one app, but a
// tick must not block anyone else reading or writing that app's row either. A
// second holder is told immediately rather than waiting — the next tick is
// fifteen seconds away, and a queue of ticks behind a slow app is how one
// unhealthy app stops every other one being looked at.
func (r *Reconciles) Lock(ctx context.Context, appID string) (release func(), ok bool, err error) {
	conn, err := r.db.Acquire(ctx)
	if err != nil {
		return nil, false, errs.Wrap(errs.Internal, "Could not take the reconciliation lock.", err)
	}

	var acquired bool
	if err := conn.QueryRow(ctx,
		`SELECT pg_try_advisory_lock(hashtext($1))`, lockNamespace+appID).Scan(&acquired); err != nil {
		conn.Release()
		return nil, false, errs.Wrap(errs.Internal, "Could not take the reconciliation lock.", err)
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}

	return func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx),
			`SELECT pg_advisory_unlock(hashtext($1))`, lockNamespace+appID)
		conn.Release()
	}, true, nil
}

// lockNamespace keeps reconciliation locks from colliding with any other
// advisory lock this install might take on the same identifier.
const lockNamespace = "pando:reconcile:"

// MarkUnobservable records that the adapter could not be reached.
//
// It touches neither state nor the failure counter. An adapter being down is a
// platform problem, not app failure — otherwise restarting the Docker daemon
// marks every app on the host as failed (design 05 §2).
func (r *Reconciles) MarkUnobservable(ctx context.Context, appID, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE apps
		SET unobservable_since = coalesce(unobservable_since, now()),
		    last_reconcile_error = $2,
		    updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
	`, appID, reason)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record that the app could not be observed.", err)
	}
	return nil
}

// ClearUnobservable is called on the first successful Observe.
func (r *Reconciles) ClearUnobservable(ctx context.Context, appID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE apps SET unobservable_since = NULL, updated_at = now()
		WHERE id = $1 AND unobservable_since IS NOT NULL
	`, appID)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not clear the unobservable marker.", err)
	}
	return nil
}

// RecordFailure increments the failure counter and returns the new count.
//
// The window is an idle timeout measured from the last failure, not a fixed
// window from the first. Measured from the first it is unreachable: R-149's
// backoff caps at five minutes, so ten attempts span 31.3 minutes against a
// 30-minute window that resets at 30 — the app retries forever and is never
// given up on, which is what R-150 exists to prevent.
//
// As an idle timeout it means what the requirement means. Consecutive failures
// always reach the threshold however long backoff stretches them out, and
// unrelated failures a day apart never accumulate.
func (r *Reconciles) RecordFailure(ctx context.Context, appID, reason string, window time.Duration, next time.Time) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		UPDATE apps
		SET consecutive_failures = CASE
		        WHEN last_failure_at IS NULL OR last_failure_at < now() - $2::interval
		        THEN 1
		        ELSE consecutive_failures + 1
		    END,
		    last_failure_at = now(),
		    next_attempt_at = $3,
		    last_reconcile_error = $4,
		    updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING consecutive_failures
	`, appID, window, next, reason).Scan(&count)
	if err != nil {
		return 0, errs.Wrap(errs.Internal, "Could not record the failure.", err)
	}
	return count, nil
}

// ClearFailures resets the counter and backoff.
//
// Called only when an app is running with health passing. A flapping app that
// recovers between failures still accumulates toward the threshold, which is
// correct: flapping is a failure mode, not a recovery.
func (r *Reconciles) ClearFailures(ctx context.Context, appID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE apps
		SET consecutive_failures = 0, last_failure_at = NULL,
		    next_attempt_at = NULL, last_reconcile_error = NULL, updated_at = now()
		WHERE id = $1 AND consecutive_failures > 0
	`, appID)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not reset the failure count.", err)
	}
	return nil
}

// SetAppliedEnvFingerprint records the environment an app was last applied with.
//
// R-193's only mechanism. Observe returns no environment and deliberately
// should not — reading it back would require every runtime adapter to handle
// secret-bearing data, which is what the adapter interface works to avoid — so
// a rotated secret is invisible to observation and is detected by comparing
// this instead (design 02 §2.4).
func (r *Reconciles) SetAppliedEnvFingerprint(ctx context.Context, appID, fingerprint string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE apps SET applied_env_fingerprint = $2, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
	`, appID, fingerprint)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the applied environment.", err)
	}
	return nil
}
