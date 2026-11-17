package api_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/chinny-labs/zigrt/internal/service"
)

// TestSpecMatchesRoutes keeps the OpenAPI document honest: every route the
// API serves is in the spec with the same scope, and the spec lists nothing
// the API doesn't serve.
func TestSpecMatchesRoutes(t *testing.T) {
	e := newEnv(t)
	var spec struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			Security    []map[string][]string `json:"security"`
			Description string                `json:"description"`
		} `json:"paths"`
	}
	if e.call("", "GET", "/openapi.json", nil, &spec) != 200 || spec.OpenAPI != "3.1.0" {
		t.Fatal("spec not served")
	}
	var inSpec, inCode []string
	for path, ops := range spec.Paths {
		for method, op := range ops {
			key := strings.ToUpper(method) + " " + path
			inSpec = append(inSpec, key)
			for _, r := range e.api.Routes() {
				if r.Method+" "+r.Path != key {
					continue
				}
				if (len(op.Security) > 0) != (r.Scope != "") {
					t.Errorf("%s: spec security %v disagrees with scope %q", key, op.Security, r.Scope)
				}
				if r.Scope != "" && !strings.Contains(op.Description, "`"+r.Scope+"`") {
					t.Errorf("%s: the description should name the %q scope", key, r.Scope)
				}
			}
		}
	}
	for _, r := range e.api.Routes() {
		inCode = append(inCode, r.Method+" "+r.Path)
		switch r.Scope {
		case "", service.ScopeRead, service.ScopeManage:
		default:
			t.Errorf("%s %s: unknown scope %q", r.Method, r.Path, r.Scope)
		}
	}
	slices.Sort(inSpec)
	slices.Sort(inCode)
	if a, b := fmt.Sprint(inSpec), fmt.Sprint(inCode); a != b {
		t.Fatalf("spec and routes differ\nspec: %s\ncode: %s", a, b)
	}
}
