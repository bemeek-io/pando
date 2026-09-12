package state

import (
	"context"
	"time"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// Notifications stores console notifications (R-231).
//
// Implements the console notify adapter's Sink. The adapter holds no storage of
// its own, which is R-027: an adapter never touches state, so core hands it a
// writer rather than a database.
type Notifications struct{ db *DB }

func NewNotifications(db *DB) *Notifications { return &Notifications{db: db} }

// Notification is one stored message.
type Notification struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	AppID     string     `json:"app_id,omitempty"`
	Kind      string     `json:"kind"`
	Subject   string     `json:"subject"`
	Body      string     `json:"body,omitempty"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Record writes one row per recipient.
//
// Per recipient rather than one row with a list, so "mark as read" is a write
// to one row and an unread count is a count. A shared row would make both of
// those a join against a second table that does not exist yet.
func (n *Notifications) Record(ctx context.Context, msg api.Notification, retainFor time.Duration) error {
	var retain *time.Time
	if retainFor > 0 {
		at := time.Now().UTC().Add(retainFor)
		retain = &at
	}

	for _, r := range msg.Recipients {
		if r.UserID == "" {
			continue
		}
		_, err := n.db.Exec(ctx, `
			INSERT INTO notifications (id, user_id, app_id, kind, subject, body, retain_until)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id.New(id.Notification), r.UserID, nullable(msg.AppID),
			string(msg.Kind), msg.Subject, msg.Body, retain)
		if err != nil {
			return errs.Wrap(errs.Internal, "Could not record the notification.", err)
		}
	}
	return nil
}

// ListForUser returns a user's notifications, newest first.
func (n *Notifications) ListForUser(ctx context.Context, userID string, unreadOnly bool) ([]Notification, error) {
	query := `
		SELECT id, user_id, coalesce(app_id, ''), kind, subject, body, read_at, created_at
		FROM notifications WHERE user_id = $1`
	if unreadOnly {
		query += ` AND read_at IS NULL`
	}
	query += ` ORDER BY created_at DESC LIMIT 200`

	rows, err := n.db.Query(ctx, query, userID)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not read your notifications.", err)
	}
	defer rows.Close()

	out := make([]Notification, 0)
	for rows.Next() {
		var rec Notification
		if err := rows.Scan(&rec.ID, &rec.UserID, &rec.AppID, &rec.Kind,
			&rec.Subject, &rec.Body, &rec.ReadAt, &rec.CreatedAt); err != nil {
			return nil, errs.Wrap(errs.Internal, "Could not read your notifications.", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// MarkRead marks one notification read, for its owner only.
//
// The user ID is in the WHERE clause rather than checked first: a check and
// then an update is two statements with a gap between them, and the gap is
// where someone marks a notification that is not theirs.
func (n *Notifications) MarkRead(ctx context.Context, userID, notificationID string) error {
	_, err := n.db.Exec(ctx,
		`UPDATE notifications SET read_at = now() WHERE id = $1 AND user_id = $2 AND read_at IS NULL`,
		notificationID, userID)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not mark the notification as read.", err)
	}
	return nil
}

// No compile-time assertion that this satisfies the adapter's Sink: that would
// mean importing the adapter here, which is the dependency this signature
// exists to avoid. main wires the two together, and a mismatch fails there.
