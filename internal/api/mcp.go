package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chinny-labs/zigrt/internal/buildinfo"
	"github.com/chinny-labs/zigrt/internal/service"
)

// MCPPath is where the MCP server is mounted.
const MCPPath = "/mcp"

// MCPHandler serves the Model Context Protocol over streamable HTTP (ADR
// 16), with the same tokens, scopes, limits and operations as the JSON API.
// Each request builds a server holding only the tools the token may use, so
// a read token never sees a manage tool. It is stateless: every call is
// authenticated on its own.
func (a *API) MCPHandler() http.Handler {
	inner := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		p, _ := r.Context().Value(principalKey{}).(*principal)
		if p == nil {
			return nil
		}
		return a.mcpServer(p)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, Logger: a.log})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		p, ok := a.authenticate(w, r)
		if !ok {
			return
		}
		if ok, wait := a.lim.allow(p.Token.ID); !ok {
			tooMany(w, wait)
			return
		}
		ctx := context.WithValue(service.WithToken(r.Context(), p.Token.ID), principalKey{}, p)
		inner.ServeHTTP(w, r.WithContext(ctx))
	})
}

type principalKey struct{}

// toolError turns a service error into a tool failure the model can act on:
// a stable code, then the message.
func (a *API) toolError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	p := classify(err)
	if p.status == http.StatusInternalServerError {
		a.log.Error("mcp tool failed", "err", err)
		return errors.New("internal: something went wrong; it was logged")
	}
	msg := p.code + ": " + p.msg
	if p.field != "" {
		msg += " (field: " + p.field + ")"
	}
	return errors.New(msg)
}

// resolve finds a student by ID or by name, ignoring case, so a model can
// say "Ava".
func (a *API) resolve(ctx context.Context, nameOrID string) (string, error) {
	students, err := a.svc.Students(ctx)
	if err != nil {
		return "", err
	}
	nameOrID = strings.TrimSpace(nameOrID)
	var match []string
	for _, s := range students {
		if s.ID == nameOrID {
			return s.ID, nil
		}
		if strings.EqualFold(s.DisplayName, nameOrID) {
			match = append(match, s.ID)
		}
	}
	switch len(match) {
	case 1:
		return match[0], nil
	case 0:
		return "", &service.UserError{Field: "student", Msg: "No student is called \"" + nameOrID + "\". Call list_students for their names."}
	}
	return "", &service.UserError{Field: "student", Msg: "More than one student is called \"" + nameOrID + "\". Use their ID from list_students."}
}

// --- tool inputs and outputs -----------------------------------------------------

type studentArg struct {
	Student string `json:"student" jsonschema:"a student's name (like Ava) or ID"`
}

type strugglesIn struct {
	Student string `json:"student" jsonschema:"a student's name or ID"`
	Days    int    `json:"days,omitempty" jsonschema:"how many days back to look (default 30, up to 365)"`
}

type settingsIn struct {
	Student string `json:"student" jsonschema:"a student's name or ID"`
	SettingsPatch
}

type studentsOut struct {
	Students []Student `json:"students"`
}

type topicsOut struct {
	Topics []Topic `json:"topics"`
}

type activityOut struct {
	Activity []Activity `json:"activity"`
}

type strugglesOut struct {
	Days      int        `json:"days"`
	Struggles []Struggle `json:"struggles"`
}

const instructions = "Zigrt is a family maths practice app for children from kindergarten to grade 5. " +
	"Students are addressed by name or ID. Topics are blocks in a tower: placed means mastered, mend means a review is due. " +
	"XP is earned for focused practice, about one XP a minute; each student has a daily XP goal. " +
	"These tools read progress and change a few settings; they never answer problems for a child."

