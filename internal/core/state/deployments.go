package state

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// Deployment statuses.
const (
	DeployPending    = "pending"
	DeployBuilding   = "building"
	DeployApplying   = "applying"
	DeploySucceeded  = "succeeded"
	DeployFailed     = "failed"
	DeploySuperseded = "superseded"
)

// Deployment triggers.
const (
	TriggerManual   = "manual"
	TriggerRollback = "rollback"
)

// Deployment is one attempt to make an app match a spec.
//
// Its own table, separate from the app, because a build failure fails the
// deployment and leaves the app untouched (R-146). Collapsing them would make
// that distinction impossible to express.
type Deployment struct {
	ID          string     `json:"id"`
	AppID       string     `json:"app_id"`
	SpecID      string     `json:"spec_id"`
	Trigger     string     `json:"trigger"`
	Status      string     `json:"status"`
	ErrorCode   string     `json:"error_code,omitempty"`
	ErrorDetail string     `json:"error_detail,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	CreatedBy   string     `json:"created_by"`
}

// Deployments stores deployment records.
type Deployments struct{ db *DB }

func NewDeployments(db *DB) *Deployments { return &Deployments{db: db} }

// Create records a new deployment.
func (d *Deployments) Create(ctx context.Context, appID, specID, trigger, createdBy string) (Deployment, error) {
	dep := Deployment{
		ID:        id.New(id.Deployment),
		AppID:     appID,
		SpecID:    specID,
		Trigger:   trigger,
		Status:    DeployPending,
		CreatedBy: createdBy,
	}
	err := d.db.QueryRow(ctx, `
		INSERT INTO deployments (id, app_id, spec_id, trigger, status, created_by)
		VALUES ($1, $2, $3, $4, 'pending', $5) RETURNING started_at`,
		dep.ID, appID, specID, trigger, createdBy).Scan(&dep.StartedAt)
	if err != nil {
		return Deployment{}, errs.Wrap(errs.Internal, "Could not start the deploy.", err)
	}
	return dep, nil
}

// SetStatus advances a deployment.
func (d *Deployments) SetStatus(ctx context.Context, deploymentID, status string) error {
	_, err := d.db.Exec(ctx, `UPDATE deployments SET status = $2 WHERE id = $1`, deploymentID, status)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not update the deploy.", err)
	}
	return nil
}

// Finish closes a deployment out.
func (d *Deployments) Finish(ctx context.Context, deploymentID, status, errorCode, message string) error {
	var detail any
	if message != "" {
		encoded, err := json.Marshal(map[string]string{"message": message})
		if err == nil {
			detail = encoded
		}
	}
	_, err := d.db.Exec(ctx, `
		UPDATE deployments SET status = $2, error_code = $3, error_detail = $4, finished_at = now()
		WHERE id = $1`, deploymentID, status, nullable(errorCode), detail)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not update the deploy.", err)
	}
	return nil
}

// ByID returns one deployment.
func (d *Deployments) ByID(ctx context.Context, deploymentID string) (Deployment, bool, error) {
	var dep Deployment
	var code *string
	var detail []byte
	err := d.db.QueryRow(ctx, `
		SELECT id, app_id, spec_id, trigger, status, error_code, error_detail, started_at, finished_at, created_by
		FROM deployments WHERE id = $1`, deploymentID).
		Scan(&dep.ID, &dep.AppID, &dep.SpecID, &dep.Trigger, &dep.Status, &code, &detail,
			&dep.StartedAt, &dep.FinishedAt, &dep.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Deployment{}, false, nil
	}
	if err != nil {
		return Deployment{}, false, errs.Wrap(errs.Internal, "Could not read the deploy.", err)
	}
	if code != nil {
		dep.ErrorCode = *code
	}
	if len(detail) > 0 {
		var parsed map[string]string
		if json.Unmarshal(detail, &parsed) == nil {
			dep.ErrorDetail = parsed["message"]
		}
	}
	return dep, true, nil
}

// ListForApp returns an app's deployments, newest first.
func (d *Deployments) ListForApp(ctx context.Context, appID string) ([]Deployment, error) {
	rows, err := d.db.Query(ctx, `
		SELECT id, app_id, spec_id, trigger, status, error_code, started_at, finished_at, created_by
		FROM deployments WHERE app_id = $1 ORDER BY started_at DESC LIMIT 50`, appID)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not list the app's deploys.", err)
	}
	defer rows.Close()

	var out []Deployment
	for rows.Next() {
		var dep Deployment
		var code *string
		if err := rows.Scan(&dep.ID, &dep.AppID, &dep.SpecID, &dep.Trigger, &dep.Status, &code,
			&dep.StartedAt, &dep.FinishedAt, &dep.CreatedBy); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not list the app's deploys.", err)
		}
		if code != nil {
			dep.ErrorCode = *code
		}
		out = append(out, dep)
	}
	return out, rows.Err()
}

// InFlight reports whether a deployment is already running for an app.
//
// Used to refuse a concurrent deploy rather than queue one: two deploys racing
// on the same bundle is how an app ends up in a state neither of them intended.
func (d *Deployments) InFlight(ctx context.Context, appID string) (bool, error) {
	var exists bool
	err := d.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM deployments
		WHERE app_id = $1 AND status IN ('pending', 'building', 'applying'))`, appID).Scan(&exists)
	if err != nil {
		return false, errs.Wrap(errs.Internal, "Could not check for a running deploy.", err)
	}
	return exists, nil
}

// SetImageRef records the image a deployment actually ran.
//
// The reconciler restores a missing workload with this rather than rebuilding.
// Rebuilding to correct drift would turn "someone killed a container" into
// "ship whatever is on the branch now" — a far larger action than the one being
// corrected, and one nobody asked for (R-120).
func (d *Deployments) SetImageRef(ctx context.Context, deploymentID, imageRef string) error {
	if imageRef == "" {
		return nil
	}
	_, err := d.db.Exec(ctx,
		`UPDATE deployments SET image_ref = $2 WHERE id = $1`, deploymentID, imageRef)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the deployed image.", err)
	}
	return nil
}
