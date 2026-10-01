package state

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/trypando/pando/internal/errs"
)

// RunningSpecID is the spec revision the app's newest successful deploy
// shipped: what it runs now. Empty for an app that has never deployed.
//
// What deploy approval measures a deploy against (R-154): the app's own
// approval requirement is read from it as well as from the spec being
// deployed, and an egress loosening counts only when the running spec does not
// already carry it.
func (d *Deployments) RunningSpecID(ctx context.Context, appID string) (string, error) {
	var specID string
	err := d.db.QueryRow(ctx, `
		SELECT spec_id FROM deployments
		WHERE app_id = $1 AND status = $2
		ORDER BY started_at DESC
		LIMIT 1`, appID, DeploySucceeded).Scan(&specID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", errs.Wrap(errs.Internal, "Could not read the app's deploys.", err)
	}
	return specID, nil
}
