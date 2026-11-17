// SPDX-License-Identifier: Apache-2.0

package content

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/chinny-labs/zigrt/problem"
)

// TestDocExamples parses and gates every template example in the authoring
// guide, so the guide cannot drift from the code.
func TestDocExamples(t *testing.T) {
	b, err := os.ReadFile("../docs/content/templates.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(string(b), -1)
	n := 0
	for _, m := range blocks {
		src := m[1]
		if !strings.HasPrefix(src, "id: ") || strings.Contains(src, "\nkps:") {
			continue // a pack file, not a template
		}
		n++
		tpl, err := problem.ParseTemplate([]byte(src))
		if err != nil {
			t.Errorf("example %d: %v", n, err)
			continue
		}
		if err := tpl.CheckFigure(nil); err != nil {
			t.Error(err)
		}
		for _, err := range tpl.Gate(300) {
			t.Error(err)
		}
	}
	if n < 6 {
		t.Errorf("found %d template examples", n)
	}
}
