// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pack = "../../content/testdata/hardest"

func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCommands(t *testing.T) {
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"-pack", pack, "lint"}, 0, "ok: 20 topics, 24 templates"},
		{[]string{"-pack", pack, "lint", "long-division-2d"}, 0, "long-division-2d: ok, 1 knowledge points, 1 templates"},
		{[]string{"-pack", pack, "lint", "nope"}, 1, `no topic "nope"`},
		{[]string{"-pack", pack, "check", "-seeds", "50"}, 0, "pass every gate at 50 seeds"},
		{[]string{"-pack", pack, "sample", "long-division-1d.kp1.t1", "-seeds", "1-2"}, 0, "2198 ÷ 5 = 439 r 3"},
		{[]string{"-pack", pack, "sample", "-seeds", "1", "coordinate-points.kp1.t1"}, 0, "* 2. (4, 1)"},
		{[]string{"-pack", pack, "sample", "nope.t1"}, 1, "no template"},
		{[]string{"-pack", pack, "sample", "x", "-seeds", "9-1"}, 1, "range like 1-20"},
		{[]string{"-pack", pack, "graph", "-strand", "division"}, 0, "long-division-2d"},
		{[]string{"-pack", pack, "ids"}, 0, "volume-prism.kp1.t1"},
		{[]string{"funcs"}, 0, "gcd (2 or more values)"},
		{[]string{"-pack", "/nowhere", "lint"}, 1, "no such file"},
		{[]string{"spin"}, 2, "unknown command"},
		{nil, 2, "Usage"},
	}
	for _, c := range cases {
		code, out, errOut := cli(t, c.args...)
		if code != c.code || !strings.Contains(out+errOut, c.want) {
			t.Errorf("%v: exit %d, output %q; want exit %d with %q", c.args, code, out+errOut, c.code, c.want)
		}
	}
}

func TestBuildAndIDs(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(pack)); err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(dir, "dist")
	code, out, errOut := cli(t, "-pack", dir, "build", "-out", dist)
	if code != 0 || !strings.Contains(out, "zigrt-hardest-2026.11.0.zip") {
		t.Fatalf("build: %d %s %s", code, out, errOut)
	}
	sum, err := os.ReadFile(filepath.Join(dist, "zigrt-hardest-2026.11.0.zip.sha256"))
	if err != nil || string(sum) != out {
		t.Errorf("checksum file %q, printed %q", sum, out)
	}
	// The built file loads like the folder.
	if code, out, _ := cli(t, "-pack", filepath.Join(dist, "zigrt-hardest-2026.11.0.zip"), "lint"); code != 0 || !strings.Contains(out, "20 topics") {
		t.Errorf("lint of the pack file: %s", out)
	}
	if code, out, _ := cli(t, "-pack", dir, "ids", "-write"); code != 0 || !strings.Contains(out, "new)") {
		t.Errorf("ids -write: %s", out)
	}
	if _, out, _ := cli(t, "-pack", dir, "ids", "-write"); !strings.Contains(out, "(0 new)") {
		t.Errorf("a second ids -write added IDs: %s", out)
	}
	// Removing a released topic now fails the ids gate.
	_ = os.Remove(filepath.Join(dir, "topics", "volume-prism.yaml"))
	topics, _ := os.ReadFile(filepath.Join(dir, "topics.yaml"))
	i := strings.Index(string(topics), "  - id: volume-prism")
	j := strings.Index(string(topics), "  - {id: coordinate-points")
	_ = os.WriteFile(filepath.Join(dir, "topics.yaml"), []byte(string(topics[:i])+string(topics[j:])), 0o600)
	if code, _, errOut := cli(t, "-pack", dir, "check", "-seeds", "20"); code != 1 || !strings.Contains(errOut, "volume-prism was released and is gone") {
		t.Errorf("check after removing a released topic: %d %s", code, errOut)
	}
}

func TestLintTopicFilter(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(pack)); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "topics", "mult-fractions.yaml")
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, []byte(strings.Replace(string(b), "value: p}", "value: q}", 1)), 0o600)
	if code, _, errOut := cli(t, "-pack", dir, "lint", "mult-fractions"); code != 1 || !strings.Contains(errOut, `"q"`) {
		t.Errorf("lint mult-fractions: %d %s", code, errOut)
	}
	if code, out, _ := cli(t, "-pack", dir, "lint", "elapsed-time"); code != 0 || !strings.Contains(out, "1 other problems") {
		t.Errorf("lint elapsed-time: %d %s", code, out)
	}
}
