package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Export and import (plan 9.1, 11.1): every row of every table as JSON
// Lines, the same from SQLite and Postgres, so a household can move between
// them in either direction. Sessions and sign-in throttling are left out:
// after an import everyone signs in again.

// ExportFormat is the version of the dump's layout.
const ExportFormat = 1

type colKind int

const (
	colText colKind = iota
	colInt
	colBool
	colTime
)

type exportTable struct {
	name  string
	order string // ORDER BY, so a dump is the same each time
	cols  []exportCol
}

type exportCol struct {
	name string
	kind colKind
}

func cols(spec string) []exportCol {
	var out []exportCol
	for _, f := range strings.Fields(spec) {
		name, kind, _ := strings.Cut(f, ":")
		c := exportCol{name: name}
		switch kind {
		case "i":
			c.kind = colInt
		case "b":
			c.kind = colBool
		case "t":
			c.kind = colTime
		}
		out = append(out, c)
	}
	return out
}

// exportTables lists every table that is exported, parents before children.
// TestExportCoversSchema fails when a migration adds a column missing here.
var exportTables = []exportTable{
	{"household", "id", cols("id name timezone created_at:t")},
	{"users", "id", cols("id role display_name avatar email password_hash pin_hash created_at:t")},
	{"student_settings", "user_id", cols("user_id course_id daily_xp_goal:i rest_days fluency_enabled:b")},
	{"settings", "key", cols("key value secret:b")},
	{"audit_log", "id", cols("id actor_id action target_id detail created_at:t")},
	{"drills", "id", cols("id owner_id kind title settings seed problems created_at:t")},
	{"drill_results", "drill_id", cols("drill_id student_id correct:i total:i duration_ms:i answers finished_at:t")},
	{"tasks", "id", cols("id student_id kind topic_id pack_version status state xp_awarded:i started_at:t updated_at:t finished_at:t")},
	{"attempts", "id", cols("id task_id student_id topic_id kp_id template_id seed answer correct:b note response_ms:i created_at:t")},
	{"mastery_events", "id", cols("id student_id topic_id kind task_id detail created_at:t")},
	{"topic_state", "student_id, topic_id", cols("student_id topic_id state stage:i due_on last_result fail_streak:i pauses:i updated_at:t")},
	{"xp_events", "id", cols("id student_id source_kind source_id amount:i local_date created_at:t")},
	{"daily_totals", "student_id, local_date", cols("student_id local_date xp:i goal:i goal_met:b")},
	{"notifications", "id", cols("id user_id kind subject_id payload dedupe_key created_at:t read_at:t")},
	{"drill_assignments", "id", cols("id student_id parent_id kind title settings drill_id created_at:t done_at:t")},
	{"job_runs", "job", cols("job last_run_for")},
	{"content_packs", "version", cols("version sha256 file installed_by installed_at:t")},
}

// NotExported lists the tables an export leaves out on purpose.
var NotExported = []string{"sessions", "auth_attempts", "goose_db_version"}

// ExportedColumns lists each exported table's columns, for the schema test.
func ExportedColumns() map[string][]string {
	out := map[string][]string{}
	for _, t := range exportTables {
		for _, c := range t.cols {
			out[t.name] = append(out[t.name], c.name)
		}
	}
	return out
}

// ExportHeader is the dump's first line.
type ExportHeader struct {
	Format     int       `json:"zigrt_export"`
	App        string    `json:"app"`
	ExportedAt time.Time `json:"exported_at"`
}

type exportLine struct {
	Table string                     `json:"t"`
	Row   map[string]json.RawMessage `json:"r"`
}

// Export writes the header and then every row, one JSON object per line.
func (s *SQL) Export(ctx context.Context, w io.Writer, h ExportHeader) error {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	h.Format = ExportFormat
	if err := enc.Encode(h); err != nil {
		return err
	}
	// One read transaction, so the dump is consistent.
	tx, err := s.read.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := queries{q: tx, d: s.dialect}
	for _, t := range exportTables {
		if err := exportRows(ctx, q, enc, t); err != nil {
			return fmt.Errorf("export %s: %w", t.name, err)
		}
	}
	return bw.Flush()
}

