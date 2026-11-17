package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Lessons, reviews and mastery (plan §3, §8). attempts, mastery_events and
// xp_events are append-only: there are no update or delete methods for them
// (ADR 3). topic_state and daily_totals are projections the service writes
// in the same transaction as the events, and Replace* rewrites for a
// rebuild.

// TaskKind is what a task is.
type TaskKind string

const (
	TaskLesson     TaskKind = "lesson"
	TaskReview     TaskKind = "review"
	TaskQuiz       TaskKind = "quiz"
	TaskDiagnostic TaskKind = "diagnostic"
)

// TaskStatus is where a task is.
type TaskStatus string

const (
	TaskActive    TaskStatus = "active" // on screen; at most one per student
	TaskPaused    TaskStatus = "paused" // a lesson stopped for the day
	TaskPassed    TaskStatus = "passed"
	TaskFailed    TaskStatus = "failed"
	TaskAbandoned TaskStatus = "abandoned"
)

// Open reports whether the task can still be worked on.
func (s TaskStatus) Open() bool { return s == TaskActive || s == TaskPaused }

// Task is a lesson, review, quiz or diagnostic. State is JSON the service
// owns.
type Task struct {
	ID          string
	StudentID   string
	Kind        TaskKind
	TopicID     string
	PackVersion string
	Status      TaskStatus
	State       json.RawMessage
	XPAwarded   int
	StartedAt   time.Time
	UpdatedAt   time.Time
	FinishedAt  *time.Time
}

// Attempt is one answered problem.
type Attempt struct {
	ID         string // issued with the problem
	TaskID     string
	StudentID  string
	TopicID    string
	KPID       string
	TemplateID string
	Seed       uint64
	Answer     json.RawMessage
	Correct    bool
	Note       string
	ResponseMS int64
	CreatedAt  time.Time
}

// MasteryEvent is one change to a topic's state. Detail is JSON.
type MasteryEvent struct {
	ID        string
	StudentID string
	TopicID   string
	Kind      string
	TaskID    string
	Detail    json.RawMessage
	CreatedAt time.Time
}

// TopicState is one row of the topic_state projection.
type TopicState struct {
	StudentID  string
	TopicID    string
	State      string // learning or mastered
	Stage      int
	DueOn      string // YYYY-MM-DD, household-local; "" when not mastered
	LastResult string
	FailStreak int
	Pauses     int
	UpdatedAt  time.Time
}

// XP sources.
const (
	XPFromTask  = "task"
	XPFromDrill = "drill"
)

// XPEvent is XP earned from one task or drill.
type XPEvent struct {
	ID         string
	StudentID  string
	SourceKind string
	SourceID   string
	Amount     int
	LocalDate  string // YYYY-MM-DD, household-local
	CreatedAt  time.Time
}

// DailyTotal is one student's XP on one day against that day's goal.
type DailyTotal struct {
	StudentID string
	LocalDate string
	XP        int
	Goal      int
	GoalMet   bool
}

// --- tasks ------------------------------------------------------------------

const taskCols = `id, student_id, kind, topic_id, pack_version, status, state, xp_awarded, started_at, updated_at, finished_at`

func scanTask(r interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var state string
	err := r.Scan(&t.ID, &t.StudentID, &t.Kind, nstr{&t.TopicID}, &t.PackVersion, &t.Status, &state,
		&t.XPAwarded, rtime{&t.StartedAt}, rtime{&t.UpdatedAt}, ntime{&t.FinishedAt})
	t.State = []byte(state)
	return t, err
}

