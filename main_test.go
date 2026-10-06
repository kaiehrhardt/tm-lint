package main

import (
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/example.expected")

// TestExample lints testdata/example, which contains a case for every rule,
// and compares the output with testdata/example.expected.
// Run `go test -update` after intentional changes.
func TestExample(t *testing.T) {
	p, err := loadProject("testdata/example")
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := selectRules("", "")
	if err != nil {
		t.Fatal(err)
	}
	opts, err := newOptions(p, "", "global.net.optional_*")
	if err != nil {
		t.Fatal(err)
	}
	got := formatFindings(p, lint(p, opts, enabled))

	const golden = "testdata/example.expected"
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestTagFilter(t *testing.T) {
	cases := []struct {
		filter string
		tags   []string
		want   bool
	}{
		{"a", []string{"a"}, true},
		{"a:b", []string{"a"}, false},
		{"a:b", []string{"a", "b"}, true},
		{"a,b", []string{"b"}, true},
		{"~a", []string{"b"}, true},
		{"~a", []string{"a"}, false},
		{"a:~b,c", []string{"c"}, true},
		{"a:~b,c", []string{"a", "b"}, false},
	}
	for _, c := range cases {
		if got := matchTagFilter(c.filter, c.tags); got != c.want {
			t.Errorf("matchTagFilter(%q, %v) = %v, want %v", c.filter, c.tags, got, c.want)
		}
	}
}

func TestLevenshtein(t *testing.T) {
	if d := levenshtein("regoin", "region"); d != 2 {
		t.Errorf("got %d", d)
	}
	if got := closestName("cidrr", []string{"cidr", "unused_child"}); got != "cidr" {
		t.Errorf("got %q", got)
	}
	if got := closestName("app", []string{"abc"}); got != "" {
		t.Errorf("short names must only match at distance 1, got %q", got)
	}
}

func TestGlobToRegexp(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"stacks/c", "stacks/c/stack.tm.hcl", true},
		{"stacks/c", "stacks/cc/stack.tm.hcl", false},
		{"/stacks/c/", "stacks/c/x/y.tm", true},
		{"stacks/*.tm.hcl", "stacks/a.tm.hcl", true},
		{"stacks/*.tm.hcl", "stacks/a/b.tm.hcl", false},
		{"stacks/**/*.tm.hcl", "stacks/a.tm.hcl", true},
		{"stacks/**/*.tm.hcl", "stacks/a/b/c.tm.hcl", true},
		{"**/legacy", "x/y/legacy/stack.tm.hcl", true},
		{"**/legacy", "legacy/stack.tm.hcl", true},
		{"imports/common.tm.hcl", "imports/common.tm.hcl", true},
		{"a.b", "axb", false},
	}
	for _, c := range cases {
		re, err := globToRegexp(c.glob)
		if err != nil {
			t.Fatal(err)
		}
		if got := re.MatchString(c.path); got != c.want {
			t.Errorf("glob %q on %q = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}

func TestIgnoreFile(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) string {
		path := dir + "/ignore"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	ign, err := loadIgnoreFile(write("unused-let,unused-global legacy  # trailing comment\nglobal.ci.*\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		f    finding
		rel  string
		want bool
	}{
		{finding{rule: "unused-let"}, "legacy/a.tm", true},
		{finding{rule: "invalid-stack-ref"}, "legacy/a.tm", false},
		{finding{rule: "undefined-global", global: []string{"ci", "token"}}, "x.tm", true},
		{finding{rule: "undefined-global", global: []string{"cix"}}, "x.tm", false},
		{finding{rule: "invalid-stack-ref"}, "x.tm", false},
	}
	for _, c := range cases {
		if got := ign.matches(c.f, c.rel); got != c.want {
			t.Errorf("matches(%+v, %q) = %v, want %v", c.f, c.rel, got, c.want)
		}
	}

	if _, err := loadIgnoreFile(write("no-such-rule foo\n"), true); err == nil {
		t.Error("unknown rule must be an error")
	}
	if _, err := loadIgnoreFile(dir+"/missing", true); err == nil {
		t.Error("missing explicit ignore file must be an error")
	}
	if _, err := loadIgnoreFile(dir+"/missing", false); err != nil {
		t.Errorf("missing default ignore file must be fine, got %v", err)
	}
}
