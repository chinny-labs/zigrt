package service

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chinny-labs/zigrt/internal/drill"
	"github.com/chinny-labs/zigrt/internal/store"
)

// Drills are free practice (plan §5): saved, never part of mastery.

// DefaultDrillTitle heads a worksheet when no title is given.
const DefaultDrillTitle = "Maths practice"

// maxDrillDuration bounds the time a browser may report for one drill.
const maxDrillDuration = 6 * time.Hour

var (
	// ErrDrillDone means a result was already saved for the drill.
	ErrDrillDone = errors.New("that drill is already finished")
	// ErrNoMisses means there is nothing to drill again.
	ErrNoMisses = errors.New("there were no misses to drill again")
)

// Answer is one entry a student typed, with how long it took.
type Answer struct {
	Main string `json:"main"`
	Rem  string `json:"rem,omitempty"`
	MS   int64  `json:"ms,omitempty"`
}

// Marked is an Answer after the server has checked it.
type Marked struct {
	Answer
	Correct bool `json:"correct"`
}

// Given is the entry as text, for "You wrote 54".
func (m Marked) Given() string {
	if m.Main == "" {
		return ""
	}
	if r := strings.TrimLeft(m.Rem, "0"); r != "" {
		return m.Main + " R" + r
	}
	return m.Main
}

// DrillView is a saved set with its JSON decoded, and its result if any.
type DrillView struct {
	store.Drill
	Settings drill.Settings
	Problems []drill.Problem
	Result   *ResultView
}

// ResultView is a finished drill, marked.
type ResultView struct {
	store.DrillResult
	Answers []Marked
}

// Miss is a problem that was answered wrongly or not at all.
type Miss struct {
	Problem drill.Problem
	Given   string
}

// Misses lists what to look at again, in order.
func (d DrillView) Misses() []Miss {
	if d.Result == nil {
		return nil
	}
	var out []Miss
	for i, p := range d.Problems {
		if i < len(d.Result.Answers) && d.Result.Answers[i].Correct {
			continue
		}
		m := Miss{Problem: p}
		if i < len(d.Result.Answers) {
			m.Given = d.Result.Answers[i].Given()
		}
		out = append(out, m)
	}
	return out
}

func randomSeed() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return binary.LittleEndian.Uint64(b[:])
}

func (s *Service) seed() uint64 {
	if s.Seed != nil {
		return s.Seed()
	}
	return randomSeed()
}

func cleanTitle(t string) (string, error) {
	t = strings.TrimSpace(t)
	if t == "" {
		return DefaultDrillTitle, nil
	}
	if utf8.RuneCountInString(t) > 80 {
		return "", invalid("title", "Keep the title under 80 characters.")
	}
	return t, nil
}

// CreateDrill saves a new set built from the settings. Students make drills
// and worksheets; parents make worksheets.
func (s *Service) CreateDrill(ctx context.Context, actor store.User, kind store.DrillKind, in drill.Settings, title string) (DrillView, error) {
	if kind != store.KindDrill && kind != store.KindWorksheet {
		return DrillView{}, invalid("kind", "Choose a drill or a worksheet.")
	}
	if kind == store.KindDrill && !actor.IsStudent() {
		return DrillView{}, invalid("kind", "On-screen drills are for students. Make a worksheet instead.")
	}
	settings, err := drill.Normalize(in)
	if err != nil {
		return DrillView{}, invalid("ops", "Choose at least one kind of problem and one layout.")
	}
	title, err = cleanTitle(title)
	if err != nil {
		return DrillView{}, err
	}
	seed := s.seed()
	return s.saveDrill(ctx, actor, kind, title, settings, seed, drill.Generate(settings, seed))
}

