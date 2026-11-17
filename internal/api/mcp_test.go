package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chinny-labs/zigrt/internal/api"
)

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

// session connects an MCP client with the named token.
func (e *env) session(t *testing.T, token string) (*mcp.ClientSession, error) {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             e.srv.URL + api.MCPPath,
		HTTPClient:           &http.Client{Transport: bearerTransport{e.secret[token]}},
		DisableStandaloneSSE: true,
	}, nil)
	if err == nil {
		t.Cleanup(func() { cs.Close() })
	}
	return cs, err
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	slices.Sort(names)
	return names
}

// call runs a tool and decodes its structured result into out. It returns
// the error text of a failed call.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return b.String()
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, raw)
		}
	}
	return ""
}

func TestMCP(t *testing.T) {
	e := newEnv(t)
	if _, err := e.session(t, "none"); err == nil {
		t.Fatal("connected without a token")
	}

	read, err := e.session(t, "read")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(toolNames(t, read)); got != "[get_student get_system list_activity list_struggles list_students list_topics]" {
		t.Errorf("read tools: %s", got)
	}
	manage, err := e.session(t, "manage")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(toolNames(t, manage)); got != 8 {
		t.Errorf("manage has %d tools, want 8", got)
	}

	var list struct{ Students []api.Student }
	if msg := call(t, read, "list_students", nil, &list); msg != "" || len(list.Students) != 2 {
		t.Fatalf("list_students: %q %+v", msg, list)
	}
	s := list.Students[0]
	// A student by name, ignoring case.
	var one api.Student
	if msg := call(t, read, "get_student", map[string]any{"student": strings.ToUpper(s.Name)}, &one); msg != "" || one.ID != s.ID {
		t.Errorf("get_student by name: %q %+v", msg, one)
	}
	if msg := call(t, read, "get_student", map[string]any{"student": "Nobody"}, nil); !strings.HasPrefix(msg, "invalid: No student is called") {
		t.Errorf("unknown name: %q", msg)
	}
	var topics struct{ Topics []api.Topic }
	if call(t, read, "list_topics", map[string]any{"student": s.ID}, &topics); len(topics.Topics) != 30 {
		t.Errorf("list_topics: %d", len(topics.Topics))
	}
	var st struct{ Days int }
	if call(t, read, "list_struggles", map[string]any{"student": s.Name}, &st); st.Days != 30 {
		t.Errorf("list_struggles default days: %d", st.Days)
	}

	// Manage: settings by name, and errors the model can act on.
	var changed api.Student
	if msg := call(t, manage, "update_student_settings", map[string]any{"student": s.Name, "daily_xp_goal": 30}, &changed); msg != "" || changed.DailyXPGoal != 30 {
		t.Errorf("update_student_settings: %q %+v", msg, changed)
	}
	if msg := call(t, manage, "update_student_settings", map[string]any{"student": s.Name, "daily_xp_goal": 0}, nil); msg != "invalid: Set a daily goal from 1 to 120 XP. (field: daily_xp_goal)" {
		t.Errorf("a bad goal: %q", msg)
	}
	var ws api.Worksheet
	if msg := call(t, manage, "create_worksheet", map[string]any{"settings": map[string]any{
		"ops": map[string]any{"mul": map[string]any{"on": true, "level": "facts10"}}, "formats": []string{"across"}, "count": 10}}, &ws); msg != "" || len(ws.Problems) != 10 {
		t.Errorf("create_worksheet: %q %+v", msg, ws)
	}
	// A read token can't reach a manage tool even by name.
	if res, err := read.CallTool(context.Background(), &mcp.CallToolParams{Name: "update_student_settings",
		Arguments: map[string]any{"student": s.Name, "daily_xp_goal": 5}}); err == nil && !res.IsError {
		t.Error("a read token changed a setting")
	}
}
