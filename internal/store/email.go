package store

import (
	"context"
	"time"
)

// Email is one message in the outbox (plan §10). The sender job delivers
// it; a failure is retried later.
type Email struct {
	ID            string
	Kind          string // password_reset, weekly
	To            string
	Subject       string
	Body          string
	DedupeKey     string // one message per key, ever
	CreatedAt     time.Time
	Attempts      int
	NextAttemptAt time.Time
	SentAt        *time.Time
	LastError     string
}

// PasswordReset is a one-time link to set a new password.
type PasswordReset struct {
	ID        string
	UserID    string
	TokenHash string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

const emailCols = `id, kind, to_addr, subject, body, dedupe_key, created_at, attempts, next_attempt_at, sent_at, last_error`

func scanEmail(r interface{ Scan(...any) error }) (Email, error) {
	var e Email
	err := r.Scan(&e.ID, &e.Kind, &e.To, &e.Subject, &e.Body, &e.DedupeKey, rtime{&e.CreatedAt}, &e.Attempts,
		rtime{&e.NextAttemptAt}, ntime{&e.SentAt}, nstr{&e.LastError})
	return e, err
}

// QueueEmail adds a message unless one with its dedupe key exists. It
// reports whether it was added.
func (x queries) QueueEmail(ctx context.Context, e Email) (bool, error) {
	res, err := x.exec(ctx, `INSERT INTO email_outbox (`+emailCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (dedupe_key) DO NOTHING`,
		e.ID, e.Kind, e.To, e.Subject, e.Body, e.DedupeKey, x.t(e.CreatedAt), e.Attempts, x.t(e.NextAttemptAt), x.tp(e.SentAt), nul(e.LastError))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (x queries) DueEmails(ctx context.Context, now time.Time, maxAttempts, limit int) ([]Email, error) {
	rows, err := x.rows(ctx, `SELECT `+emailCols+` FROM email_outbox
		WHERE sent_at IS NULL AND attempts < ? AND next_attempt_at <= ? ORDER BY next_attempt_at, id LIMIT ?`,
		maxAttempts, x.t(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Email
	for rows.Next() {
		e, err := scanEmail(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (x queries) GetEmail(ctx context.Context, id string) (Email, error) {
	e, err := scanEmail(x.row(ctx, `SELECT `+emailCols+` FROM email_outbox WHERE id = ?`, id))
	return e, notFound(err)
}

// CountEmails counts messages not sent yet (still being tried) and those
// given up on.
func (x queries) CountEmails(ctx context.Context, maxAttempts int) (pending, failed int, err error) {
	err = x.row(ctx, `SELECT
		COALESCE(SUM(CASE WHEN attempts < ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN attempts >= ? THEN 1 ELSE 0 END), 0)
		FROM email_outbox WHERE sent_at IS NULL`, maxAttempts, maxAttempts).Scan(&pending, &failed)
	return pending, failed, err
}

func (x queries) UpdateEmail(ctx context.Context, e Email) error {
	_, err := x.exec(ctx, `UPDATE email_outbox SET attempts = ?, next_attempt_at = ?, sent_at = ?, last_error = ? WHERE id = ?`,
		e.Attempts, x.t(e.NextAttemptAt), x.tp(e.SentAt), nul(e.LastError), e.ID)
	return err
}

// PruneEmails removes messages sent or created before a time.
func (x queries) PruneEmails(ctx context.Context, before time.Time) (int64, error) {
	res, err := x.exec(ctx, `DELETE FROM email_outbox WHERE created_at < ?`, x.t(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const resetCols = `id, user_id, token_hash, created_at, expires_at, used_at`

func scanReset(r interface{ Scan(...any) error }) (PasswordReset, error) {
	var p PasswordReset
	err := r.Scan(&p.ID, &p.UserID, &p.TokenHash, rtime{&p.CreatedAt}, rtime{&p.ExpiresAt}, ntime{&p.UsedAt})
	return p, err
}

func (x queries) CreatePasswordReset(ctx context.Context, p PasswordReset) error {
	_, err := x.exec(ctx, `INSERT INTO password_resets (`+resetCols+`) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.UserID, p.TokenHash, x.t(p.CreatedAt), x.t(p.ExpiresAt), x.tp(p.UsedAt))
	return err
}

func (x queries) GetPasswordReset(ctx context.Context, hash string) (PasswordReset, error) {
	p, err := scanReset(x.row(ctx, `SELECT `+resetCols+` FROM password_resets WHERE token_hash = ?`, hash))
	return p, notFound(err)
}

// LatestPasswordReset is a user's newest reset link.
func (x queries) LatestPasswordReset(ctx context.Context, userID string) (PasswordReset, error) {
	p, err := scanReset(x.row(ctx, `SELECT `+resetCols+` FROM password_resets WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, userID))
	return p, notFound(err)
}

// UsePasswordResets marks every open link of a user used.
func (x queries) UsePasswordResets(ctx context.Context, userID string, at time.Time) error {
	_, err := x.exec(ctx, `UPDATE password_resets SET used_at = ? WHERE user_id = ? AND used_at IS NULL`, x.t(at), userID)
	return err
}
