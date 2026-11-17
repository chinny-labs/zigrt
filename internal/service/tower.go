package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/learning"
	"github.com/chinny-labs/zigrt/internal/store"
)

// The tower (S12): the student's course by grade (tiers) and strand (faces),
// every topic a block. A tier or face is finished once every block in it
// has been placed at some time; a finished one stays finished even if a
// block later needs mending (plan 6.6: nobody falls).

// Block states, in the student's words.
const (
	BlockPlaced   = "placed"   // mastered, not due
	BlockMend     = "mend"     // mastered, review due
	BlockBuilding = "building" // lesson started or reopened
	BlockReady    = "ready"
	BlockLater    = "later" // locked
)

// TowerView is the tower page.
type TowerView struct {
	Tiers  []TowerTier // youngest grade first
	Placed int
	Next   []NextTask // what the student may start, to act on a block
}

// TowerTier is one grade band.
type TowerTier struct {
	Grade         string
	Placed, Total int
	Finished      bool // every block placed at some time
	Faces         []TowerFace
}

// TowerFace is one strand within a tier.
type TowerFace struct {
	Strand   content.Strand
	Finished bool
	Blocks   []Block
}

// Block is one topic on the tower.
type Block struct {
	Topic       *content.Topic
	State       string
	FirstPlaced *time.Time // when it was first placed, if ever
	Mended      int        // reviews passed
	Due         string     // household-local date of the next review, if placed
}

// Tower builds a student's tower.
func (s *Service) Tower(ctx context.Context, student store.User) (TowerView, error) {
	var v TowerView
	if !student.IsStudent() {
		return v, ErrForbidden
	}
	l, err := s.library()
	if err != nil {
		return v, err
	}
	today, err := s.today(ctx, s.Store)
	if err != nil {
		return v, err
	}
	sit, err := s.situation(ctx, s.Store, l, student.ID, today)
	if err != nil {
		return v, err
	}
	events, err := s.Store.ListMasteryEvents(ctx, student.ID)
	if err != nil {
		return v, err
	}
	first, mended := placementHistory(events)
	v.Next, _ = s.toNext(l, sit)

	byGrade := map[string]*TowerTier{}
	for _, id := range sit.Course {
		t := l.pack.Topic(id)
		tier := byGrade[t.Grade]
		if tier == nil {
			tier = &TowerTier{Grade: t.Grade}
			byGrade[t.Grade] = tier
		}
		b := Block{Topic: t, Mended: mended[id]}
		if at, ok := first[id]; ok {
			b.FirstPlaced = &at
		}
		switch st := sit.States[id]; sit.States.Status(l.graph, id) {
		case learning.Mastered:
			b.State = BlockPlaced
			if st.Due <= today.date {
				b.State = BlockMend
			}
			b.Due = st.Due.String()
		case learning.Learning:
			b.State = BlockBuilding
		case learning.Ready:
			b.State = BlockReady
		default:
			b.State = BlockLater
		}
		if b.State == BlockPlaced || b.State == BlockMend {
			tier.Placed++
			v.Placed++
		}
		tier.Total++
		fi := slices.IndexFunc(tier.Faces, func(f TowerFace) bool { return f.Strand.ID == t.Strand })
		if fi < 0 {
			tier.Faces = append(tier.Faces, TowerFace{Strand: strand(l.pack, t.Strand)})
			fi = len(tier.Faces) - 1
		}
		tier.Faces[fi].Blocks = append(tier.Faces[fi].Blocks, b)
	}
	for _, g := range content.Grades {
		tier := byGrade[g]
		if tier == nil {
			continue
		}
		tier.Finished = true
		for i := range tier.Faces {
			f := &tier.Faces[i]
			f.Finished = !slices.ContainsFunc(f.Blocks, func(b Block) bool { return b.FirstPlaced == nil })
			tier.Finished = tier.Finished && f.Finished
		}
		v.Tiers = append(v.Tiers, *tier)
	}
	return v, nil
}

func strand(p *content.Pack, id string) content.Strand {
	for _, s := range p.Strands {
		if s.ID == id {
			return s
		}
	}
	return content.Strand{ID: id, Title: content.Text{"en": id}}
}

// placementHistory reads, from the event log, when each topic was first
// placed (a passed lesson, the placement test or a parent) and how many of
// its reviews passed.
func placementHistory(events []store.MasteryEvent) (map[string]time.Time, map[string]int) {
	first, mended := map[string]time.Time{}, map[string]int{}
	for _, e := range events {
		switch learning.EventKind(e.Kind) {
		case learning.LessonPassed, learning.Placed, learning.ParentOverride:
			if _, ok := first[e.TopicID]; !ok && (e.Kind != string(learning.ParentOverride) || overrideMastered(e)) {
				first[e.TopicID] = e.CreatedAt
			}
		case learning.ReviewPassed:
			mended[e.TopicID]++
		}
	}
	return first, mended
}

func overrideMastered(e store.MasteryEvent) bool {
	var d eventDetail
	return decodeDetail(e.Detail, &d) == nil && d.Mastered
}

// Milestone is a tier or face a lesson just finished (S9b).
type Milestone struct {
	Grade    string
	Strand   *content.Strand // nil when the whole tier is finished
	Blocks   int             // blocks in the tier or face
	Placed   int             // blocks on the whole tower
	TierNext string          // the grade above, if any
}

// milestone reports what, if anything, placing topicID by task taskID
// finished: the whole tier first, else the topic's face.
func (s *Service) milestone(ctx context.Context, r store.Reader, l *library, studentID, topicID, taskID string) (*Milestone, error) {
	events, err := r.ListMasteryEvents(ctx, studentID)
	if err != nil {
		return nil, err
	}
	first, _ := placementHistory(events)
	// Only the task that placed the topic for the first time can finish
	// anything: a lesson reopened and passed again is not a new milestone.
	firstHere := false
	for _, e := range events {
		if e.TopicID == topicID && e.Kind == string(learning.LessonPassed) {
			firstHere = e.TaskID == taskID && first[topicID].Equal(e.CreatedAt)
			break
		}
	}
	topic := l.pack.Topic(topicID)
	if !firstHere || topic == nil {
		return nil, nil
	}
	set, err := r.GetStudentSettings(ctx, studentID)
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	course := l.courseTopics(set.CourseID)
	tierDone, faceDone, tierN, faceN := true, true, 0, 0
	for _, id := range course {
		t := l.pack.Topic(id)
		if t.Grade != topic.Grade {
			continue
		}
		_, placed := first[id]
		tierN++
		tierDone = tierDone && placed
		if t.Strand == topic.Strand {
			faceN++
			faceDone = faceDone && placed
		}
	}
	m := &Milestone{Grade: topic.Grade, Placed: len(first)}
	for i, g := range content.Grades {
		if g == topic.Grade && i+1 < len(content.Grades) {
			m.TierNext = content.Grades[i+1]
		}
	}
	switch {
	case tierDone:
		m.Blocks = tierN
	case faceDone:
		st := strand(l.pack, topic.Strand)
		m.Strand, m.Blocks = &st, faceN
	default:
		return nil, nil
	}
	return m, nil
}

func decodeDetail(b []byte, d *eventDetail) error {
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, d)
}

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }
