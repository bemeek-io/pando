package state

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// App lifecycle states (design 05 §1.1).
const (
	StateDraft     = "draft"
	StateProposed  = "proposed"
	StateDeploying = "deploying"
	StateRunning   = "running"
	StateDegraded  = "degraded"
	StateStopped   = "stopped"
	StateFailed    = "failed"
	StateArchived  = "archived"
)

// App is a deployable unit.
type App struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Slug         string     `json:"slug"`
	OwnerUserID  string     `json:"owner_user_id"`
	State        string     `json:"state"`
	DesiredState string     `json:"desired_state"`
	PinnedSpecID string     `json:"pinned_spec_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

// Apps stores apps and their spec revisions.
type Apps struct{ db *DB }

func NewApps(db *DB) *Apps { return &Apps{db: db} }

// Create inserts an app and its two grants in one transaction.
//
// R-073: app creation writes two grant rows, one per plane, independently
// revocable. They are written here rather than by the handler so that an app
// cannot exist without them — a control grant without a data grant would leave
// the owner able to manage an app they cannot open.
func (a *Apps) Create(ctx context.Context, name, slug, ownerUserID, createdBy string) (App, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return App{}, errs.Wrap(errs.Internal, "Could not create the app.", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	app := App{
		ID:           id.New(id.App),
		Name:         name,
		Slug:         slug,
		OwnerUserID:  ownerUserID,
		State:        StateDraft,
		DesiredState: "stopped",
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO apps (id, name, slug, owner_user_id, state, desired_state)
		VALUES ($1, $2, $3, $4, 'draft', 'stopped')
		RETURNING created_at, updated_at`,
		app.ID, name, slug, ownerUserID).Scan(&app.CreatedAt, &app.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return App{}, errs.Newf(errs.ValidInvalid, "An app named %q already exists.", name).
				WithRemedy("Choose a different name.")
		}
		return App{}, errs.Wrap(errs.Internal, "Could not create the app.", err)
	}

	for _, g := range []struct {
		plane  string
		roleID any
	}{
		{"control", authz.RoleOwner},
		{"data", nil},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO grants (id, app_id, plane, principal_kind, principal_id, role_id, created_by)
			VALUES ($1, $2, $3, 'user', $4, $5, $6)`,
			id.New(id.Grant), app.ID, g.plane, ownerUserID, g.roleID, createdBy)
		if err != nil {
			return App{}, errs.Wrap(errs.Internal, "Could not set up access for the app.", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return App{}, errs.Wrap(errs.Internal, "Could not create the app.", err)
	}
	return app, nil
}

// ByID returns an app.
func (a *Apps) ByID(ctx context.Context, appID string) (App, bool, error) {
	var app App
	var owner, pinned *string
	err := a.db.QueryRow(ctx, `
		SELECT id, name, slug, owner_user_id, state, desired_state, pinned_spec_id, created_at, updated_at, deleted_at
		FROM apps WHERE id = $1 AND deleted_at IS NULL`, appID).
		Scan(&app.ID, &app.Name, &app.Slug, &owner, &app.State, &app.DesiredState, &pinned,
			&app.CreatedAt, &app.UpdatedAt, &app.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return App{}, false, nil
	}
	if err != nil {
		return App{}, false, errs.Wrap(errs.Internal, "Could not read the app.", err)
	}
	if owner != nil {
		app.OwnerUserID = *owner
	}
	if pinned != nil {
		app.PinnedSpecID = *pinned
	}
	return app, true, nil
}

// ListForPrincipal returns apps the principal can see on the control plane.
//
// Scoped by grant rather than returning everything and filtering afterward: a
// list endpoint that leaks the existence of apps is a smaller problem than one
// that leaks their contents, but it is still a leak.
func (a *Apps) ListForPrincipal(ctx context.Context, p authz.Principal) ([]App, error) {
	rows, err := a.db.Query(ctx, `
		SELECT DISTINCT a.id, a.name, a.slug, a.owner_user_id, a.state, a.desired_state,
		       a.pinned_spec_id, a.created_at, a.updated_at
		FROM apps a
		JOIN grants g ON g.app_id = a.id AND g.plane = 'control'
		WHERE a.deleted_at IS NULL
		  AND (
		        (g.principal_kind = 'user'  AND g.principal_id = $1)
		     OR (g.principal_kind = 'token' AND g.principal_id = $2)
		     OR (g.principal_kind = 'group' AND g.principal_id IN (
		            SELECT group_id FROM group_members WHERE user_id = $1))
		  )
		ORDER BY a.created_at DESC`,
		nullable(p.UserID), nullable(accountTokenID(p)))
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not list apps.", err)
	}
	defer rows.Close()

	var out []App
	for rows.Next() {
		var app App
		var owner, pinned *string
		if err := rows.Scan(&app.ID, &app.Name, &app.Slug, &owner, &app.State, &app.DesiredState,
			&pinned, &app.CreatedAt, &app.UpdatedAt); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not list apps.", err)
		}
		if owner != nil {
			app.OwnerUserID = *owner
		}
		if pinned != nil {
			app.PinnedSpecID = *pinned
		}
		out = append(out, app)
	}
	return out, rows.Err()
}

// ListForUse returns apps the principal holds a DATA-plane grant on (R-264).
//
// Deliberately a different query from ListForPrincipal. Two planes, two
// endpoints: the launcher shows what you can open, not what you can manage.
func (a *Apps) ListForUse(ctx context.Context, p authz.Principal) ([]App, error) {
	rows, err := a.db.Query(ctx, `
		SELECT DISTINCT a.id, a.name, a.slug, a.state
		FROM apps a
		LEFT JOIN grants g ON g.app_id = a.id AND g.plane = 'data'
		WHERE a.deleted_at IS NULL
		  AND (
		        a.owner_user_id = $1
		     OR g.principal_kind = 'anonymous'
		     OR (g.principal_kind = 'user'  AND g.principal_id = $1)
		     OR (g.principal_kind = 'token' AND g.principal_id = $2)
		     OR (g.principal_kind = 'group' AND g.principal_id IN (
		            SELECT group_id FROM group_members WHERE user_id = $1))
		  )
		ORDER BY a.name`,
		nullable(p.UserID), nullable(accountTokenID(p)))
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not list apps.", err)
	}
	defer rows.Close()

	var out []App
	for rows.Next() {
		var app App
		if err := rows.Scan(&app.ID, &app.Name, &app.Slug, &app.State); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not list apps.", err)
		}
		out = append(out, app)
	}
	return out, rows.Err()
}

// Rename changes an app's display name.
func (a *Apps) Rename(ctx context.Context, appID, name string) error {
	_, err := a.db.Exec(ctx,
		`UPDATE apps SET name = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, appID, name)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not rename the app.", err)
	}
	return nil
}

