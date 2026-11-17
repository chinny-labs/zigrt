package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/chinny-labs/zigrt/internal/buildinfo"
	"github.com/chinny-labs/zigrt/internal/drill"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
)

func (a *API) routeAll() {
	a.handle("GET", "/openapi.json", "", func(w http.ResponseWriter, _ *http.Request, _ *principal) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(openapiJSON)
	})
	a.handle("GET", "/system", service.ScopeRead, a.getSystem)
	a.handle("GET", "/students", service.ScopeRead, a.listStudents)
	a.handle("GET", "/students/{id}", service.ScopeRead, a.getStudent)
	a.handle("GET", "/students/{id}/topics", service.ScopeRead, a.getTopics)
	a.handle("GET", "/students/{id}/activity", service.ScopeRead, a.getActivity)
	a.handle("GET", "/students/{id}/struggles", service.ScopeRead, a.getStruggles)
	a.handle("PATCH", "/students/{id}/settings", service.ScopeManage, a.patchSettings)
	a.handle("POST", "/worksheets", service.ScopeManage, a.createWorksheet)
}

// --- reads ---------------------------------------------------------------------

func (a *API) system() System {
	s := System{Version: buildinfo.Version, Healthy: true, PackSource: a.svc.PackSource}
	if p := a.svc.Pack(); p != nil {
		s.PackID, s.PackVersion, s.Topics = p.Manifest.ID, p.Manifest.Version, len(p.Topics)
	} else {
		s.Healthy = false
	}
	if s.PackSource == "" && s.PackID != "" {
		s.PackSource = "embedded"
	}
	return s
}

func (a *API) getSystem(w http.ResponseWriter, _ *http.Request, _ *principal) {
	writeJSON(w, http.StatusOK, a.system())
}

// The operations, shared by the REST handlers and the MCP tools.

func (a *API) students(ctx context.Context, p *principal) ([]Student, error) {
	cards, err := a.svc.Dashboard(ctx, p.User)
	if err != nil {
		return nil, err
	}
	out := make([]Student, 0, len(cards))
	for _, c := range cards {
		out = append(out, studentDTO(c))
	}
	return out, nil
}

func (a *API) student(ctx context.Context, p *principal, id string) (Student, error) {
	rep, err := a.svc.StudentReport(ctx, p.User, id)
	return studentDTO(rep.StudentCard), err
}

func (a *API) topics(ctx context.Context, p *principal, id string) ([]Topic, error) {
	v, err := a.svc.StudentTower(ctx, p.User, id)
	return topicsDTO(v), err
}

func (a *API) activity(ctx context.Context, p *principal, id string) ([]Activity, error) {
	rep, err := a.svc.StudentReport(ctx, p.User, id)
	return activityDTO(rep.Activity), err
}

// defaultStruggleDays is how far back "repeated failures" looks by default.
const defaultStruggleDays = 30

func (a *API) struggles(ctx context.Context, p *principal, id string, days int) ([]Struggle, error) {
	if days < 1 || days > 365 {
		return nil, &service.UserError{Field: "days", Msg: "days must be a whole number from 1 to 365."}
	}
	st, err := a.svc.Struggles(ctx, p.User, id, days)
	return strugglesDTO(st), err
}

// --- REST reads ------------------------------------------------------------------

func (a *API) listStudents(w http.ResponseWriter, r *http.Request, p *principal) {
	v, err := a.students(r.Context(), p)
	a.respond(w, r, err, map[string]any{"students": v})
}

func (a *API) getStudent(w http.ResponseWriter, r *http.Request, p *principal) {
	v, err := a.student(r.Context(), p, r.PathValue("id"))
	a.respond(w, r, err, v)
}

func (a *API) getTopics(w http.ResponseWriter, r *http.Request, p *principal) {
	v, err := a.topics(r.Context(), p, r.PathValue("id"))
	a.respond(w, r, err, map[string]any{"topics": v})
}