func (a *API) mcpServer(p *principal) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "zigrt", Version: buildinfo.Version}, &mcp.ServerOptions{Instructions: instructions})
	read := &mcp.ToolAnnotations{ReadOnlyHint: true}
	change := &mcp.ToolAnnotations{DestructiveHint: boolp(false), IdempotentHint: true}
	create := &mcp.ToolAnnotations{DestructiveHint: boolp(false)}

	// withStudent resolves the student argument before calling f.
	withStudent := func(ctx context.Context, who string, f func(id string) error) error {
		id, err := a.resolve(ctx, who)
		if err != nil {
			return a.toolError(err)
		}
		return a.toolError(f(id))
	}

	mcp.AddTool(s, &mcp.Tool{Name: "get_system", Description: "Zigrt's version and the content pack in use.", Annotations: read},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, System, error) {
			return nil, a.system(), nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_students", Description: "Every student with today's XP against their goal, " +
		"their streak, this week's XP day by day (Monday to Sunday), topics mastered, new this week, when last active, and any lesson they are stuck on. " +
		"Use it for \"how did the kids do this week?\".", Annotations: read},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, studentsOut, error) {
			v, err := a.students(ctx, p)
			return nil, studentsOut{v}, a.toolError(err)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_student", Description: "One student's summary, as in list_students.", Annotations: read},
		func(ctx context.Context, _ *mcp.CallToolRequest, in studentArg) (*mcp.CallToolResult, Student, error) {
			var v Student
			err := withStudent(ctx, in.Student, func(id string) (err error) { v, err = a.student(ctx, p, id); return })
			return nil, v, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_topics", Description: "Every topic in a student's course with its state, by grade and strand.", Annotations: read},
		func(ctx context.Context, _ *mcp.CallToolRequest, in studentArg) (*mcp.CallToolResult, topicsOut, error) {
			var v []Topic
			err := withStudent(ctx, in.Student, func(id string) (err error) { v, err = a.topics(ctx, p, id); return })
			return nil, topicsOut{v}, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_activity", Description: "What a student did recently, newest first: lessons, reviews, quizzes, placement, drills and worksheets, with scores.", Annotations: read},
		func(ctx context.Context, _ *mcp.CallToolRequest, in studentArg) (*mcp.CallToolResult, activityOut, error) {
			var v []Activity
			err := withStudent(ctx, in.Student, func(id string) (err error) { v, err = a.activity(ctx, p, id); return })
			return nil, activityOut{v}, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_struggles", Description: "Topics a student keeps finding hard: two or more lessons stopped or reviews failed in the period.", Annotations: read},
		func(ctx context.Context, _ *mcp.CallToolRequest, in strugglesIn) (*mcp.CallToolResult, strugglesOut, error) {
			out := strugglesOut{Days: in.Days}
			if out.Days == 0 {
				out.Days = defaultStruggleDays
			}
			err := withStudent(ctx, in.Student, func(id string) (err error) { out.Struggles, err = a.struggles(ctx, p, id, out.Days); return })
			return nil, out, err
		})

	if service.HasScope(p.Token, service.ScopeManage) {
		mcp.AddTool(s, &mcp.Tool{Name: "update_student_settings", Description: "Change a student's daily XP goal, rest days, course or fact-speed checks. " +
			"Fields left out keep their values. Returns the student's summary.", Annotations: change},
			func(ctx context.Context, _ *mcp.CallToolRequest, in settingsIn) (*mcp.CallToolResult, Student, error) {
				var v Student
				err := withStudent(ctx, in.Student, func(id string) (err error) { v, err = a.applySettings(ctx, p, id, in.SettingsPatch); return })
				return nil, v, err
			})
		mcp.AddTool(s, &mcp.Tool{Name: "create_worksheet", Description: "Make a printable arithmetic worksheet and return its URL and problems. " +
			"settings.ops maps add, sub, mul and div to {on, level}; levels are like sum10, sum20, d2d2 (add), within20 (sub), facts10 (mul, div). " +
			"settings.formats is any of across, stacked, missing; settings.count is 10 to 100.", Annotations: create},
			func(ctx context.Context, _ *mcp.CallToolRequest, in WorksheetRequest) (*mcp.CallToolResult, Worksheet, error) {
				v, err := a.worksheet(ctx, p, in)
				return nil, v, a.toolError(err)
			})
	}
	return s
}

func boolp(b bool) *bool { return &b }