// Archive soft-deletes an app.
//
// Soft delete because the audit log references it, and an audit trail pointing
// at a row that no longer exists answers fewer questions than one that does.
// Volumes are ON DELETE RESTRICT and must be resolved first (R-204) — the caller
// handles the keep-or-discard decision.
func (a *Apps) Archive(ctx context.Context, appID string) error {
	_, err := a.db.Exec(ctx, `
		UPDATE apps SET state = 'archived', desired_state = 'stopped', deleted_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`, appID)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not delete the app.", err)
	}
	return nil
}

// VolumeCount returns how many volumes an app holds, for the delete decision.
func (a *Apps) VolumeCount(ctx context.Context, appID string) (int, error) {
	var n int
	if err := a.db.QueryRow(ctx, `SELECT count(*) FROM volumes WHERE app_id = $1`, appID).Scan(&n); err != nil {
		return 0, errs.Wrap(errs.Internal, "Could not read the app's storage.", err)
	}
	return n, nil
}

// --- spec revisions --------------------------------------------------------

// Revision is a stored spec revision.
type Revision struct {
	ID        string        `json:"id"`
	AppID     string        `json:"app_id"`
	Revision  int           `json:"revision"`
	Origin    spec.Origin   `json:"origin"`
	Body      *spec.AppSpec `json:"body"`
	CreatedBy string        `json:"created_by"`
	CreatedAt time.Time     `json:"created_at"`

	// EverPinned is derived from spec_pins, not stored on the revision —
	// spec_revisions is append-only and a mutable column on it would be a hole
	// in that guarantee.
	EverPinned bool `json:"ever_pinned"`
}

