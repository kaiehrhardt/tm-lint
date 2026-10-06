package lint

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kaiehrhardt/tm-lint/internal/project"
)

// writeProject creates a Terramate project from a map of relative file paths
// to contents and loads it.
func writeProject(t *testing.T, files map[string]string) *project.Project {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := project.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// messages runs the given rules and returns "file:line: msg" per finding.
func messages(t *testing.T, p *project.Project, opts *Options, rules ...string) []string {
	t.Helper()
	enabled, err := SelectRules(rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range Run(p, opts, enabled) {
		out = append(out, p.RelFile(f.File)+":"+strconv.Itoa(f.Range.Start.Line)+": "+f.Msg)
	}
	return out
}

func TestSelectRules(t *testing.T) {
	all, err := SelectRules(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(Rules) {
		t.Errorf("default must enable all %d rules, got %v", len(Rules), all)
	}

	got, err := SelectRules(nil, []string{"unused-let"})
	if err != nil {
		t.Fatal(err)
	}
	if got["unused-let"] || len(got) != len(Rules)-1 {
		t.Errorf("disable did not work: %v", got)
	}

	got, err = SelectRules([]string{"unused-let", "invalid-stack-ref"}, []string{"invalid-stack-ref"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got["unused-let"] {
		t.Errorf("disable must win over enable: %v", got)
	}

	if _, err := SelectRules([]string{"nope"}, nil); err == nil {
		t.Error("unknown rule must be an error")
	}
}

func TestInlineSuppression(t *testing.T) {
	p := writeProject(t, map[string]string{
		"globals.tm.hcl": `
globals {
  # tm-lint:ignore
  a = 1
  # tm-lint:ignore unused-global
  b = 1
  # tm-lint:ignore undefined-global
  c = 1
  d = 1 # tm-lint:ignore
}
`,
	})
	got := messages(t, p, nil, "unused-global")
	want := []string{"globals.tm.hcl:8: global.c is never used"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnusedLetChain(t *testing.T) {
	p := writeProject(t, map[string]string{
		"stack.tm.hcl": `
generate_hcl "x.tf" {
  lets {
    a = 1
    b = let.a
    c = let.c
  }
  content {
    v = let.b
  }
}
`,
	})
	got := messages(t, p, nil, "unused-let")
	want := []string{`stack.tm.hcl:6: let.c is never used in generate_hcl "x.tf"`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", got, want)
	}
}
