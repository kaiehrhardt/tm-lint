package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/terramate-io/hcl/v2"
	"github.com/terramate-io/hcl/v2/hclsyntax"

	"github.com/kaiehrhardt/tm-lint/internal/hclutil"
)

// Scope is a part of the project whose findings are reported. The whole
// project is always loaded, so imports, globals from parent directories and
// stack references resolve exactly as when linting everything.
type Scope struct {
	Path  string // project path, e.g. /stacks/a or /stacks/a/stack.tm.hcl
	IsDir bool
}

// DetectRoot finds the Terramate project root for start the same way
// Terramate does: the nearest directory at or above start whose Terramate
// files contain `terramate { required_version = ... }`. Without one, the
// nearest git repository root is used, and failing that start itself.
func DetectRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(abs); err != nil {
		return "", err
	} else if !fi.IsDir() {
		abs = filepath.Dir(abs)
	}

	gitRoot := ""
	for dir := abs; ; {
		if hasRootConfig(dir) {
			return dir, nil
		}
		if gitRoot == "" {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				gitRoot = dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if gitRoot != "" {
		return gitRoot, nil
	}
	return abs, nil
}

// hasRootConfig reports whether any Terramate file directly in dir has a
// top-level terramate block with required_version.
func hasRootConfig(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !hclutil.IsTerramateFile(e.Name()) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		f, diags := hclsyntax.ParseConfig(src, e.Name(), hcl.InitialPos)
		if diags.HasErrors() {
			continue
		}
		for _, blk := range f.Body.(*hclsyntax.Body).Blocks {
			if blk.Type != "terramate" {
				continue
			}
			if _, ok := blk.Body.Attributes["required_version"]; ok {
				return true
			}
		}
	}
	return false
}

// ResolveScopes turns host paths (relative to the working directory) into
// scopes inside the project root.
func (p *Project) ResolveScopes(paths []string) ([]Scope, error) {
	var out []Scope
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(p.Root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%s is outside the project root %s", path, p.Root)
		}
		pp := "/"
		if rel != "." {
			pp = "/" + filepath.ToSlash(rel)
		}
		out = append(out, Scope{Path: pp, IsDir: fi.IsDir()})
	}
	return out, nil
}

// InScope reports whether file belongs to one of the scopes: it is inside a
// scope, or it is imported into a scope directory (an imported file's content
// is evaluated in the importing directory).
func (p *Project) InScope(file string, scopes []Scope) bool {
	pp := "/" + p.RelFile(file)
	for _, s := range scopes {
		if s.Path == "/" || pp == s.Path || (s.IsDir && strings.HasPrefix(pp, s.Path+"/")) {
			return true
		}
		if !s.IsDir || !p.IsImported(file) {
			continue
		}
		for _, ctx := range p.Contexts(file) {
			if IsAncestorDir(s.Path, ctx) {
				return true
			}
		}
	}
	return false
}
