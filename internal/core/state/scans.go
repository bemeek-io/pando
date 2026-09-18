package state

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// Security scans (R-310 – R-319, design 09 §3).
//
// A scan belongs to a spec revision rather than to an app, which is what makes
// a rollback restore the score of what it rolled back to with no rescan: the
// row for that revision is still here.

// Scan is one scanner's answer about one revision.
type Scan struct {
	ID         string `json:"id"`
	AppID      string `json:"app_id"`
	SpecID     string `json:"spec_id,omitempty"`
	ScannerRef string `json:"scanner_ref"`
	Scanner    string `json:"scanner,omitempty"`
	Score      *int   `json:"score"`

	// ScoreFixable counts only findings with a fix available. Which of the two
	// an installation means is host policy's (R-313); both are stored because
	// policy changes without rescanning.
	ScoreFixable *int          `json:"score_fixable,omitempty"`
	Findings     []api.Finding `json:"findings"`
	Error        string        `json:"error,omitempty"`
	RanAt        time.Time     `json:"ran_at"`
}

// Scans stores them.
type Scans struct{ db *DB }

func NewScans(db *DB) *Scans { return &Scans{db: db} }

// Record writes a scan.
//
// Every scan, including a failed one. A scanner that could not run is a fact
// somebody has to see — silence would leave an unscannable app looking clean
// (R-318).
func (s *Scans) Record(ctx context.Context, scan Scan) (Scan, error) {
	scan.ID = id.New(id.Scan)
	if scan.RanAt.IsZero() {
		scan.RanAt = time.Now().UTC()
	}
	if scan.Findings == nil {
		scan.Findings = []api.Finding{}
	}

	body, err := json.Marshal(scan.Findings)
	if err != nil {
		return Scan{}, errs.Wrap(errs.Internal, "Could not record the scan.", err)
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO app_scans (id, app_id, spec_id, scanner_ref, scanner, score, score_fixable,
		                       findings, error, ran_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		scan.ID, scan.AppID, nullable(scan.SpecID), scan.ScannerRef, scan.Scanner,
		scan.Score, scan.ScoreFixable, body, scan.Error, scan.RanAt)
	if err != nil {
		return Scan{}, errs.Wrap(errs.Internal, "Could not record the scan.", err)
	}
	return scan, nil
}

// Latest returns the scan that describes what this app is running.
//
// The newest scan of that revision, and failing that the newest scan that
// belongs to no revision — which is what detection produces, from the source,
// before there is a revision to attach it to. Never another revision's: a score
// for a spec the app is not running is not a score of what is deployed.
//
// The fallback is the difference between "scanned at discovery" meaning
// something and meaning nothing: accepting a proposal pins a revision, and
// without this the scan taken of that very source a minute earlier disappeared
// behind "this app has not been scanned yet".
func (s *Scans) Latest(ctx context.Context, appID, specID string) (Scan, bool, error) {
	query := `
		SELECT id, app_id, coalesce(spec_id, ''), scanner_ref, scanner, score, score_fixable,
		       findings, error, ran_at
		FROM app_scans
		WHERE app_id = $1 AND ($2 = '' OR spec_id = $2 OR spec_id IS NULL)
		ORDER BY (spec_id IS NOT NULL) DESC, ran_at DESC
		LIMIT 1`

	var scan Scan
	var findings []byte
	err := s.db.QueryRow(ctx, query, appID, specID).Scan(
		&scan.ID, &scan.AppID, &scan.SpecID, &scan.ScannerRef, &scan.Scanner,
		&scan.Score, &scan.ScoreFixable, &findings, &scan.Error, &scan.RanAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Scan{}, false, nil
	}
	if err != nil {
		return Scan{}, false, errs.Wrap(errs.Internal, "Could not read the app's scans.", err)
	}
	if len(findings) > 0 {
		_ = json.Unmarshal(findings, &scan.Findings)
	}
	return scan, true, nil
}

// History returns an app's scans, newest first.
func (s *Scans) History(ctx context.Context, appID string, limit int) ([]Scan, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, app_id, coalesce(spec_id, ''), scanner_ref, scanner, score, score_fixable,
		       findings, error, ran_at
		FROM app_scans
		WHERE app_id = $1
		ORDER BY ran_at DESC
		LIMIT $2`, appID, limit)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read the app's scans.", err)
	}
	defer rows.Close()

	out := make([]Scan, 0)
	for rows.Next() {
		var scan Scan
		var findings []byte
		if err := rows.Scan(&scan.ID, &scan.AppID, &scan.SpecID, &scan.ScannerRef, &scan.Scanner,
			&scan.Score, &scan.ScoreFixable, &findings, &scan.Error, &scan.RanAt); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read the app's scans.", err)
		}
		if len(findings) > 0 {
			_ = json.Unmarshal(findings, &scan.Findings)
		}
		out = append(out, scan)
	}
	return out, rows.Err()
}

// SecurityState is an app's standing against the installation's threshold.
type SecurityState struct {
	AppID              string
	InsecureSince      *time.Time
	StoppedForSecurity bool
	DesiredState       string
	State              string
	PinnedSpecID       string
	OwnerUserID        string
	Name               string
}

// LiveSecurityState returns every app the policy pass has to consider.
//
// Archived apps are not in it, and neither are drafts: an app that has never
// been deployed cannot be running below a threshold. What is in it is anything
// with a pinned revision, including a stopped one — an app Pando stopped for
// being insecure has to be looked at again to be started again.
func (s *Scans) LiveSecurityState(ctx context.Context) ([]SecurityState, error) {
	rows, err := s.db.Query(ctx, `
		SELECT a.id, a.name, coalesce(a.owner_user_id, ''), a.state, a.desired_state,
		       coalesce(a.pinned_spec_id, ''), a.insecure_since, a.stopped_for_security
		FROM apps a
		WHERE a.deleted_at IS NULL
		  AND a.pinned_spec_id IS NOT NULL
		  AND a.state <> 'archived'
		ORDER BY a.id`)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not list apps for the security pass.", err)
	}
	defer rows.Close()

	out := make([]SecurityState, 0)
	for rows.Next() {
		var row SecurityState
		if err := rows.Scan(&row.AppID, &row.Name, &row.OwnerUserID, &row.State, &row.DesiredState,
			&row.PinnedSpecID, &row.InsecureSince, &row.StoppedForSecurity); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not list apps for the security pass.", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// SecurityStateFor returns one app's standing.
func (s *Scans) SecurityStateFor(ctx context.Context, appID string) (SecurityState, bool, error) {
	var row SecurityState
	err := s.db.QueryRow(ctx, `
		SELECT a.id, a.name, coalesce(a.owner_user_id, ''), a.state, a.desired_state,
		       coalesce(a.pinned_spec_id, ''), a.insecure_since, a.stopped_for_security
		FROM apps a
		WHERE a.id = $1 AND a.deleted_at IS NULL`, appID).
		Scan(&row.AppID, &row.Name, &row.OwnerUserID, &row.State, &row.DesiredState,
			&row.PinnedSpecID, &row.InsecureSince, &row.StoppedForSecurity)
	if errors.Is(err, pgx.ErrNoRows) {
		return SecurityState{}, false, nil
	}
	if err != nil {
		return SecurityState{}, false, errs.Wrap(errs.Internal, "Could not read the app.", err)
	}
	return row, true, nil
}

// MarkInsecure records when an app was first found below the threshold.
//
// Idempotent on purpose: the pass runs every tick, and the grace period is
// measured from the first time it was true, not the most recent (R-316). A
// clock that restarted on every pass would be a grace period that never expired.
func (s *Scans) MarkInsecure(ctx context.Context, appID string, at time.Time) error {
	_, err := s.db.Exec(ctx, `
		UPDATE apps SET insecure_since = coalesce(insecure_since, $2), updated_at = now()
		WHERE id = $1`, appID, at)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the app's security state.", err)
	}
	return nil
}

// ClearInsecure records that an app is back above the threshold.
func (s *Scans) ClearInsecure(ctx context.Context, appID string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE apps SET insecure_since = NULL, updated_at = now()
		WHERE id = $1`, appID)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the app's security state.", err)
	}
	return nil
}

// SetStoppedForSecurity records whether Pando is the reason this app is stopped.
//
// The difference decides whether a recovered score starts it again: an app its
// owner stopped stays stopped (design 09 §4.2).
func (s *Scans) SetStoppedForSecurity(ctx context.Context, appID string, stopped bool) error {
	_, err := s.db.Exec(ctx, `
		UPDATE apps SET stopped_for_security = $2, updated_at = now()
		WHERE id = $1`, appID, stopped)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the app's security state.", err)
	}
	return nil
}
