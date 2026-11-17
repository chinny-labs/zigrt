// Package learning holds the mastery rules (plan section 3): passing a
// knowledge point, the spaced-review schedule, implicit credit through
// encompasses edges, and choosing the next task. It is pure: it takes the
// graph, the states, a date and a config, and never reads the clock or the
// database. The service loads state, calls this package and writes what it
// returns.
package learning

import (
	"fmt"
	"slices"
	"time"
)

// Config holds every number the rules use, so they can be tuned in one
// place (plan section 3).
type Config struct {
	KPPassStreak int // right answers in a row that pass a knowledge point
	KPMaxMisses  int // wrong answers in one knowledge point that pause the lesson

	// Intervals is the days until the next review, by stage; stages past the
	// end use the last value.
	Intervals     []int
	FailStageDrop int // stages lost on a failed review
	FailsToReopen int // failed reviews in a row that reopen the lesson

	// PausesBeforeReview is how many pauses of one lesson bring its
	// prerequisites' reviews forward and hold the lesson until they are done.
	PausesBeforeReview int

	ReviewProblems    int // problems in a review
	ReviewPassPercent int // share right that passes a review

	ReviewBaseXP       int // a passed review
	PerfectReviewBonus int // extra XP when every answer was right
	LessonBonusPercent int // extra XP when no knowledge point needed more than one retry
	DefaultLessonXP    int // when the pack gives a topic none

	MaxReviewsInRow int // reviews in a row before a lesson goes first
	MaxTasks        int // tasks on the student home

	FastAnswer time.Duration // answers quicker than this count as rushed
	FastRun    int           // rushed answers in a row that pause the task

	DrillXPPer      int // correct drill answers per XP
	DrillXPDailyCap int // drill XP a day
}

// Default is the plan's tuning.
func Default() Config {
	return Config{
		KPPassStreak:       2,
		KPMaxMisses:        3,
		Intervals:          []int{1, 3, 7, 14, 30, 60, 120, 240},
		FailStageDrop:      2,
		FailsToReopen:      2,
		PausesBeforeReview: 2,
		ReviewProblems:     5,
		ReviewPassPercent:  80,
		ReviewBaseXP:       4,
		PerfectReviewBonus: 1,
		LessonBonusPercent: 20,
		DefaultLessonXP:    10,
		MaxReviewsInRow:    3,
		MaxTasks:           4,
		FastAnswer:         1500 * time.Millisecond,
		FastRun:            3,
		DrillXPPer:         10,
		DrillXPDailyCap:    5,
	}
}

// Interval is the days until the next review at a stage.
func (c Config) Interval(stage int) int {
	return c.Intervals[min(max(stage, 0), len(c.Intervals)-1)]
}

// Date is a calendar day in the household's time zone, counted in days
// since 1970-01-01. Reviews fall due on a day, not at an instant.
type Date int64

const dateLayout = "2006-01-02"

// DateOf is the household-local day an instant falls on.
func DateOf(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// ParseDate reads YYYY-MM-DD.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return 0, fmt.Errorf("date %q: %w", s, err)
	}
	return Date(t.Unix() / 86400), nil
}

func (d Date) time() time.Time       { return time.Unix(int64(d)*86400, 0).UTC() }
func (d Date) String() string        { return d.time().Format(dateLayout) }
func (d Date) Weekday() time.Weekday { return d.time().Weekday() }
func (d Date) Add(days int) Date     { return d + Date(days) }
func (d Date) Sub(other Date) int    { return int(d - other) }
func (d Date) Start(loc *time.Location) time.Time {
	y, m, day := d.time().Date()
	return time.Date(y, m, day, 0, 0, 0, 0, loc)
}

// RestDays reads the student setting: weekday digits, 0 for Sunday, so "06"
// is weekends.
func RestDays(s string) []time.Weekday {
	var out []time.Weekday
	for _, r := range s {
		if r >= '0' && r <= '6' && !slices.Contains(out, time.Weekday(r-'0')) {
			out = append(out, time.Weekday(r-'0'))
		}
	}
	return out
}
