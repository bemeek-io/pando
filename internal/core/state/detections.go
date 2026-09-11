package state

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bemeek-io/pando/internal/errs"
)

// Detection statuses. The first is Pando's; the rest come from the auction.
const (
	DetectionRunning      = "running"
	DetectionReady        = "ready"
	DetectionNeedsAnswers = "needs_answers"
	DetectionUnknown      = "unknown"
	DetectionBlocked      = "blocked"
	DetectionFailed       = "failed"
)

// Detection is an app's current proposal.
type Detection struct {
	AppID     string            `json:"app_id"`
	Status    string            `json:"status"`
	Body      json.RawMessage   `json:"body"`
	Answers   map[string]string `json:"answers"`
	Commit    string            `json:"commit,omitempty"`
	StartedAt time.Time         `json:"started_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Detections stores detection proposals.
type Detections struct{ db *DB }

// NewDetections returns a store over db.
func NewDetections(db *DB) *Detections { return &Detections{db: db} }

// Start marks detection as running for an app.
//
// Answers are preserved across a re-run. Someone who answered "which service is
// primary" should not be asked again because detection was re-run for an
// unrelated reason (R-022 makes re-detection explicit, not free).
func (d *Detections) Start(ctx context.Context, appID string) error {
	_, err := d.db.Exec(ctx, `
		INSERT INTO detections (app_id, status, body, started_at, updated_at)
		VALUES ($1, $2, '{}'::jsonb, now(), now())
		ON CONFLICT (app_id) DO UPDATE
		SET status = EXCLUDED.status, body = '{}'::jsonb, started_at = now(), updated_at = now()
	`, appID, DetectionRunning)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not start detection.", err)
	}
	return nil
}

// Save records the outcome of a detection run.
func (d *Detections) Save(ctx context.Context, appID, status string, body any, commit string) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the detection result.", err)
	}

	_, err = d.db.Exec(ctx, `
		INSERT INTO detections (app_id, status, body, commit, updated_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), now())
		ON CONFLICT (app_id) DO UPDATE
		SET status = EXCLUDED.status, body = EXCLUDED.body,
		    commit = EXCLUDED.commit, updated_at = now()
	`, appID, status, encoded, commit)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the detection result.", err)
	}
	return nil
}

// Get returns an app's current detection.
func (d *Detections) Get(ctx context.Context, appID string) (Detection, error) {
	var out Detection
	var answers []byte
	var commit *string

	err := d.db.QueryRow(ctx, `
		SELECT app_id, status, body, answers, commit, started_at, updated_at
		FROM detections WHERE app_id = $1
	`, appID).Scan(&out.AppID, &out.Status, &out.Body, &answers, &commit, &out.StartedAt, &out.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return Detection{}, errs.New(errs.NotFound, "This app has not been through detection yet.").
			WithRemedy("Run detection on the app first.")
	}
	if err != nil {
		return Detection{}, errs.Wrap(errs.Internal, "Could not read the detection result.", err)
	}

	out.Answers = map[string]string{}
	if len(answers) > 0 {
		_ = json.Unmarshal(answers, &out.Answers)
	}
	if commit != nil {
		out.Commit = *commit
	}
	return out, nil
}

// SaveAnswers merges answers into an app's detection.
//
// Merged rather than replaced: the console posts answers as they are given,
// which is the workflow R-105 describes — one question at a time, pasted into
// an assistant and pasted back.
func (d *Detections) SaveAnswers(ctx context.Context, appID string, answers map[string]string) error {
	if len(answers) == 0 {
		return nil
	}
	encoded, err := json.Marshal(answers)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the answers.", err)
	}

	tag, err := d.db.Exec(ctx, `
		UPDATE detections SET answers = answers || $2::jsonb, updated_at = now()
		WHERE app_id = $1
	`, appID, encoded)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the answers.", err)
	}
	if tag.RowsAffected() == 0 {
		return errs.New(errs.NotFound, "This app has not been through detection yet.").
			WithRemedy("Run detection on the app first.")
	}
	return nil
}