func (a *API) getActivity(w http.ResponseWriter, r *http.Request, p *principal) {
	v, err := a.activity(r.Context(), p, r.PathValue("id"))
	a.respond(w, r, err, map[string]any{"activity": v})
}

func (a *API) getStruggles(w http.ResponseWriter, r *http.Request, p *principal) {
	days := defaultStruggleDays
	if q := r.URL.Query().Get("days"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil {
			n = -1
		}
		days = n
	}
	v, err := a.struggles(r.Context(), p, r.PathValue("id"), days)
	a.respond(w, r, err, map[string]any{"days": days, "struggles": v})
}

// respond writes v, or the error.
func (a *API) respond(w http.ResponseWriter, r *http.Request, err error, v any) {
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// --- manage --------------------------------------------------------------------

// SettingsPatch changes some of a student's learning settings; fields left
// out keep their values.
type SettingsPatch struct {
	CourseID    *string `json:"course_id,omitempty" jsonschema:"a course ID from the content pack"`
	DailyXPGoal *int    `json:"daily_xp_goal,omitempty" jsonschema:"XP a day, 1 to 120"`
	RestDays    *[]int  `json:"rest_days,omitempty" jsonschema:"weekdays with no goal, 0 = Sunday; at least one day must not be a rest day"`
	Fluency     *bool   `json:"fluency,omitempty" jsonschema:"whether fact topics also check speed"`
}

func (a *API) applySettings(ctx context.Context, p *principal, id string, in SettingsPatch) (Student, error) {
	sv, err := a.svc.Student(ctx, id)
	if err != nil {
		return Student{}, err
	}
	up := service.StudentUpdate{DisplayName: sv.DisplayName, Avatar: sv.Avatar, CourseID: sv.Settings.CourseID,
		DailyXPGoal: sv.Settings.DailyXPGoal, RestDays: sv.Settings.RestDays, Fluency: sv.Settings.FluencyEnabled}
	if in.CourseID != nil {
		up.CourseID = *in.CourseID
	}
	if in.DailyXPGoal != nil {
		up.DailyXPGoal = *in.DailyXPGoal
	}
	if in.RestDays != nil {
		up.RestDays = ""
		for _, d := range *in.RestDays {
			if d < 0 || d > 6 {
				return Student{}, &service.UserError{Field: "rest_days", Msg: "Rest days are weekday numbers from 0 (Sunday) to 6."}
			}
			up.RestDays += strconv.Itoa(d)
		}
	}
	if in.Fluency != nil {
		up.Fluency = *in.Fluency
	}
	if err := a.svc.UpdateStudent(ctx, p.User, id, up); err != nil {
		return Student{}, err
	}
	return a.student(ctx, p, id)
}

func (a *API) patchSettings(w http.ResponseWriter, r *http.Request, p *principal) {
	var in SettingsPatch
	if !decode(w, r, &in) {
		return
	}
	v, err := a.applySettings(r.Context(), p, r.PathValue("id"), in)
	a.respond(w, r, err, v)
}

// WorksheetRequest builds a worksheet. Settings are the drill builder's:
// the operations to include with a level each, the layouts, and a count.
type WorksheetRequest struct {
	Title    string         `json:"title,omitempty"`
	Settings drill.Settings `json:"settings"`
}

func (a *API) createWorksheet(w http.ResponseWriter, r *http.Request, p *principal) {
	var in WorksheetRequest
	if !decode(w, r, &in) {
		return
	}
	v, err := a.worksheet(r.Context(), p, in)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (a *API) worksheet(ctx context.Context, p *principal, in WorksheetRequest) (Worksheet, error) {
	d, err := a.svc.CreateDrill(ctx, p.User, store.KindWorksheet, in.Settings, in.Title)
	if err != nil {
		return Worksheet{}, err
	}
	return a.worksheetDTO(d), nil
}