func (s *Service) saveDrill(ctx context.Context, actor store.User, kind store.DrillKind, title string,
	settings drill.Settings, seed uint64, problems []drill.Problem) (DrillView, error) {
	sj, err := json.Marshal(settings)
	if err != nil {
		return DrillView{}, err
	}
	pj, err := json.Marshal(problems)
	if err != nil {
		return DrillView{}, err
	}
	d := store.Drill{ID: store.NewID(), OwnerID: actor.ID, Kind: kind, Title: title, Settings: sj, Seed: seed,
		Problems: pj, CreatedAt: s.now()}
	if err := s.Store.Tx(ctx, "", func(tx store.Tx) error { return tx.CreateDrill(ctx, d) }); err != nil {
		return DrillView{}, err
	}
	return DrillView{Drill: d, Settings: settings, Problems: problems}, nil
}

// Drill loads a set. Its owner may see it, and so may any parent.
func (s *Service) Drill(ctx context.Context, actor store.User, id string) (DrillView, error) {
	return s.loadDrill(ctx, s.Store, actor, id)
}

func (s *Service) loadDrill(ctx context.Context, r store.Reader, actor store.User, id string) (DrillView, error) {
	d, err := r.GetDrill(ctx, id)
	if err != nil {
		return DrillView{}, err
	}
	if d.OwnerID != actor.ID && !actor.IsParent() {
		return DrillView{}, ErrNotFound // not yours: as if it weren't there
	}
	v := DrillView{Drill: d}
	if err := json.Unmarshal(d.Settings, &v.Settings); err != nil {
		return v, err
	}
	if err := json.Unmarshal(d.Problems, &v.Problems); err != nil {
		return v, err
	}
	res, err := r.GetDrillResult(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	rv := ResultView{DrillResult: res}
	if err := json.Unmarshal(res.Answers, &rv.Answers); err != nil {
		return v, err
	}
	v.Result = &rv
	return v, nil
}

// FinishDrill marks and saves a student's answers. The browser's own marking
// is ignored: every answer is checked here. Problems past the end of answers
// count as not answered, so stopping early is allowed. A drill is finished
// once; a second call returns the saved result.
func (s *Service) FinishDrill(ctx context.Context, actor store.User, id string, answers []Answer, duration time.Duration) (DrillView, error) {
	var out DrillView
	// Locked on the student: the daily drill XP cap reads then writes.
	err := s.Store.Tx(ctx, actor.ID, func(tx store.Tx) error {
		v, err := s.loadDrill(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if v.OwnerID != actor.ID || !actor.IsStudent() || v.Kind != store.KindDrill {
			return ErrForbidden
		}
		if v.Result != nil {
			out = v
			return nil
		}
		if len(answers) > len(v.Problems) {
			answers = answers[:len(v.Problems)]
		}
		marked := make([]Marked, len(answers))
		correct := 0
		for i, a := range answers {
			a.Main, a.Rem = strings.TrimSpace(a.Main), strings.TrimSpace(a.Rem)
			if len(a.Main) > 8 || len(a.Rem) > 8 {
				a.Main, a.Rem = "", ""
			}
			a.MS = min(max(a.MS, 0), int64(maxDrillDuration/time.Millisecond))
			marked[i] = Marked{Answer: a, Correct: v.Problems[i].Check(a.Main, a.Rem)}
			if marked[i].Correct {
				correct++
			}
		}
		aj, err := json.Marshal(marked)
		if err != nil {
			return err
		}
		res := store.DrillResult{DrillID: v.ID, StudentID: actor.ID, Correct: correct, Total: len(v.Problems),
			DurationMS: int64(min(max(duration, 0), maxDrillDuration) / time.Millisecond), Answers: aj, FinishedAt: s.now()}
		inserted, err := tx.InsertDrillResult(ctx, res)
		if err != nil {
			return err
		}
		if inserted {
			if err := assignmentDone(ctx, tx, actor.ID, v.ID, res.FinishedAt); err != nil {
				return err
			}
			// Free practice is worth a little XP, capped a day (plan 5.2).
			today, err := s.today(ctx, tx)
			if err != nil {
				return err
			}
			earned, err := tx.SumXP(ctx, actor.ID, store.XPFromDrill, today.date.String())
			if err != nil {
				return err
			}
			if err := s.awardXP(ctx, tx, actor.ID, store.XPFromDrill, v.ID, s.Learn.DrillXP(correct, earned), today, res.FinishedAt); err != nil {
				return err
			}
		}
		v.Result = &ResultView{DrillResult: res, Answers: marked}
		out = v
		return nil
	})
	return out, err
}

// RetryMisses makes a new drill from the problems a finished drill missed.
func (s *Service) RetryMisses(ctx context.Context, actor store.User, id string) (DrillView, error) {
	v, err := s.Drill(ctx, actor, id)
	if err != nil {
		return DrillView{}, err
	}
	if v.OwnerID != actor.ID || !actor.IsStudent() {
		return DrillView{}, ErrForbidden
	}
	misses := v.Misses()
	if len(misses) == 0 {
		return DrillView{}, ErrNoMisses
	}
	problems := make([]drill.Problem, len(misses))
	for i, m := range misses {
		problems[i] = m.Problem
	}
	settings := v.Settings
	settings.Count = len(problems)
	return s.saveDrill(ctx, actor, store.KindDrill, v.Title, settings, v.Seed, problems)
}

// NewVersion makes a set with the same settings and fresh problems.
func (s *Service) NewVersion(ctx context.Context, actor store.User, id string) (DrillView, error) {
	v, err := s.Drill(ctx, actor, id)
	if err != nil {
		return DrillView{}, err
	}
	return s.CreateDrill(ctx, actor, v.Kind, v.Settings, v.Title)
}

// DrillSummary is one line of a history list.
type DrillSummary struct {
	ID        string
	Kind      store.DrillKind
	Title     string
	About     string // "Addition (Sums to 20)"
	Count     int
	CreatedAt time.Time
	Finished  bool
	Correct   int
	Total     int
}

// RecentDrills lists the actor's latest sets, newest first.
func (s *Service) RecentDrills(ctx context.Context, actor store.User, limit int) ([]DrillSummary, error) {
	drills, err := s.Store.ListDrills(ctx, actor.ID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]DrillSummary, 0, len(drills))
	for _, d := range drills {
		var set drill.Settings
		var problems []drill.Problem
		if err := json.Unmarshal(d.Settings, &set); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(d.Problems, &problems); err != nil {
			return nil, err
		}
		sum := DrillSummary{ID: d.ID, Kind: d.Kind, Title: d.Title, About: drill.Describe(set), Count: len(problems), CreatedAt: d.CreatedAt}
		if d.Kind == store.KindDrill {
			res, err := s.Store.GetDrillResult(ctx, d.ID)
			switch {
			case err == nil:
				sum.Finished, sum.Correct, sum.Total = true, res.Correct, res.Total
			case !errors.Is(err, store.ErrNotFound):
				return nil, err
			}
		}
		out = append(out, sum)
	}
	return out, nil
}

// LastDrillSettings is what the builder starts from: the actor's most recent
// settings, or the defaults.
func (s *Service) LastDrillSettings(ctx context.Context, actor store.User) (drill.Settings, error) {
	drills, err := s.Store.ListDrills(ctx, actor.ID, 1)
	if err != nil || len(drills) == 0 {
		return drill.Default(), err
	}
	var set drill.Settings
	if err := json.Unmarshal(drills[0].Settings, &set); err != nil {
		return drill.Default(), nil //nolint:nilerr // unreadable old settings: start from the defaults
	}
	if norm, err := drill.Normalize(set); err == nil {
		norm.Count = nearestCount(norm.Count)
		return norm, nil
	}
	return drill.Default(), nil
}

// nearestCount maps any count (a retry of 3 misses, say) to one the builder offers.
func nearestCount(n int) int {
	for _, c := range drill.Counts {
		if c == n {
			return n
		}
	}
	return drill.Counts[1]
}
