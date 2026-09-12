package state

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bemeek-io/pando/internal/core/backup"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// Backups records what has been backed up and where (design 02 §2.8).
type Backups struct{ db *DB }

func NewBackups(db *DB) *Backups { return &Backups{db: db} }

// Backup is one stored backup.
type Backup struct {
	ID         string `json:"id"`
	AppID      string `json:"app_id,omitempty"`
	Kind       string `json:"kind"`
	AdapterRef string `json:"adapter_ref"`
	ObjectName string `json:"object_name"`
	SizeBytes  int64  `json:"size_bytes"`

	Manifest backup.Manifest `json:"manifest"`

	// RetainUntil is nil when the backup is kept until explicitly discarded:
	// always for on_delete (R-204), and for any kind whose destination owns
	// retention itself (R-217).
	RetainUntil *time.Time `json:"retain_until,omitempty"`

	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// NewID returns an ID for a backup about to be written.
//
// Allocated before the bundle is built, because the ID is the object name at
// the destination — so a crash mid-write leaves an object nothing references
// rather than a row pointing at nothing. The first is garbage; the second is a
// restore that cannot find its bundle.
func (b *Backups) NewID() string { return id.New(id.Backup) }

// Record stores a backup that has been fully written.
//
// Called after the destination's Close returns, never before. A row written
// first would claim a bundle exists during the window where it does not, and
// that window is exactly when a disaster is most likely to interrupt.
func (b *Backups) Record(ctx context.Context, rec Backup) error {
	manifest, err := json.Marshal(rec.Manifest)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not record the backup.", err)
	}

	_, err = b.db.Exec(ctx, `
		INSERT INTO backups (id, app_id, kind, adapter_ref, object_name, size_bytes,
		                     manifest, retain_until, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		rec.ID, nullable(rec.AppID), rec.Kind, rec.AdapterRef, rec.ObjectName,
		rec.SizeBytes, manifest, rec.RetainUntil, rec.CreatedBy)
	if err != nil {
		if isUniqueViolation(err) {
			return errs.New(errs.Internal, "A backup with that name already exists at this destination.")
		}
		return errs.Wrap(errs.Internal, "Could not record the backup.", err)
	}
	return nil
}

// ByID returns one backup.
func (b *Backups) ByID(ctx context.Context, backupID string) (Backup, bool, error) {
	row := b.db.QueryRow(ctx, `
		SELECT id, coalesce(app_id, ''), kind, adapter_ref, object_name,
		       coalesce(size_bytes, 0), manifest, retain_until, created_by, created_at
		FROM backups WHERE id = $1`, backupID)

	rec, err := scanBackup(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Backup{}, false, nil
	}
	if err != nil {
		return Backup{}, false, errs.Wrap(errs.Internal, "Could not read the backup.", err)
	}
	return rec, true, nil
}

// List returns backups, newest first. An empty appID lists every backup.
func (b *Backups) List(ctx context.Context, appID string) ([]Backup, error) {
	var rows pgx.Rows
	var err error

	const columns = `SELECT id, coalesce(app_id, ''), kind, adapter_ref, object_name,
	                        coalesce(size_bytes, 0), manifest, retain_until, created_by, created_at
	                 FROM backups`
	if appID == "" {
		rows, err = b.db.Query(ctx, columns+` ORDER BY created_at DESC`)
	} else {
		rows, err = b.db.Query(ctx, columns+` WHERE app_id = $1 ORDER BY created_at DESC`, appID)
	}
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read the backups.", err)
	}
	defer rows.Close()

	out := make([]Backup, 0)
	for rows.Next() {
		rec, err := scanBackup(rows)
		if err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read the backups.", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Expired returns backups whose retention has passed (R-211).
//
// Never returns an on_delete row: those have no retain_until at all, and the
// schema refuses to give them one. The WHERE clause says so anyway, because a
// pruning query that relies on a constraint elsewhere is one migration away
// from deleting the backups R-204 promises to keep.
func (b *Backups) Expired(ctx context.Context, now time.Time) ([]Backup, error) {
	rows, err := b.db.Query(ctx, `
		SELECT id, coalesce(app_id, ''), kind, adapter_ref, object_name,
		       coalesce(size_bytes, 0), manifest, retain_until, created_by, created_at
		FROM backups
		WHERE kind <> 'on_delete' AND retain_until IS NOT NULL AND retain_until < $1
		ORDER BY retain_until`, now)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read expired backups.", err)
	}
	defer rows.Close()

	out := make([]Backup, 0)
	for rows.Next() {
		rec, err := scanBackup(rows)
		if err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read expired backups.", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Forget removes the record of a backup whose object has been deleted.
//
// The object goes first, then this. The other order leaves an object nothing
// references, which is invisible and accumulates.
func (b *Backups) Forget(ctx context.Context, backupID string) error {
	_, err := b.db.Exec(ctx, `DELETE FROM backups WHERE id = $1`, backupID)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not discard the backup.", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanBackup(row scanner) (Backup, error) {
	var rec Backup
	var manifest []byte
	if err := row.Scan(&rec.ID, &rec.AppID, &rec.Kind, &rec.AdapterRef, &rec.ObjectName,
		&rec.SizeBytes, &manifest, &rec.RetainUntil, &rec.CreatedBy, &rec.CreatedAt); err != nil {
		return Backup{}, err
	}
	if len(manifest) > 0 {
		if err := json.Unmarshal(manifest, &rec.Manifest); err != nil {
			return Backup{}, err
		}
	}
	return rec, nil
}
