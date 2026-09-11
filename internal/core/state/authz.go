package state

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/errs"
)

// AuthzStore implements authz.Store against Postgres.
type AuthzStore struct{ db *DB }

func NewAuthzStore(db *DB) *AuthzStore { return &AuthzStore{db: db} }

// UserStatus returns a user's status, or "deleted" if no such user.
//
// A missing user resolving to "deleted" rather than an error matters for R-059:
// a delegated token whose owner was hard-removed must be orphaned, not produce a
// lookup failure that some caller might treat as transient and retry past.
func (s *AuthzStore) UserStatus(ctx context.Context, userID string) (string, error) {
	var status string
	err := s.db.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "deleted", nil
	}
	if err != nil {
		return "", errs.Wrap(errs.Internal, "Could not read the account's status.", err)
	}
	return status, nil
}

// ControlGrantsFor returns control-plane grants matching the principal.
//
// Group membership is joined live (R-079) rather than read from the principal,
// so a grant made to a group the caller joined a moment ago is honored without
// waiting for a cache.
func (s *AuthzStore) ControlGrantsFor(ctx context.Context, appID string, p authz.Principal) ([]authz.Grant, error) {
	rows, err := s.db.Query(ctx, `
		SELECT g.id, g.app_id, g.plane, g.principal_kind, coalesce(g.principal_id, ''), coalesce(g.role_id, '')
		FROM grants g
		WHERE g.app_id = $1
		  AND g.plane = 'control'
		  AND (
		        (g.principal_kind = 'user'  AND g.principal_id = $2)
		     OR (g.principal_kind = 'token' AND g.principal_id = $3)
		     OR (g.principal_kind = 'group' AND g.principal_id IN (
		            SELECT group_id FROM group_members WHERE user_id = $2))
		  )`,
		appID, nullable(p.UserID), nullable(accountTokenID(p)))
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read permissions for this app.", err)
	}
	defer rows.Close()

	var out []authz.Grant
	for rows.Next() {
		var g authz.Grant
		if err := rows.Scan(&g.ID, &g.AppID, &g.Plane, &g.PrincipalKind, &g.PrincipalID, &g.RoleID); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read permissions for this app.", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// IsOwner reports whether userID is the app's owner of record (R-031).
func (s *AuthzStore) IsOwner(ctx context.Context, appID, userID string) (bool, error) {
	if userID == "" {
		return false, nil
	}
	var owner bool
	err := s.db.QueryRow(ctx,
		`SELECT owner_user_id = $2 FROM apps WHERE id = $1 AND deleted_at IS NULL`,
		appID, userID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errs.Wrap(errs.Internal, "Could not read the app's owner.", err)
	}
	return owner, nil
}

// HasDataGrant reports whether the principal may use the app.
func (s *AuthzStore) HasDataGrant(ctx context.Context, appID string, p authz.Principal) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM grants g
			WHERE g.app_id = $1
			  AND g.plane = 'data'
			  AND (
			        (g.principal_kind = 'user'  AND g.principal_id = $2)
			     OR (g.principal_kind = 'token' AND g.principal_id = $3)
			     OR (g.principal_kind = 'group' AND g.principal_id IN (
			            SELECT group_id FROM group_members WHERE user_id = $2))
			  )
		)`, appID, nullable(p.UserID), nullable(accountTokenID(p))).Scan(&exists)
	if err != nil {
		return false, errs.Wrap(errs.Internal, "Could not read access for this app.", err)
	}
	return exists, nil
}

// HasAnonymousGrant reports whether the app is shared with everyone (R-075).
func (s *AuthzStore) HasAnonymousGrant(ctx context.Context, appID string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM grants
			WHERE app_id = $1 AND plane = 'data' AND principal_kind = 'anonymous'
		)`, appID).Scan(&exists)
	if err != nil {
		return false, errs.Wrap(errs.Internal, "Could not read access for this app.", err)
	}
	return exists, nil
}

// Role returns a role by ID.
func (s *AuthzStore) Role(ctx context.Context, roleID string) (authz.Role, error) {
	var r authz.Role
	var verbs []string
	err := s.db.QueryRow(ctx,
		`SELECT id, name, builtin, verbs FROM roles WHERE id = $1`, roleID).
		Scan(&r.ID, &r.Name, &r.Builtin, &verbs)
	if errors.Is(err, pgx.ErrNoRows) {
		// A grant referencing a missing role grants nothing. The FK makes this
		// unreachable; returning an empty role rather than an error keeps a
		// data problem from becoming an outage on the authorization path.
		return authz.Role{ID: roleID}, nil
	}
	if err != nil {
		return authz.Role{}, errs.Wrap(errs.Internal, "Could not read the role.", err)
	}
	for _, v := range verbs {
		r.Verbs = append(r.Verbs, authz.Verb(v))
	}
	return r, nil
}

// GroupsForUser resolves group membership live (R-079).
func (s *AuthzStore) GroupsForUser(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT group_id FROM group_members WHERE user_id = $1`, userID)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read group membership.", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read group membership.", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// accountTokenID returns the principal's own ID when it is an account token.
//
// A delegated token must never match a grant under its own ID — it holds no
// grants and resolves entirely through its owner (R-058, R-059). Returning empty
// here is what keeps that true at the query level rather than by convention.
func accountTokenID(p authz.Principal) string {
	if p.Kind == authz.KindToken && p.UserID == "" {
		return p.ID
	}
	return ""
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var _ authz.Store = (*AuthzStore)(nil)
