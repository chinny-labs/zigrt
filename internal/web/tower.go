package web

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
)

// The tower (S12): tiers by grade, the faces (strands) of one tier, the
// blocks of one face, and one block's story. Everything is a link, so it
// works without JavaScript.

type towerData struct {
	T      service.TowerView
	Tiers  []towerTierView // top first
	Grade  string
	Faces  []faceChip
	Blocks []blockView
	Detail *blockDetail
}

type towerTierView struct {
	Label, State, Href string
	Width              int
	Placed, Total      int
	Finished, Selected bool
}

type faceChip struct {
	Title, Href        string
	Selected, Finished bool
}

type blockView struct {
	Title, State, Href string
	Selected           bool
}

type blockDetail struct {
	Title, State, Placed string
	Mended               int
	Action               *taskCard
	ActionLabel          string
}

// stateWords are the student's words for each block state (S12 legend).
var stateWords = map[string]string{
	service.BlockPlaced: "Placed", service.BlockMend: "Needs mending", service.BlockBuilding: "Building",
	service.BlockReady: "Ready", service.BlockLater: "Later",
}

func (s *Server) towerPage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	tv, err := s.svc.Tower(r.Context(), *c.User)
	if err != nil {
		s.taskFail(w, r, err)
		return
	}
	q := r.URL.Query()
	d := towerData{T: tv, Grade: q.Get("grade")}

	// The tier shown: the one asked for, else the one being built, else the top.
	sel := -1
	for i, t := range tv.Tiers {
		if t.Grade == d.Grade {
			sel = i
		}
	}
	if sel < 0 {
		for i, t := range tv.Tiers {
			if t.Placed < t.Total {
				sel = i
				break
			}
		}
	}
	if sel < 0 && len(tv.Tiers) > 0 {
		sel = len(tv.Tiers) - 1
	}
	here := false
	for i, t := range tv.Tiers {
		label := gradeName(t.Grade)
		if 6-i <= 1 {
			label = t.Grade // the narrow top tier has room for the number only
		}
		v := towerTierView{Label: label, Width: max(6-i, 1), Placed: t.Placed, Total: t.Total,
			Finished: t.Finished, Selected: i == sel, Href: "/tower?grade=" + url.QueryEscape(t.Grade), State: "idle"}
		switch {
		case t.Finished || t.Placed == t.Total:
			v.State = "done"
		case !here:
			v.State, here = "here", true
		}
		d.Tiers = append([]towerTierView{v}, d.Tiers...)
	}
	if sel < 0 {
		v := s.newView(w, r, "My tower", d)
		v.Shell, v.Nav = "student", "tower"
		s.render(w, r, http.StatusOK, "tower", v)
		return
	}
	tier := tv.Tiers[sel]
	d.Grade = tier.Grade

	// The face shown, and its blocks.
	face := 0
	for i, f := range tier.Faces {
		if f.Strand.ID == q.Get("strand") {
			face = i
		}
	}
	base := "/tower?grade=" + url.QueryEscape(tier.Grade)
	for i, f := range tier.Faces {
		d.Faces = append(d.Faces, faceChip{Title: f.Strand.Title.In(service.Locale), Selected: i == face, Finished: f.Finished,
			Href: base + "&strand=" + url.QueryEscape(f.Strand.ID)})
	}
	topic := q.Get("topic")
	fbase := base + "&strand=" + url.QueryEscape(tier.Faces[face].Strand.ID)
	for _, b := range tier.Faces[face].Blocks {
		bv := blockView{Title: b.Topic.Title.In(service.Locale), State: b.State, Selected: b.Topic.ID == topic,
			Href: fbase + "&topic=" + url.QueryEscape(b.Topic.ID)}
		d.Blocks = append(d.Blocks, bv)
		if bv.Selected {
			d.Detail = s.blockDetail(b, tv.Next, s.svc.Now())
		}
	}
	v := s.newView(w, r, "My tower", d)
	v.Shell, v.Nav = "student", "tower"
	s.render(w, r, http.StatusOK, "tower", v)
}

func (s *Server) blockDetail(b service.Block, next []service.NextTask, now time.Time) *blockDetail {
	d := &blockDetail{Title: b.Topic.Title.In(service.Locale), State: stateWords[b.State], Mended: b.Mended}
	if b.FirstPlaced != nil {
		d.Placed = "Placed " + ago(now.Sub(*b.FirstPlaced))
	}
	for _, n := range next {
		if n.Topic == nil || n.Topic.ID != b.Topic.ID {
			continue
		}
		c := s.card(n)
		d.Action = &c
		switch {
		case n.Kind == store.TaskReview:
			d.ActionLabel = fmt.Sprintf("Mend it now · %d XP", c.XP)
		case n.Resume:
			d.ActionLabel = fmt.Sprintf("Keep building · %d XP", c.XP)
		default:
			d.ActionLabel = fmt.Sprintf("Start this block · %d XP", c.XP)
		}
		break
	}
	return d
}

// ago says how long ago, the way a child would: today, yesterday, 3 days
// ago, 2 weeks ago, 4 months ago.
func ago(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days < 1:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 14:
		return fmt.Sprintf("%d days ago", days)
	case days < 60:
		return fmt.Sprintf("%d weeks ago", days/7)
	}
	return fmt.Sprintf("%d months ago", days/30)
}
