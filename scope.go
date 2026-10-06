package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/terramate-io/hcl/v2"
	"github.com/terramate-io/hcl/v2/hclsyntax"
)

// scope limits which findings are reported. The whole project is always
// loaded, so imports, globals from parent directories and stack references
// resolve exactly as when linting everything.
type scope struct {
	path  string // project path, e.g. /stacks/a or /stacks/a/stack.tm.hcl
	isDir bool
}

// detectRoot finds the Terramate project root for start the same way
// Terramate does: the nearest directory at or above start whose Terramate
// files contain `terramate { required_version = ... }`. Without one, the
// nearest git repository root is used, and failing that start itself.
func detectRoot(start string) (string, error) {
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
		if e.IsDir() || !isTerramateFile(e.Name()) {
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

// resolveScopes turns the given paths into scopes inside the project root.
func resolveScopes(root string, paths []string) ([]scope, error) {
	var out []scope
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%s is outside the project root %s", p, root)
		}
		pp := "/"
		if rel != "." {
			pp = "/" + filepath.ToSlash(rel)
		}
		out = append(out, scope{path: pp, isDir: fi.IsDir()})
	}
	return out, nil
}

// inScope reports whether a finding belongs to one of the scopes: its file is
// inside a scope, or the file is imported into a scope directory (an imported
// file's content is evaluated in the importing directory).
func (p *project) inScope(f finding, scopes []scope) bool {
	file := "/" + p.relFile(f.file)
	for _, s := range scopes {
		if s.path == "/" || file == s.path || (s.isDir && strings.HasPrefix(file, s.path+"/")) {
			return true
		}
		if !s.isDir || len(p.imports[f.file]) == 0 {
			continue
		}
		for _, ctx := range p.contexts(f.file) {
			if isAncestorDir(s.path, ctx) {
				return true
			}
		}
	}
	return false
}

func filterScope(p *project, findings []finding, scopes []scope) []finding {
	var out []finding
	for _, f := range findings {
		if p.inScope(f, scopes) {
			out = append(out, f)
		}
	}
	return out
}