func exportRows(ctx context.Context, q queries, enc *json.Encoder, t exportTable) error {
	names := make([]string, len(t.cols))
	for i, c := range t.cols {
		names[i] = c.name
	}
	rows, err := q.rows(ctx, `SELECT `+strings.Join(names, ", ")+` FROM `+t.name+` ORDER BY `+t.order)
	if err != nil {
		return err
	}
	defer rows.Close()
	vals := make([]any, len(t.cols))
	ptrs := make([]any, len(t.cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		row := make(map[string]json.RawMessage, len(t.cols))
		for i, c := range t.cols {
			v, err := exportValue(c.kind, vals[i])
			if err != nil {
				return fmt.Errorf("%s: %w", c.name, err)
			}
			if row[c.name], err = json.Marshal(v); err != nil {
				return err
			}
		}
		if err := enc.Encode(exportLine{Table: t.name, Row: row}); err != nil {
			return err
		}
	}
	return rows.Err()
}

// exportValue turns a scanned value into its engine-independent form:
// text, a whole number, true/false, or a UTC time in TimeLayout.
func exportValue(k colKind, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch k {
	case colInt:
		switch x := v.(type) {
		case int64:
			return x, nil
		case int32:
			return int64(x), nil
		}
	case colBool:
		switch x := v.(type) {
		case bool:
			return x, nil
		case int64:
			return x != 0, nil
		}
	case colTime:
		t, err := scanTime(v)
		if err != nil || t == nil {
			return nil, err
		}
		return t.UTC().Format(TimeLayout), nil
	default:
		switch x := v.(type) {
		case string:
			return x, nil
		case []byte:
			return string(x), nil
		}
	}
	return nil, fmt.Errorf("unexpected %T", v)
}

// ErrNotEmpty means an import was asked to load into a database that
// already has a household.
var ErrNotEmpty = errors.New("this database already has a household; import only into an empty one")

// Import loads a dump into an empty database in one transaction, and
// reports how many rows it loaded.
func (s *SQL) Import(ctx context.Context, r io.Reader) (int, error) {
	if _, err := s.GetHousehold(ctx); err == nil {
		return 0, ErrNotEmpty
	} else if !errors.Is(err, ErrNotFound) {
		return 0, err
	}
	byName := map[string]exportTable{}
	for _, t := range exportTables {
		byName[t.name] = t
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	if !sc.Scan() {
		return 0, errors.Join(errors.New("the file is empty"), sc.Err())
	}
	var h ExportHeader
	if err := json.Unmarshal(sc.Bytes(), &h); err != nil || h.Format == 0 {
		return 0, errors.New("this is not a Zigrt export")
	}
	if h.Format > ExportFormat {
		return 0, fmt.Errorf("this export (format %d) is newer than this Zigrt can read", h.Format)
	}
	n := 0
	err := s.Tx(ctx, "", func(tx Tx) error {
		q := tx.(queries)
		for line := 2; sc.Scan(); line++ {
			var l exportLine
			dec := json.NewDecoder(strings.NewReader(sc.Text()))
			dec.UseNumber()
			if err := dec.Decode(&l); err != nil {
				return fmt.Errorf("line %d: %w", line, err)
			}
			t, ok := byName[l.Table]
			if !ok {
				return fmt.Errorf("line %d: unknown table %q", line, l.Table)
			}
			names := make([]string, 0, len(t.cols))
			args := make([]any, 0, len(t.cols))
			for _, c := range t.cols {
				v, err := importValue(q, c.kind, l.Row[c.name])
				if err != nil {
					return fmt.Errorf("line %d, %s.%s: %w", line, t.name, c.name, err)
				}
				names = append(names, c.name)
				args = append(args, v)
			}
			marks := strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ")
			if _, err := q.exec(ctx, `INSERT INTO `+t.name+` (`+strings.Join(names, ", ")+`) VALUES (`+marks+`)`, args...); err != nil {
				return fmt.Errorf("line %d, %s: %w", line, t.name, err)
			}
			n++
		}
		return sc.Err()
	})
	return n, err
}

func importValue(q queries, k colKind, raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	switch k {
	case colInt:
		if num, ok := v.(json.Number); ok {
			return num.Int64()
		}
	case colBool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case colTime:
		if s, ok := v.(string); ok {
			t, err := scanTime(s)
			if err != nil {
				return nil, err
			}
			return q.t(*t), nil
		}
	default:
		if s, ok := v.(string); ok {
			return s, nil
		}
	}
	return nil, fmt.Errorf("unexpected value %s", raw)
}
