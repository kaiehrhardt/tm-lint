package lint

import (
	"os"
	"path/filepath"
	"testing"
)

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
		path := filepath.Join(dir, "ignore")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	ign, err := LoadIgnoreFile(write("unused-let,unused-global legacy  # trailing comment\nglobal.ci.*\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		f    Finding
		rel  string
		want bool
	}{
		{Finding{Rule: "unused-let"}, "legacy/a.tm", true},
		{Finding{Rule: "invalid-stack-ref"}, "legacy/a.tm", false},
		{Finding{Rule: "undefined-global", Global: []string{"ci", "token"}}, "x.tm", true},
		{Finding{Rule: "undefined-global", Global: []string{"cix"}}, "x.tm", false},
		{Finding{Rule: "invalid-stack-ref"}, "x.tm", false},
	}
	for _, c := range cases {
		if got := ign.Matches(c.f, c.rel); got != c.want {
			t.Errorf("Matches(%+v, %q) = %v, want %v", c.f, c.rel, got, c.want)
		}
	}

	all, err := LoadIgnoreFile(write("unused-global global\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !all.Matches(Finding{Rule: "unused-global", Global: []string{"any", "thing"}}, "x.tm") {
		t.Error("a bare `global` target must match every global")
	}

	if _, err := LoadIgnoreFile(write("no-such-rule foo\n"), true); err == nil {
		t.Error("unknown rule must be an error")
	}
	if _, err := LoadIgnoreFile(write("a b c\n"), true); err == nil {
		t.Error("more than two fields must be an error")
	}
	if _, err := LoadIgnoreFile(filepath.Join(dir, "missing"), true); err == nil {
		t.Error("missing explicit ignore file must be an error")
	}
	if _, err := LoadIgnoreFile(filepath.Join(dir, "missing"), false); err != nil {
		t.Errorf("missing default ignore file must be fine, got %v", err)
	}

	var none *IgnoreFile
	if none.Matches(Finding{Rule: "unused-let"}, "x.tm") {
		t.Error("a nil ignore file must match nothing")
	}
}
