package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Dialect captures the few places SQLite and Postgres differ. Queries are
// written once, in a portable subset, with ? placeholders (plan §8.3).
type Dialect interface {
	Name() string
	// Rebind rewrites ? placeholders for the engine ($1, $2… for Postgres).
	Rebind(query string) string
	// Time converts a timestamp to a driver argument (always UTC).
	Time(t time.Time) any
	// LockUser serializes writes for one user inside tx.
	LockUser(ctx context.Context, tx *sql.Tx, userID string) error
	IsUniqueViolation(err error) bool
}

// SQL implements Store on database/sql. Reads use the read pool; Tx uses the
// write pool. For Postgres both are the same *sql.DB.
type SQL struct {
	queries
	read, write *sql.DB
	dialect     Dialect
	close       func() error
}

// NewSQL wraps already-open, already-migrated pools.
func NewSQL(read, write *sql.DB, d Dialect, closeFn func() error) *SQL {
	return &SQL{queries: queries{q: read, d: d}, read: read, write: write, dialect: d, close: closeFn}
}

func (s *SQL) Dialect() string                { return s.dialect.Name() }
func (s *SQL) Ping(ctx context.Context) error { return s.write.PingContext(ctx) }
func (s *SQL) Close() error                   { return s.close() }

// WriteDB exposes the write pool to engine packages (backups, migrations).
func (s *SQL) WriteDB() *sql.DB { return s.write }

func (s *SQL) Tx(ctx context.Context, lockUser string, fn func(Tx) error) (err error) {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if lockUser != "" {
		if err = s.dialect.LockUser(ctx, tx, lockUser); err != nil {
			return err
		}
	}
	if err = fn(queries{q: tx, d: s.dialect}); err != nil {
		return err
	}
	return tx.Commit()
}

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type queries struct {
	q querier
	d Dialect
}

func (x queries) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	res, err := x.q.ExecContext(ctx, x.d.Rebind(query), args...)
	if err != nil && x.d.IsUniqueViolation(err) {
		return nil, fmt.Errorf("%w: %w", ErrConflict, err)
	}
	return res, err
}

func (x queries) row(ctx context.Context, query string, args ...any) *sql.Row {
	return x.q.QueryRowContext(ctx, x.d.Rebind(query), args...)
}

func (x queries) rows(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return x.q.QueryContext(ctx, x.d.Rebind(query), args...)
}

func (x queries) t(t time.Time) any { return x.d.Time(t) }

func (x queries) tp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return x.d.Time(*t)
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// --- scanning helpers -------------------------------------------------------

// nstr scans a nullable TEXT column into a string ("" for NULL).
type nstr struct{ p *string }

func (n nstr) Scan(v any) error {
	switch x := v.(type) {
	case nil:
		*n.p = ""
	case string:
		*n.p = x
	case []byte:
		*n.p = string(x)
	default:
		*n.p = fmt.Sprint(x)
	}
	return nil
}

// ntime scans a timestamp stored as TIMESTAMPTZ or RFC 3339 TEXT.
type ntime struct{ p **time.Time }

func (n ntime) Scan(v any) error {
	t, err := scanTime(v)
	if err != nil {
		return err
	}
	*n.p = t
	return nil
}

// rtime scans a non-null timestamp.
type rtime struct{ p *time.Time }

func (r rtime) Scan(v any) error {
	t, err := scanTime(v)
	if err != nil {
		return err
	}
	if t == nil {
		return errors.New("unexpected NULL timestamp")
	}
	*r.p = *t
	return nil
}

// TimeLayout is the fixed-width UTC format SQLite stores, so text order is
// time order.
const TimeLayout = "2006-01-02T15:04:05.000000Z"

func scanTime(v any) (*time.Time, error) {
	var s string
	switch x := v.(type) {
	case nil:
		return nil, nil
	case time.Time:
		t := x.UTC()
		return &t, nil
	case string:
		s = x
	case []byte:
		s = string(x)
	default:
		return nil, fmt.Errorf("cannot scan %T into a time", v)
	}
	t, err := time.Parse(TimeLayout, s)
	if err != nil {
		if t, err = time.Parse(time.RFC3339Nano, s); err != nil {
			return nil, err
		}
	}
	t = t.UTC()
	return &t, nil
}

