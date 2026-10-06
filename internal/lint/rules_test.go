package lint

import "testing"

func TestMatchTagFilter(t *testing.T) {
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

func TestClosestName(t *testing.T) {
	if d := levenshtein("regoin", "region"); d != 2 {
		t.Errorf("levenshtein = %d, want 2", d)
	}
	if got := closestName("cidrr", []string{"cidr", "unused_child"}); got != "cidr" {
		t.Errorf("got %q, want cidr", got)
	}
	if got := closestName("app", []string{"abc"}); got != "" {
		t.Errorf("short names must only match at distance 1, got %q", got)
	}
}

func TestGlobalPatterns(t *testing.T) {
	pats := ParseGlobalPatterns("global.ci.*, tags ,global.a.b")
	cases := []struct {
		path []string
		want bool
	}{
		{[]string{"ci", "token"}, true},
		{[]string{"ci"}, true},
		{[]string{"tags"}, true},
		{[]string{"tags", "x"}, false},
		{[]string{"a", "b"}, true},
		{[]string{"a"}, false},
	}
	for _, c := range cases {
		if got := matchesGlobalPattern(c.path, pats); got != c.want {
			t.Errorf("matchesGlobalPattern(%v) = %v, want %v", c.path, got, c.want)
		}
	}
}
