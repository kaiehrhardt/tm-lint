package project

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeFiles creates files (relative path -> content) below a temp dir and
// returns its path.
func writeFiles(t *testing.T, files map[string]string) string {
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
	return root
}

func TestLoad(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"terramate.tm.hcl":         `terramate { required_version = ">= 0.9" }`,
		"stacks/a/stack.tm.hcl":    "stack {\n  tags = [\"prod\"]\n}\nimport {\n  source = \"/imports/*.tm.hcl\"\n}\n",
		"stacks/b/stack.tm":        "stack {}\nimport {\n  source = \"../../imports/common.tm.hcl\"\n}\n",
		"imports/common.tm.hcl":    "globals {\n  x = 1\n}\n",
		"skipped/.tmskip":          "",
		"skipped/stack.tm.hcl":     "stack {}",
		".hidden/stack.tm.hcl":     "stack {}",
		"stacks/a/main.tf":         "not terramate",
		"stacks/a/.hidden.tm.hcl":  "stack {}",
		"stacks/a/nested/x.tm.hcl": "globals {}",
	})
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	var files []string
	for _, f := range p.Files {
		files = append(files, p.RelFile(f))
	}
	wantFiles := []string{
		"imports/common.tm.hcl",
		"stacks/a/nested/x.tm.hcl",
		"stacks/a/stack.tm.hcl",
		"stacks/b/stack.tm",
		"terramate.tm.hcl",
	}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Errorf("files = %v, want %v", files, wantFiles)
	}

	var stacks []string
	for _, s := range p.Stacks {
		stacks = append(stacks, s.Dir)
	}
	if !reflect.DeepEqual(stacks, []string{"/stacks/a", "/stacks/b"}) {
		t.Errorf("stacks = %v", stacks)
	}
	if !reflect.DeepEqual(p.Stacks[0].Tags, []string{"prod"}) {
		t.Errorf("tags = %v", p.Stacks[0].Tags)
	}

	common := filepath.Join(root, "imports", "common.tm.hcl")
	if !p.IsImported(common) {
		t.Error("imports/common.tm.hcl must be imported")
	}
	if got, want := p.Contexts(common), []string{"/imports", "/stacks/a", "/stacks/b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Contexts = %v, want %v", got, want)
	}
}

func TestLoadParseError(t *testing.T) {
	root := writeFiles(t, map[string]string{"broken.tm.hcl": "globals {"})
	if _, err := Load(root); err == nil {
		t.Error("a parse error must be returned")
	}
}

func TestDetectRoot(t *testing.T) {
	t.Run("required_version", func(t *testing.T) {
		root := writeFiles(t, map[string]string{
			".git/HEAD":             "",
			"infra/terramate.tm":    `terramate { required_version = ">= 0.9" }`,
			"infra/stacks/a/a.tm":   "stack {}",
			"infra/stacks/a/b.json": "{}",
		})
		want := filepath.Join(root, "infra")
		for _, start := range []string{"infra", "infra/stacks/a", "infra/stacks/a/a.tm"} {
			got, err := DetectRoot(filepath.Join(root, start))
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("DetectRoot(%s) = %s, want %s", start, got, want)
			}
		}
	})

	t.Run("git root fallback", func(t *testing.T) {
		root := writeFiles(t, map[string]string{
			".git/HEAD":         "",
			"stacks/a/stack.tm": "stack {}",
		})
		got, err := DetectRoot(filepath.Join(root, "stacks", "a"))
		if err != nil {
			t.Fatal(err)
		}
		if got != root {
			t.Errorf("got %s, want %s", got, root)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		if _, err := DetectRoot(filepath.Join(t.TempDir(), "nope")); err == nil {
			t.Error("a missing path must be an error")
		}
	})
}

func TestScopes(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"stacks/a/stack.tm.hcl": "stack {}\nimport {\n  source = \"/imports/x.tm.hcl\"\n}\n",
		"stacks/b/stack.tm.hcl": "stack {}",
		"imports/x.tm.hcl":      "globals {}",
		"imports/y.tm.hcl":      "globals {}",
	})
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := p.ResolveScopes([]string{filepath.Join(root, "stacks", "a")})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scopes, []Scope{{Path: "/stacks/a", IsDir: true}}) {
		t.Errorf("scopes = %+v", scopes)
	}

	in := func(rel string) bool { return p.InScope(filepath.Join(root, filepath.FromSlash(rel)), scopes) }
	if !in("stacks/a/stack.tm.hcl") {
		t.Error("a file inside the scope must be in scope")
	}
	if !in("imports/x.tm.hcl") {
		t.Error("a file imported into the scope must be in scope")
	}
	if in("imports/y.tm.hcl") || in("stacks/b/stack.tm.hcl") {
		t.Error("other files must not be in scope")
	}

	if _, err := p.ResolveScopes([]string{filepath.Dir(root)}); err == nil {
		t.Error("a path outside the project root must be an error")
	}
}
