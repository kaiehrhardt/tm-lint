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
	got := formatFindings(p, lint(p, &options{ignoreGlobals: parseGlobalPatterns("global.net.optional_*")}, enabled))

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
