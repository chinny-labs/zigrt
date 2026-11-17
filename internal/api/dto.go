package api

import (
	"slices"
	"time"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/drill"
	"github.com/chinny-labs/zigrt/internal/learning"
	"github.com/chinny-labs/zigrt/internal/service"
)

// The JSON shapes, shared by the REST handlers and the MCP tools.

// Student is one student's summary.
type Student struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Avatar      string     `json:"avatar"`
	CourseID    string     `json:"course_id"`
	Course      string     `json:"course"`
	DailyXPGoal int        `json:"daily_xp_goal"`
	RestDays    []int      `json:"rest_days" jsonschema:"weekdays with no goal, 0 = Sunday"`
	Fluency     bool       `json:"fluency" jsonschema:"whether fact topics also check speed"`
	Locked      bool       `json:"locked" jsonschema:"locked out after too many wrong PINs; a parent unlocks in the app"`
	Today       Today      `json:"today"`
	Week        Week       `json:"week"`
	Placed      int        `json:"blocks_placed" jsonschema:"topics mastered so far"`
	NewBlocks   int        `json:"new_blocks_this_week"`
	LastActive  *time.Time `json:"last_active"`
	Stuck       *TopicRef  `json:"stuck" jsonschema:"a lesson that stopped twice and is not passed yet"`
}

// Today is a student's XP today.
type Today struct {
	XP      int  `json:"xp"`
	Goal    int  `json:"goal" jsonschema:"0 on a rest day"`
	GoalMet bool `json:"goal_met"`
	Streak  int  `json:"streak" jsonschema:"days in a row the goal was met (rest days don't break it)"`
}

// Week is a student's week, Monday to Sunday.
type Week struct {
	XP   int   `json:"xp"`
	Goal int   `json:"goal"`
	Days []Day `json:"days"`
}

// Day is one day of a week.
type Day struct {
	Date   string `json:"date"`
	XP     int    `json:"xp"`
	Goal   int    `json:"goal"`
	Met    bool   `json:"met"`
	Rest   bool   `json:"rest"`
	Future bool   `json:"future"`
}

// TopicRef names a topic.
type TopicRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Topic is a topic and where a student is with it.
type Topic struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Grade         string     `json:"grade"`
	Strand        string     `json:"strand"`
	State         string     `json:"state" jsonschema:"placed (mastered), mend (mastered, a review is due), building (a lesson is under way), ready (can be started) or later (needs earlier topics first)"`
	FirstPlaced   *time.Time `json:"first_placed,omitempty"`
	ReviewsPassed int        `json:"reviews_passed"`
	NextReview    string     `json:"next_review,omitempty" jsonschema:"the household-local date of the next review"`
}

// Activity is one thing a student did.
type Activity struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind" jsonschema:"lesson, review, quiz, placement, paused, drill or worksheet"`
	Title  string    `json:"title"`
	Detail string    `json:"detail"`
	Passed bool      `json:"passed"`
}

// Struggle is a topic a student keeps finding hard.
type Struggle struct {
	Topic         TopicRef  `json:"topic"`
	Stops         int       `json:"lessons_stopped"`
	FailedReviews int       `json:"reviews_failed"`
	Last          time.Time `json:"last"`
}

// Worksheet is a printable worksheet.
type Worksheet struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	About    string          `json:"about"`
	URL      string          `json:"url" jsonschema:"the printable page; opens for a signed-in parent"`
	Problems []drill.Problem `json:"problems"`
	Settings drill.Settings  `json:"settings"`
}

// System is the server's version and content.
type System struct {
	Version     string `json:"version"`
	PackID      string `json:"pack_id,omitempty"`
	PackVersion string `json:"pack_version,omitempty"`
	PackSource  string `json:"pack_source,omitempty"`
	Topics      int    `json:"topics"`
	Healthy     bool   `json:"healthy"`
}

func ref(t *content.Topic) *TopicRef {
	if t == nil {
		return nil
	}
	return &TopicRef{ID: t.ID, Title: t.Title.In(service.Locale)}
}

func studentDTO(c service.StudentCard) Student {
	s := Student{
		ID: c.Student.ID, Name: c.Student.DisplayName, Avatar: c.Student.Avatar,
		CourseID: c.Student.Settings.CourseID, Course: c.Course, DailyXPGoal: c.Student.Settings.DailyXPGoal,
		RestDays: []int{}, Fluency: c.Student.Settings.FluencyEnabled, Locked: c.Student.Locked,
		Today:  Today{XP: c.Today.XP, Goal: c.Today.Goal, GoalMet: c.Today.GoalMet, Streak: c.Today.Streak},
		Week:   Week{XP: c.WeekXP, Goal: c.WeekGoal, Days: []Day{}},
		Placed: c.Placed, NewBlocks: c.NewBlocks, LastActive: c.LastActive, Stuck: ref(c.Stuck),
	}
	for _, d := range learning.RestDays(c.Student.Settings.RestDays) {
		s.RestDays = append(s.RestDays, int(d))
	}
	slices.Sort(s.RestDays)
	for _, d := range c.Week {
		s.Week.Days = append(s.Week.Days, Day{Date: d.Date, XP: d.XP, Goal: d.Goal, Met: d.Met, Rest: d.Rest, Future: d.Future})
	}
	return s
}

func topicsDTO(v service.TowerView) []Topic {
	out := []Topic{}
	for _, tier := range v.Tiers {
		for _, f := range tier.Faces {
			for _, b := range f.Blocks {
				out = append(out, Topic{ID: b.Topic.ID, Title: b.Topic.Title.In(service.Locale), Grade: b.Topic.Grade,
					Strand: b.Topic.Strand, State: b.State, FirstPlaced: b.FirstPlaced, ReviewsPassed: b.Mended, NextReview: b.Due})
			}
		}
	}
	return out
}

func activityDTO(in []service.Activity) []Activity {
	out := make([]Activity, 0, len(in))
	for _, a := range in {
		out = append(out, Activity{At: a.At, Kind: a.Kind, Title: a.Title, Detail: a.Detail, Passed: a.Passed})
	}
	return out
}

func strugglesDTO(in []service.Struggle) []Struggle {
	out := make([]Struggle, 0, len(in))
	for _, s := range in {
		out = append(out, Struggle{Topic: *ref(s.Topic), Stops: s.Stops, FailedReviews: s.FailedReviews, Last: s.Last})
	}
	return out
}

func (a *API) worksheetDTO(d service.DrillView) Worksheet {
	return Worksheet{ID: d.ID, Title: d.Title, About: drill.Describe(d.Settings), URL: a.baseURL + "/worksheets/" + d.ID,
		Problems: d.Problems, Settings: d.Settings}
}
