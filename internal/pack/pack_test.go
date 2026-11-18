package pack

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/chinny-labs/zigrt/problem"
)

// The embedded pack is the pinned release, loads, and passes every gate:
// the app must never ship a pack its own checks reject.
func TestEmbedded(t *testing.T) {
	p, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	l, _ := ReadLock()
	if p.Manifest.Version != l.Version || p.Manifest.ID != "zigrt-k5" || len(p.Topics) < 30 {
		t.Fatalf("pack %s %s with %d topics", p.Manifest.ID, p.Manifest.Version, len(p.Topics))
	}
	if errs := p.Gate(200); len(errs) > 0 {
		t.Fatalf("gates: %v", errs)
	}
	// Every answer type, layout and catalogue figure is in it (the sample
	// pack's purpose).
	types, layouts, figures := map[problem.AnswerType]bool{}, map[problem.Layout]bool{}, map[string]bool{}
	for _, tpl := range p.Templates() {
		pr, err := tpl.Generate(1, "en")
		if err != nil {
			t.Fatal(err)
		}
		types[pr.Answer.Type], layouts[pr.Layout], figures[pr.Figure] = true, true, true
	}
	if len(types) != 6 || len(layouts) != 6 {
		t.Errorf("answer types %v, layouts %v", types, layouts)
	}
	for name := range problem.Figures {
		if !figures[name] && !kFigures[name] {
			t.Errorf("no %s figure", name)
		}
	}
}

func TestParseLock(t *testing.T) {
	sum := strings.Repeat("a", 64)
	l, err := ParseLock("# c\nversion 2027.01.2\nfile x.zip\nurl https://e/x.zip\nsha256 " + sum + "\n")
	if err != nil || l.Version != "2027.01.2" || l.File != "x.zip" || l.SHA256 != sum {
		t.Fatalf("%+v %v", l, err)
	}
	for _, bad := range []string{"version 1\nfile x\nsha256 short", "colour blue\n", ""} {
		if _, err := ParseLock(bad); err == nil {
			t.Errorf("parsed %q", bad)
		}
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"2026.11.2", "2026.11.1", true},
		{"2026.12.1", "2026.11.9", true},
		{"2027.01.1", "2026.12.10", true},
		{"2026.11.10", "2026.11.9", true},
		{"2026.11.1", "2026.11.1", false},
		{"2026.10.5", "2026.11.1", false},
		{"bad", "2026.11.1", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

// writePack unpacks the embedded pack into dir/name with another version
// and minimum app version.
func writePack(t *testing.T, dir, name, version, minApp string) {
	t.Helper()
	l, err := ReadLock()
	if err != nil {
		t.Fatal(err)
	}
	b, err := files.ReadFile(l.File)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "manifest.yaml" {
			data = regexp.MustCompile(`(?m)^version: .*$`).ReplaceAll(data, []byte("version: "+version))
			data = regexp.MustCompile(`(?m)^min_app_version: .*$`).ReplaceAll(data, []byte("min_app_version: "+minApp))
		}
		path := filepath.Join(dir, name, f.Name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSelect(t *testing.T) {
	// No folder, or an empty one: the embedded pack.
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		f, err := Select(dir, "0.2.0")
		if err != nil || f.Source != "embedded" {
			t.Fatalf("Select(%q) = %s, %v", dir, f.Source, err)
		}
	}

	// A newer pack in the folder wins; an older one and junk do not.
	dir := t.TempDir()
	writePack(t, dir, "old", "2026.01.1", "0.1.0")
	writePack(t, dir, "new", "2099.01.1", "0.2.0")
	if err := os.WriteFile(filepath.Join(dir, "broken.zip"), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Select(dir, "0.2.0")
	if err != nil || f.Pack.Manifest.Version != "2099.01.1" || f.Source != filepath.Join(dir, "new") {
		t.Fatalf("newer: %s from %s, %v", f.Pack.Manifest.Version, f.Source, err)
	}
	if len(f.Skip) != 1 || !strings.Contains(f.Skip[0], "broken.zip") {
		t.Errorf("skipped %v", f.Skip)
	}
	// A development build runs any pack.
	if _, err := Select(dir, "dev"); err != nil {
		t.Errorf("dev build: %v", err)
	}

	// The newest pack needs a newer app: refuse, rather than quietly run
	// an older pack than the operator installed.
	writePack(t, dir, "future", "2099.02.1", "9.0.0")
	f, err = Select(dir, "0.2.0")
	if err == nil || f.Pack.Manifest.Version != "2099.02.1" || !strings.Contains(err.Error(), "needs Zigrt 9.0.0") {
		t.Fatalf("too new: %v", err)
	}
}

// kFigures were added for the kindergarten lessons, which are not in the
// pinned pack yet. Remove this once a pack with the K lessons is pinned.
var kFigures = map[string]bool{
	"dots": true, "dot-groups": true, "ten-frame": true, "shape": true, "shape-mix": true,
	"solid": true, "compare": true, "balance": true, "position": true,
	"base-ten": true, "ruler": true, "table": true, "cut-shape": true,
	"coins": true, "picture-graph": true, "bar-graph": true, "line-plot": true, "grid": true,
	"polygon": true, "dial": true, "jug": true, "split-rectangle": true,
	"lines": true, "angle": true, "protractor": true, "symmetry": true, "triangle": true,
}
