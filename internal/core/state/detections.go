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

// AbandonRunning fails every detection still marked running, and is called at
// startup, before anything new can start one.
//
// Detection runs inside the server process. One that was running when the
// process stopped will never finish, and it stayed "running" for good: a
// console spinning forever, and a client polling for an answer that could not
// come (issue #55). Failing it says what happened and lets it be run again.
func (d *Detections) AbandonRunning(ctx context.Context) (int64, error) {
	body, err := json.Marshal(map[string]any{"error": errs.New(errs.StateInvalid,
		"Pando restarted while it was working out how to run this app, so that work did not finish.").
		WithRemedy("Run detection again.")})
	if err != nil {
		return 0, errs.Wrap(errs.Internal, "Could not record interrupted detections.", err)
	}
	tag, err := d.db.Exec(ctx, `
		UPDATE detections SET status = $1, body = $2, updated_at = now()
		WHERE status = $3`, DetectionFailed, body, DetectionRunning)
	if err != nil {
		return 0, errs.Wrap(errs.Internal, "Could not record interrupted detections.", err)
	}
	return tag.RowsAffected(), nil
}

// FailIfRunning records a failure for a detection that is still marked running,
// and leaves one that has already recorded its own outcome alone.
func (d *Detections) FailIfRunning(ctx context.Context, appID string, cause error) error {
	recorded := any(map[string]any{"message": cause.Error()})
	if e := errs.As(cause); e != nil {
		recorded = e
	}
	body, err := json.Marshal(map[string]any{"error": recorded})
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the detection.", err)
	}
	_, err = d.db.Exec(ctx, `
		UPDATE detections SET status = $2, body = $3, updated_at = now()
		WHERE app_id = $1 AND status = $4`, appID, DetectionFailed, body, DetectionRunning)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the detection.", err)
	}
	return nil
}

// Save records the outcome of a detection run.
func (d *Detections) Save(ctx context.Context, appID, status string, body any, commit string) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the detection result.", err)
	}
	encoded = withoutNUL(encoded)

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
