package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// The parent side (plan §6.3): notifications, assigned drills, job
// bookkeeping and uploaded content packs.

// Notification is one item in a parent's bell (P6). Payload is JSON the
// service owns.
type Notification struct {
	ID        string
	UserID    string // the parent it is for
	Kind      string
	SubjectID string // the student it is about, if any
	Payload   json.RawMessage
	DedupeKey string
	CreatedAt time.Time
	ReadAt    *time.Time
}

// DrillAssignment is a drill or worksheet a parent set for one student.
type DrillAssignment struct {
	ID        string
	StudentID string
	ParentID  string
	Kind      DrillKind
	Title     string
	Settings  json.RawMessage
	DrillID   string // the set made when the student started it
	CreatedAt time.Time
	DoneAt    *time.Time
}

// ContentPackRecord is an uploaded content pack.
type ContentPackRecord struct {
	Version     string
	SHA256      string
	File        string
	InstalledBy string
	InstalledAt time.Time
}

// --- notifications ----------------------------------------------------------

func (x queries) InsertNotification(ctx context.Context, n Notification) (bool, error) {
	res, err := x.exec(ctx, `INSERT INTO notifications (id, user_id, kind, subject_id, payload, dedupe_key, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (user_id, dedupe_key) DO NOTHING`,
		n.ID, n.UserID, n.Kind, nul(n.SubjectID), nul(string(n.Payload)), n.DedupeKey, x.t(n.CreatedAt))
	if err != nil {
		return false, err
	}
	c, err := res.RowsAffected()
	return c == 1, err
}

func (x queries) ListNotifications(ctx context.Context, userID string, limit int) ([]Notification, error) {
	q := `SELECT id, user_id, kind, subject_id, payload, dedupe_key, created_at, read_at FROM notifications
		WHERE user_id = ? ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := x.rows(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		var payload string
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, nstr{&n.SubjectID}, nstr{&payload}, &n.DedupeKey,
			rtime{&n.CreatedAt}, ntime{&n.ReadAt}); err != nil {
			return nil, err
		}
		if payload != "" {
			n.Payload = []byte(payload)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (x queries) CountUnread(ctx context.Context, userID string) (int, error) {
	var n int
	err := x.row(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

func (x queries) MarkNotificationsRead(ctx context.Context, userID, id string, at time.Time) error {
	q, args := `UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, []any{x.t(at), userID}
	if id != "" {
		q += ` AND id = ?`
		args = append(args, id)
	}
	_, err := x.exec(ctx, q, args...)
	return err
}

// --- assigned drills --------------------------------------------------------

const assignmentCols = `id, student_id, parent_id, kind, title, settings, drill_id, created_at, done_at`

func scanAssignment(r interface{ Scan(...any) error }) (DrillAssignment, error) {
	var a DrillAssignment
	var settings string
	err := r.Scan(&a.ID, &a.StudentID, nstr{&a.ParentID}, &a.Kind, &a.Title, &settings, nstr{&a.DrillID},
		rtime{&a.CreatedAt}, ntime{&a.DoneAt})
	a.Settings = []byte(settings)
	return a, err
}

func (x queries) GetAssignment(ctx context.Context, id string) (DrillAssignment, error) {
	a, err := scanAssignment(x.row(ctx, `SELECT `+assignmentCols+` FROM drill_assignments WHERE id = ?`, id))
	return a, notFound(err)
}

func (x queries) ListAssignments(ctx context.Context, studentID string, openOnly bool) ([]DrillAssignment, error) {
	q := `SELECT ` + assignmentCols + ` FROM drill_assignments WHERE student_id = ?`
	if openOnly {
		q += ` AND done_at IS NULL`
	}
	rows, err := x.rows(ctx, q+` ORDER BY created_at, id`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DrillAssignment
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (x queries) CreateAssignment(ctx context.Context, a DrillAssignment) error {
	_, err := x.exec(ctx, `INSERT INTO drill_assignments (`+assignmentCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.StudentID, nul(a.ParentID), a.Kind, a.Title, string(a.Settings), nul(a.DrillID), x.t(a.CreatedAt), x.tp(a.DoneAt))
	return err
}

func (x queries) UpdateAssignment(ctx context.Context, a DrillAssignment) error {
	res, err := x.exec(ctx, `UPDATE drill_assignments SET drill_id = ?, done_at = ? WHERE id = ?`, nul(a.DrillID), x.tp(a.DoneAt), a.ID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return firstErr(err, ErrNotFound)
	}
	return nil
}

// --- jobs and packs ---------------------------------------------------------

func (x queries) GetJobRun(ctx context.Context, job string) (string, error) {
	var last string
	err := x.row(ctx, `SELECT last_run_for FROM job_runs WHERE job = ?`, job).Scan(&last)
	return last, notFound(err)
}

func (x queries) PutJobRun(ctx context.Context, job, lastRunFor string) error {
	_, err := x.exec(ctx, `INSERT INTO job_runs (job, last_run_for) VALUES (?, ?)
		ON CONFLICT (job) DO UPDATE SET last_run_for = excluded.last_run_for`, job, lastRunFor)
	return err
}

func (x queries) ListContentPacks(ctx context.Context) ([]ContentPackRecord, error) {
	rows, err := x.rows(ctx, `SELECT version, sha256, file, installed_by, installed_at FROM content_packs ORDER BY installed_at DESC, version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContentPackRecord
	for rows.Next() {
		var c ContentPackRecord
		if err := rows.Scan(&c.Version, &c.SHA256, &c.File, nstr{&c.InstalledBy}, rtime{&c.InstalledAt}); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (x queries) PutContentPack(ctx context.Context, c ContentPackRecord) error {
	_, err := x.exec(ctx, `INSERT INTO content_packs (version, sha256, file, installed_by, installed_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (version) DO UPDATE SET sha256 = excluded.sha256, file = excluded.file,
			installed_by = excluded.installed_by, installed_at = excluded.installed_at`,
		c.Version, c.SHA256, c.File, nul(c.InstalledBy), x.t(c.InstalledAt))
	return err
}