func (x queries) listTasks(ctx context.Context, q string, args ...any) ([]Task, error) {
	rows, err := x.rows(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (x queries) GetTask(ctx context.Context, id string) (Task, error) {
	t, err := scanTask(x.row(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
	return t, notFound(err)
}

func (x queries) GetActiveTask(ctx context.Context, studentID string) (Task, error) {
	t, err := scanTask(x.row(ctx, `SELECT `+taskCols+` FROM tasks WHERE student_id = ? AND status = 'active'`, studentID))
	return t, notFound(err)
}

func (x queries) ListOpenTasks(ctx context.Context, studentID string) ([]Task, error) {
	return x.listTasks(ctx, `SELECT `+taskCols+` FROM tasks WHERE student_id = ? AND status IN ('active', 'paused')
		ORDER BY started_at, id`, studentID)
}

func (x queries) ListFinishedTasks(ctx context.Context, studentID string, limit int) ([]Task, error) {
	q := `SELECT ` + taskCols + ` FROM tasks WHERE student_id = ? AND finished_at IS NOT NULL
		ORDER BY finished_at DESC, id DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	return x.listTasks(ctx, q, studentID)
}

func (x queries) CreateTask(ctx context.Context, t Task) error {
	_, err := x.exec(ctx, `INSERT INTO tasks (`+taskCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.StudentID, t.Kind, nul(t.TopicID), t.PackVersion, t.Status, string(t.State), t.XPAwarded,
		x.t(t.StartedAt), x.t(t.UpdatedAt), x.tp(t.FinishedAt))
	return err
}

func (x queries) UpdateTask(ctx context.Context, t Task) error {
	res, err := x.exec(ctx, `UPDATE tasks SET status = ?, state = ?, xp_awarded = ?, updated_at = ?, finished_at = ?
		WHERE id = ?`, t.Status, string(t.State), t.XPAwarded, x.t(t.UpdatedAt), x.tp(t.FinishedAt), t.ID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return firstErr(err, ErrNotFound)
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// --- attempts ---------------------------------------------------------------

const attemptCols = `id, task_id, student_id, topic_id, kp_id, template_id, seed, answer, correct, note, response_ms, created_at`

func scanAttempt(r interface{ Scan(...any) error }) (Attempt, error) {
	var a Attempt
	var seed, answer string
	err := r.Scan(&a.ID, &a.TaskID, &a.StudentID, &a.TopicID, nstr{&a.KPID}, &a.TemplateID, &seed, &answer,
		&a.Correct, nstr{&a.Note}, &a.ResponseMS, rtime{&a.CreatedAt})
	if err != nil {
		return a, err
	}
	a.Answer = []byte(answer)
	a.Seed, err = strconv.ParseUint(seed, 10, 64)
	return a, err
}

func (x queries) GetAttempt(ctx context.Context, id string) (Attempt, error) {
	a, err := scanAttempt(x.row(ctx, `SELECT `+attemptCols+` FROM attempts WHERE id = ?`, id))
	return a, notFound(err)
}

func (x queries) ListAttempts(ctx context.Context, taskID string) ([]Attempt, error) {
	rows, err := x.rows(ctx, `SELECT `+attemptCols+` FROM attempts WHERE task_id = ? ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attempt
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (x queries) InsertAttempt(ctx context.Context, a Attempt) (bool, error) {
	res, err := x.exec(ctx, `INSERT INTO attempts (`+attemptCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING`,
		a.ID, a.TaskID, a.StudentID, a.TopicID, nul(a.KPID), a.TemplateID, strconv.FormatUint(a.Seed, 10),
		string(a.Answer), a.Correct, nul(a.Note), a.ResponseMS, x.t(a.CreatedAt))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// --- mastery ----------------------------------------------------------------

func (x queries) InsertMasteryEvent(ctx context.Context, e MasteryEvent) error {
	_, err := x.exec(ctx, `INSERT INTO mastery_events (id, student_id, topic_id, kind, task_id, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.StudentID, e.TopicID, e.Kind, nul(e.TaskID), nul(string(e.Detail)), x.t(e.CreatedAt))
	return err
}

func (x queries) ListMasteryEvents(ctx context.Context, studentID string) ([]MasteryEvent, error) {
	rows, err := x.rows(ctx, `SELECT id, student_id, topic_id, kind, task_id, detail, created_at FROM mastery_events
		WHERE student_id = ? ORDER BY created_at, id`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MasteryEvent
	for rows.Next() {
		var e MasteryEvent
		var detail string
		if err := rows.Scan(&e.ID, &e.StudentID, &e.TopicID, &e.Kind, nstr{&e.TaskID}, nstr{&detail}, rtime{&e.CreatedAt}); err != nil {
			return nil, err
		}
		if detail != "" {
			e.Detail = []byte(detail)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (x queries) ListTopicStates(ctx context.Context, studentID string) ([]TopicState, error) {
	rows, err := x.rows(ctx, `SELECT student_id, topic_id, state, stage, due_on, last_result, fail_streak, pauses, updated_at
		FROM topic_state WHERE student_id = ? ORDER BY topic_id`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TopicState
	for rows.Next() {
		var s TopicState
		if err := rows.Scan(&s.StudentID, &s.TopicID, &s.State, &s.Stage, nstr{&s.DueOn}, nstr{&s.LastResult},
			&s.FailStreak, &s.Pauses, rtime{&s.UpdatedAt}); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (x queries) PutTopicState(ctx context.Context, s TopicState) error {
	_, err := x.exec(ctx, `INSERT INTO topic_state (student_id, topic_id, state, stage, due_on, last_result, fail_streak, pauses, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (student_id, topic_id) DO UPDATE SET state = excluded.state, stage = excluded.stage,
			due_on = excluded.due_on, last_result = excluded.last_result, fail_streak = excluded.fail_streak,
			pauses = excluded.pauses, updated_at = excluded.updated_at`,
		s.StudentID, s.TopicID, s.State, s.Stage, nul(s.DueOn), nul(s.LastResult), s.FailStreak, s.Pauses, x.t(s.UpdatedAt))
	return err
}

func (x queries) DeleteTopicState(ctx context.Context, studentID, topicID string) error {
	_, err := x.exec(ctx, `DELETE FROM topic_state WHERE student_id = ? AND topic_id = ?`, studentID, topicID)
	return err
}

func (x queries) ReplaceTopicStates(ctx context.Context, studentID string, states []TopicState) error {
	if _, err := x.exec(ctx, `DELETE FROM topic_state WHERE student_id = ?`, studentID); err != nil {
		return err
	}
	for _, s := range states {
		s.StudentID = studentID
		if err := x.PutTopicState(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// --- xp ---------------------------------------------------------------------

func (x queries) InsertXPEvent(ctx context.Context, e XPEvent) (bool, error) {
	res, err := x.exec(ctx, `INSERT INTO xp_events (id, student_id, source_kind, source_id, amount, local_date, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (source_kind, source_id) DO NOTHING`,
		e.ID, e.StudentID, e.SourceKind, e.SourceID, e.Amount, e.LocalDate, x.t(e.CreatedAt))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (x queries) ListXPEvents(ctx context.Context, studentID string) ([]XPEvent, error) {
	rows, err := x.rows(ctx, `SELECT id, student_id, source_kind, source_id, amount, local_date, created_at FROM xp_events
		WHERE student_id = ? ORDER BY created_at, id`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []XPEvent
	for rows.Next() {
		var e XPEvent
		if err := rows.Scan(&e.ID, &e.StudentID, &e.SourceKind, &e.SourceID, &e.Amount, &e.LocalDate, rtime{&e.CreatedAt}); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (x queries) SumXP(ctx context.Context, studentID, sourceKind, localDate string) (int, error) {
	var n int
	err := x.row(ctx, `SELECT COALESCE(SUM(amount), 0) FROM xp_events WHERE student_id = ? AND source_kind = ? AND local_date = ?`,
		studentID, sourceKind, localDate).Scan(&n)
	return n, err
}

func (x queries) GetDailyTotal(ctx context.Context, studentID, localDate string) (DailyTotal, error) {
	d := DailyTotal{StudentID: studentID, LocalDate: localDate}
	err := x.row(ctx, `SELECT xp, goal, goal_met FROM daily_totals WHERE student_id = ? AND local_date = ?`, studentID, localDate).
		Scan(&d.XP, &d.Goal, &d.GoalMet)
	return d, notFound(err)
}

func (x queries) ListDailyTotals(ctx context.Context, studentID, from, to string) ([]DailyTotal, error) {
	rows, err := x.rows(ctx, `SELECT local_date, xp, goal, goal_met FROM daily_totals
		WHERE student_id = ? AND local_date >= ? AND local_date <= ? ORDER BY local_date`, studentID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyTotal
	for rows.Next() {
		d := DailyTotal{StudentID: studentID}
		if err := rows.Scan(&d.LocalDate, &d.XP, &d.Goal, &d.GoalMet); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (x queries) PutDailyTotal(ctx context.Context, d DailyTotal) error {
	_, err := x.exec(ctx, `INSERT INTO daily_totals (student_id, local_date, xp, goal, goal_met) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (student_id, local_date) DO UPDATE SET xp = excluded.xp, goal = excluded.goal, goal_met = excluded.goal_met`,
		d.StudentID, d.LocalDate, d.XP, d.Goal, d.GoalMet)
	return err
}

func (x queries) ReplaceDailyTotals(ctx context.Context, studentID string, days []DailyTotal) error {
	if _, err := x.exec(ctx, `DELETE FROM daily_totals WHERE student_id = ?`, studentID); err != nil {
		return err
	}
	for _, d := range days {
		d.StudentID = studentID
		if err := x.PutDailyTotal(ctx, d); err != nil {
			return err
		}
	}
	return nil
}