// CreateRevision appends a spec revision.
//
// The revision number is assigned inside the transaction from the current
// maximum, so two concurrent edits cannot both claim the same number — the
// UNIQUE (app_id, revision) constraint is the backstop, and this is what keeps
// it from being hit in normal operation.
func (a *Apps) CreateRevision(ctx context.Context, appID string, s *spec.AppSpec, origin spec.Origin, createdBy string) (Revision, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return Revision{}, errs.Wrap(errs.Internal, "Could not save the spec.", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var next int
	if err := tx.QueryRow(ctx,
		`SELECT coalesce(max(revision), 0) + 1 FROM spec_revisions WHERE app_id = $1`, appID).Scan(&next); err != nil {
		return Revision{}, errs.Wrap(errs.Internal, "Could not save the spec.", err)
	}

	s.SchemaVersion = spec.SchemaVersion
	s.AppID = appID
	s.Revision = next
	s.Origin = origin
	s.CreatedBy = createdBy
	s.CreatedAt = time.Now().UTC()

	body, err := json.Marshal(s)
	if err != nil {
		return Revision{}, errs.Wrap(errs.Internal, "Could not encode the spec.", err)
	}

	rev := Revision{
		ID:        id.New(id.Spec),
		AppID:     appID,
		Revision:  next,
		Origin:    origin,
		Body:      s,
		CreatedBy: createdBy,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO spec_revisions (id, app_id, revision, origin, body, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at`,
		rev.ID, appID, next, string(origin), body, createdBy).Scan(&rev.CreatedAt); err != nil {
		return Revision{}, errs.Wrap(errs.Internal, "Could not save the spec.", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Revision{}, errs.Wrap(errs.Internal, "Could not save the spec.", err)
	}
	return rev, nil
}

// RevisionByID returns one revision.
func (a *Apps) RevisionByID(ctx context.Context, specID string) (Revision, bool, error) {
	return a.scanRevision(ctx, `WHERE id = $1`, specID)
}

// RevisionByNumber returns an app's revision by its number.
func (a *Apps) RevisionByNumber(ctx context.Context, appID string, number int) (Revision, bool, error) {
	return a.scanRevision(ctx, `WHERE app_id = $1 AND revision = $2`, appID, number)
}

func (a *Apps) scanRevision(ctx context.Context, where string, args ...any) (Revision, bool, error) {
	var rev Revision
	var body []byte
	var origin string
	err := a.db.QueryRow(ctx, `
		SELECT r.id, r.app_id, r.revision, r.origin, r.body, r.created_by, r.created_at,
		       EXISTS (SELECT 1 FROM spec_pins p WHERE p.spec_id = r.id)
		FROM spec_revisions r `+where,
		args...).
		Scan(&rev.ID, &rev.AppID, &rev.Revision, &origin, &body, &rev.CreatedBy, &rev.CreatedAt, &rev.EverPinned)
	if errors.Is(err, pgx.ErrNoRows) {
		return Revision{}, false, nil
	}
	if err != nil {
		return Revision{}, false, errs.Wrap(errs.Internal, "Could not read the spec.", err)
	}

	rev.Origin = spec.Origin(origin)
	var s spec.AppSpec
	if err := json.Unmarshal(body, &s); err != nil {
		return Revision{}, false, errs.Wrap(errs.Internal, "Could not read the stored spec.", err)
	}
	rev.Body = &s
	return rev, true, nil
}

// ListRevisions returns an app's revisions, newest first.
func (a *Apps) ListRevisions(ctx context.Context, appID string) ([]Revision, error) {
	rows, err := a.db.Query(ctx, `
		SELECT r.id, r.app_id, r.revision, r.origin, r.created_by, r.created_at,
		       EXISTS (SELECT 1 FROM spec_pins p WHERE p.spec_id = r.id)
		FROM spec_revisions r WHERE r.app_id = $1 ORDER BY r.revision DESC`, appID)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not list the app's specs.", err)
	}
	defer rows.Close()

	var out []Revision
	for rows.Next() {
		var rev Revision
		var origin string
		if err := rows.Scan(&rev.ID, &rev.AppID, &rev.Revision, &origin, &rev.CreatedBy, &rev.CreatedAt, &rev.EverPinned); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not list the app's specs.", err)
		}
		rev.Origin = spec.Origin(origin)
		out = append(out, rev)
	}
	return out, rows.Err()
}

// Pin points the app at a revision.
//
// Records the pinning as an event in the same transaction, because retention
// pruning must never remove a revision that was once live (R-152) and that fact
// has to survive the pointer moving on during a rollback. The event log doubles
// as the rollback history.
func (a *Apps) Pin(ctx context.Context, appID, specID, newState, pinnedBy string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not pin the spec.", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO spec_pins (app_id, spec_id, pinned_by) VALUES ($1, $2, $3)`,
		appID, specID, pinnedBy); err != nil {
		return errs.Wrap(errs.Internal, "Could not pin the spec.", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE apps SET pinned_spec_id = $2, state = $3, updated_at = now() WHERE id = $1`,
		appID, specID, newState); err != nil {
		return errs.Wrap(errs.Internal, "Could not pin the spec.", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return errs.Wrap(errs.Internal, "Could not pin the spec.", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}

// Volumes records storage attached to apps.
type Volumes struct{ db *DB }

func NewVolumes(db *DB) *Volumes { return &Volumes{db: db} }

// DeleteForApp removes an app's volume rows.
//
// Only reached once the caller has resolved the keep-or-discard decision
// (R-204). The ON DELETE RESTRICT on volumes.app_id means an app cannot be
// archived while rows remain, so this is the explicit step that constraint
// forces — which is exactly why the constraint is there.
func (v *Volumes) DeleteForApp(ctx context.Context, appID string) error {
	if _, err := v.db.Exec(ctx, `DELETE FROM volumes WHERE app_id = $1`, appID); err != nil {
		return errs.Wrap(errs.Internal, "Could not remove the app's storage.", err)
	}
	return nil
}

// Create records a volume.
func (v *Volumes) Create(ctx context.Context, appID, name, adapterRef string) (string, error) {
	volumeID := id.New(id.Volume)
	_, err := v.db.Exec(ctx,
		`INSERT INTO volumes (id, app_id, name, adapter_ref) VALUES ($1, $2, $3, $4)`,
		volumeID, appID, name, adapterRef)
	if err != nil {
		return "", errs.Wrap(errs.Internal, "Could not add the storage.", err)
	}
	return volumeID, nil
}

// SetDesiredState records what a human asked for.
//
// Separate from state, which is what is true. The reconciler reads both: the
// gap between them is the work it has to do.
func (a *Apps) SetDesiredState(ctx context.Context, appID, desired string) error {
	switch desired {
	case "running", "stopped":
	default:
		return errs.New(errs.ValidInvalid, "An app is either running or stopped.")
	}
	_, err := a.db.Exec(ctx,
		`UPDATE apps SET desired_state = $2, updated_at = now() WHERE id = $1`, appID, desired)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not update the app.", err)
	}
	return nil
}

// SetState records what is observed to be true.
func (a *Apps) SetState(ctx context.Context, appID, appState string) error {
	_, err := a.db.Exec(ctx,
		`UPDATE apps SET state = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, appID, appState)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not update the app.", err)
	}
	return nil
}

// ByRouting resolves a running app from how it is addressed.
//
// Used by the proxy on every request, so it reads the pinned spec in the same
// query rather than making a second round trip per request.
//
// `by` is "hostname" or "slug". A hostname lookup reads the pinned spec's
// routing block; a slug lookup reads the app's own column.
func (a *Apps) ByRouting(ctx context.Context, by, value string) (App, *spec.AppSpec, bool, error) {
	var where string
	switch by {
	case "hostname":
		where = `r.body->'routing'->>'hostname' = $1`
	case "slug":
		where = `a.slug = $1`
	default:
		return App{}, nil, false, errs.Newf(errs.Internal, "Unknown routing lookup %q.", by)
	}

	var app App
	var owner *string
	var body []byte
	err := a.db.QueryRow(ctx, `
		SELECT a.id, a.name, a.slug, a.owner_user_id, a.state, a.desired_state, a.pinned_spec_id,
		       a.created_at, a.updated_at, r.body
		FROM apps a
		JOIN spec_revisions r ON r.id = a.pinned_spec_id
		WHERE a.deleted_at IS NULL AND `+where, value).
		Scan(&app.ID, &app.Name, &app.Slug, &owner, &app.State, &app.DesiredState, &app.PinnedSpecID,
			&app.CreatedAt, &app.UpdatedAt, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return App{}, nil, false, nil
	}
	if err != nil {
		return App{}, nil, false, errs.Wrap(errs.Internal, "Could not look up the app.", err)
	}
	if owner != nil {
		app.OwnerUserID = *owner
	}

	var s spec.AppSpec
	if err := json.Unmarshal(body, &s); err != nil {
		return App{}, nil, false, errs.Wrap(errs.Internal, "Could not read the app's spec.", err)
	}
	return app, &s, true, nil
}
