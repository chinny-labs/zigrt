package service

import (
	"errors"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/learning"
)

// ErrNoContent means no content pack is loaded, so there are no lessons.
var ErrNoContent = errors.New("no content pack is loaded")

// library is the active content pack with its graph. It is immutable and
// swapped whole (plan section 7.3).
type library struct {
	pack  *content.Pack
	graph *learning.Graph
}

// SetPack makes a pack the active one. Tasks already started keep their
// pack version on record; their templates are looked up in the new pack.
func (s *Service) SetPack(p *content.Pack) {
	if p == nil {
		s.lib.Store(nil)
		return
	}
	s.lib.Store(&library{pack: p, graph: learning.NewGraph(p)})
}

// Pack returns the active content pack, or nil.
func (s *Service) Pack() *content.Pack {
	if l := s.lib.Load(); l != nil {
		return l.pack
	}
	return nil
}

func (s *Service) library() (*library, error) {
	l := s.lib.Load()
	if l == nil {
		return nil, ErrNoContent
	}
	return l, nil
}

// courseTopics is the working set for a student's course: the course's
// topics and everything they require. A student with no course, or one the
// pack no longer has, gets the pack's first course.
func (l *library) courseTopics(courseID string) []string {
	m := l.pack.Manifest
	if courseID == "" || !hasCourse(m.Courses, courseID) {
		if len(m.Courses) == 0 {
			return nil
		}
		courseID = m.Courses[0].ID
	}
	var ids []string
	for _, t := range l.pack.CourseTopics(courseID) {
		ids = append(ids, t.ID)
	}
	return l.graph.WorkingSet(ids)
}

func hasCourse(cs []content.Course, id string) bool {
	for _, c := range cs {
		if c.ID == id {
			return true
		}
	}
	return false
}
