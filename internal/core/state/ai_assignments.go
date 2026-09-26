package state

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bemeek-io/pando/internal/errs"
)

// AIAssignment is one stored assignment of an AI function to an adapter
// (R-259).
type AIAssignment struct {
	Function  string    `json:"function"`
	AdapterID string    `json:"adapter_id"`
	Model     string    `json:"model,omitempty"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AIAssignments reads and writes ai_assignments.
type AIAssignments struct{ db *DB }

func NewAIAssignments(db *DB) *AIAssignments { return &AIAssignments{db: db} }

// List returns every stored assignment, by function.
func (s *AIAssignments) List(ctx context.Context) ([]AIAssignment, error) {
	rows, err := s.db.Query(ctx, `
		SELECT function, adapter_id, model, updated_by, updated_at
		FROM ai_assignments ORDER BY function`)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read which AI adapter handles each function.", err)
	}
	defer rows.Close()

	var out []AIAssignment
	for rows.Next() {
		var a AIAssignment
		if err := rows.Scan(&a.Function, &a.AdapterID, &a.Model, &a.UpdatedBy, &a.UpdatedAt); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read which AI adapter handles each function.", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ErrAssigned is returned by Put when another adapter holds the function.
// Held names that adapter.
type ErrAssigned struct{ Held AIAssignment }

func (e ErrAssigned) Error() string {
	return "function " + e.Held.Function + " is handled by " + e.Held.AdapterID
}

// Put assigns a function to an adapter, or changes the model of an existing
// assignment to the same adapter.
//
// A function another adapter already handles is refused, and the refusal is
// the database's: the primary key on function admits one row, and the
// conflict clause updates that row only when it names the same adapter. No
// row back means a different adapter holds it, which is ErrAssigned — never a
// silent move from one adapter to another.
func (s *AIAssignments) Put(ctx context.Context, a AIAssignment) error {
	var got string
	err := s.db.QueryRow(ctx, `
		INSERT INTO ai_assignments (function, adapter_id, model, updated_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (function) DO UPDATE SET
			model = EXCLUDED.model, updated_by = EXCLUDED.updated_by, updated_at = now()
		WHERE ai_assignments.adapter_id = EXCLUDED.adapter_id
		RETURNING adapter_id`,
		a.Function, a.AdapterID, a.Model, a.UpdatedBy).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		held, found, gerr := s.Get(ctx, a.Function)
		if gerr != nil {
			return gerr
		}
		if !found {
			// Removed between the two statements. Not worth a retry loop: the
			// caller can ask again, and this time it will be free.
			return errs.New(errs.StateInvalid, "That AI function changed while it was being assigned. Try again.")
		}
		return ErrAssigned{Held: held}
	}
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not save the AI function's adapter.", err)
	}
	return nil
}

// Get returns one function's stored assignment.
func (s *AIAssignments) Get(ctx context.Context, function string) (AIAssignment, bool, error) {
	var a AIAssignment
	err := s.db.QueryRow(ctx, `
		SELECT function, adapter_id, model, updated_by, updated_at
		FROM ai_assignments WHERE function = $1`, function).
		Scan(&a.Function, &a.AdapterID, &a.Model, &a.UpdatedBy, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AIAssignment{}, false, nil
	}
	if err != nil {
		return AIAssignment{}, false, errs.Wrap(errs.Internal, "Could not read the AI function's adapter.", err)
	}
	return a, true, nil
}

// Delete removes a function's assignment, turning the function off. Removing
// one that is not assigned is not an error: the function is off either way.
func (s *AIAssignments) Delete(ctx context.Context, function string) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM ai_assignments WHERE function = $1`, function); err != nil {
		return errs.Wrap(errs.Internal, "Could not remove the AI function's adapter.", err)
	}
	return nil
}