// nul turns "" into NULL.
func nul(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// --- household & settings ---------------------------------------------------

func (x queries) GetHousehold(ctx context.Context) (Household, error) {
	var h Household
	err := x.row(ctx, `SELECT id, name, timezone, created_at FROM household LIMIT 1`).Scan(
		&h.ID, &h.Name, &h.Timezone, rtime{&h.CreatedAt})
	return h, notFound(err)
}

func (x queries) CreateHousehold(ctx context.Context, h Household) error {
	_, err := x.exec(ctx, `INSERT INTO household (id, name, timezone, created_at) VALUES (?, ?, ?, ?)`,
		h.ID, h.Name, h.Timezone, x.t(h.CreatedAt))
	return err
}

func (x queries) UpdateHousehold(ctx context.Context, h Household) error {
	_, err := x.exec(ctx, `UPDATE household SET name = ?, timezone = ? WHERE id = ?`, h.Name, h.Timezone, h.ID)
	return err
}

func (x queries) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := x.row(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v, notFound(err)
}

func (x queries) PutSetting(ctx context.Context, key, value string, secret bool) error {
	_, err := x.exec(ctx, `INSERT INTO settings (key, value, secret) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, secret = excluded.secret`, key, value, secret)
	return err
}

// --- users ------------------------------------------------------------------

const userCols = `id, role, display_name, avatar, email, password_hash, pin_hash, created_at`

func scanUser(sc interface{ Scan(...any) error }) (User, error) {
	var u User
	err := sc.Scan(&u.ID, &u.Role, &u.DisplayName, nstr{&u.Avatar}, nstr{&u.Email}, nstr{&u.PasswordHash},
		nstr{&u.PINHash}, rtime{&u.CreatedAt})
	return u, err
}

func (x queries) CountParents(ctx context.Context) (int, error) {
	var n int
	err := x.row(ctx, `SELECT COUNT(*) FROM users WHERE role = 'parent'`).Scan(&n)
	return n, err
}

func (x queries) GetUser(ctx context.Context, id string) (User, error) {
	u, err := scanUser(x.row(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
	return u, notFound(err)
}

func (x queries) GetUserByEmail(ctx context.Context, email string) (User, error) {
	u, err := scanUser(x.row(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, normEmail(email)))
	return u, notFound(err)
}

func normEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func (x queries) ListUsers(ctx context.Context, role Role) ([]User, error) {
	q, args := `SELECT `+userCols+` FROM users`, []any{}
	if role != "" {
		q += ` WHERE role = ?`
		args = append(args, role)
	}
	rows, err := x.rows(ctx, q+` ORDER BY role, display_name, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (x queries) CreateUser(ctx context.Context, u User) error {
	_, err := x.exec(ctx, `INSERT INTO users (`+userCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Role, u.DisplayName, nul(u.Avatar), nul(normEmail(u.Email)), nul(u.PasswordHash), nul(u.PINHash), x.t(u.CreatedAt))
	return err
}

func (x queries) UpdateUser(ctx context.Context, u User) error {
	_, err := x.exec(ctx, `UPDATE users SET display_name = ?, avatar = ?, email = ?, password_hash = ?, pin_hash = ? WHERE id = ?`,
		u.DisplayName, nul(u.Avatar), nul(normEmail(u.Email)), nul(u.PasswordHash), nul(u.PINHash), u.ID)
	return err
}

func (x queries) DeleteUser(ctx context.Context, id string) error {
	// Spelled out rather than left to ON DELETE CASCADE, so it also holds on a
	// connection where SQLite's foreign keys happen to be off.
	for _, q := range []string{
		`DELETE FROM sessions WHERE user_id = ?`,
		`DELETE FROM auth_attempts WHERE user_id = ?`,
		`DELETE FROM student_settings WHERE user_id = ?`,
		`DELETE FROM users WHERE id = ?`,
	} {
		if _, err := x.exec(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}

// --- sessions & throttling --------------------------------------------------

const sessionCols = `id_hash, user_id, device_label, created_at, last_seen_at, expires_at, revoked_at`

func (x queries) GetSession(ctx context.Context, idHash string) (Session, error) {
	var s Session
	err := x.row(ctx, `SELECT `+sessionCols+` FROM sessions WHERE id_hash = ?`, idHash).Scan(
		&s.IDHash, &s.UserID, nstr{&s.DeviceLabel}, rtime{&s.CreatedAt}, rtime{&s.LastSeenAt}, rtime{&s.ExpiresAt}, ntime{&s.RevokedAt})
	return s, notFound(err)
}

func (x queries) CreateSession(ctx context.Context, s Session) error {
	_, err := x.exec(ctx, `INSERT INTO sessions (`+sessionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.IDHash, s.UserID, nul(s.DeviceLabel), x.t(s.CreatedAt), x.t(s.LastSeenAt), x.t(s.ExpiresAt), x.tp(s.RevokedAt))
	return err
}

func (x queries) TouchSession(ctx context.Context, idHash string, lastSeen, expires time.Time) error {
	_, err := x.exec(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id_hash = ? AND revoked_at IS NULL`,
		x.t(lastSeen), x.t(expires), idHash)
	return err
}

func (x queries) RevokeSession(ctx context.Context, idHash string, at time.Time) error {
	_, err := x.exec(ctx, `UPDATE sessions SET revoked_at = ? WHERE id_hash = ? AND revoked_at IS NULL`, x.t(at), idHash)
	return err
}

func (x queries) RevokeUserSessions(ctx context.Context, userID string, at time.Time) error {
	_, err := x.exec(ctx, `UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, x.t(at), userID)
	return err
}

func (x queries) DeleteDeadSessions(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := x.exec(ctx, `DELETE FROM sessions WHERE expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?)`,
		x.t(cutoff), x.t(cutoff))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (x queries) GetAuthAttempts(ctx context.Context, userID string) (AuthAttempts, error) {
	a := AuthAttempts{UserID: userID}
	err := x.row(ctx, `SELECT failures, window_start, next_allowed_at, locked_at FROM auth_attempts WHERE user_id = ?`, userID).
		Scan(&a.Failures, ntime{&a.WindowStart}, ntime{&a.NextAllowedAt}, ntime{&a.LockedAt})
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil
	}
	return a, err
}

func (x queries) PutAuthAttempts(ctx context.Context, a AuthAttempts) error {
	_, err := x.exec(ctx, `INSERT INTO auth_attempts (user_id, failures, window_start, next_allowed_at, locked_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET failures = excluded.failures, window_start = excluded.window_start,
			next_allowed_at = excluded.next_allowed_at, locked_at = excluded.locked_at`,
		a.UserID, a.Failures, x.tp(a.WindowStart), x.tp(a.NextAllowedAt), x.tp(a.LockedAt))
	return err
}

func (x queries) ClearAuthAttempts(ctx context.Context, userID string) error {
	_, err := x.exec(ctx, `DELETE FROM auth_attempts WHERE user_id = ?`, userID)
	return err
}

// --- student settings -------------------------------------------------------

func (x queries) GetStudentSettings(ctx context.Context, userID string) (StudentSettings, error) {
	s := StudentSettings{UserID: userID}
	err := x.row(ctx, `SELECT course_id, daily_xp_goal, rest_days, fluency_enabled FROM student_settings WHERE user_id = ?`, userID).
		Scan(nstr{&s.CourseID}, &s.DailyXPGoal, &s.RestDays, &s.FluencyEnabled)
	return s, notFound(err)
}

func (x queries) PutStudentSettings(ctx context.Context, s StudentSettings) error {
	_, err := x.exec(ctx, `INSERT INTO student_settings (user_id, course_id, daily_xp_goal, rest_days, fluency_enabled)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET course_id = excluded.course_id, daily_xp_goal = excluded.daily_xp_goal,
			rest_days = excluded.rest_days, fluency_enabled = excluded.fluency_enabled`,
		s.UserID, nul(s.CourseID), s.DailyXPGoal, s.RestDays, s.FluencyEnabled)
	return err
}

// --- audit ------------------------------------------------------------------

func (x queries) InsertAudit(ctx context.Context, a AuditEntry) error {
	_, err := x.exec(ctx, `INSERT INTO audit_log (id, actor_id, action, target_id, detail, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, nul(a.ActorID), a.Action, nul(a.TargetID), nul(string(a.Detail)), x.t(a.CreatedAt))
	return err
}

func (x queries) ListAudit(ctx context.Context, targetID string, limit int) ([]AuditEntry, error) {
	q, args := `SELECT id, actor_id, action, target_id, detail, created_at FROM audit_log`, []any{}
	if targetID != "" {
		q += ` WHERE target_id = ?`
		args = append(args, targetID)
	}
	q += ` ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := x.rows(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		var detail string
		if err := rows.Scan(&a.ID, nstr{&a.ActorID}, &a.Action, nstr{&a.TargetID}, nstr{&detail}, rtime{&a.CreatedAt}); err != nil {
			return nil, err
		}
		if detail != "" {
			a.Detail = []byte(detail)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
