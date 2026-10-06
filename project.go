package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/terramate-io/hcl/v2"
	"github.com/terramate-io/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// project holds every parsed Terramate file of a project and the facts the
// rules share: import relationships and the list of stacks.
type project struct {
	root    string                     // absolute host path of the project root
	files   []string                   // sorted absolute paths of all parsed files
	bodies  map[string]*hclsyntax.Body // abs file -> parsed body
	sources map[string][]byte
	imports map[string][]string // imported abs file -> importing abs files

	stacks []*stackInfo

	ctxCache map[string][]string
}

type stackInfo struct {
	dir   string // project path, e.g. /stacks/a
	file  string // file containing the stack block
	block *hclsyntax.Block
	tags  []string
}

func loadProject(root string) (*project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	p := &project{
		root:     abs,
		bodies:   map[string]*hclsyntax.Body{},
		sources:  map[string][]byte{},
		imports:  map[string][]string{},
		ctxCache: map[string][]string{},
	}
	if err := p.load(); err != nil {
		return nil, err
	}
	for f := range p.bodies {
		p.files = append(p.files, f)
	}
	sort.Strings(p.files)
	p.collectStacks()
	return p, nil
}

// load parses every Terramate file of the project plus everything reachable
// through import blocks (imported files may live in skipped directories).
func (p *project) load() error {
	var queue []string
	err := filepath.WalkDir(p.root, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if fpath != p.root && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(fpath, ".tmskip")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if isTerramateFile(name) {
			queue = append(queue, fpath)
		}
		return nil
	})
	if err != nil {
		return err
	}

	for len(queue) > 0 {
		file := queue[0]
		queue = queue[1:]
		if _, done := p.bodies[file]; done {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		f, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
		if diags.HasErrors() {
			return diags
		}
		body := f.Body.(*hclsyntax.Body)
		p.bodies[file] = body
		p.sources[file] = src

		imported, err := p.resolveImports(file, body)
		if err != nil {
			return err
		}
		for _, imp := range imported {
			p.imports[imp] = append(p.imports[imp], file)
			queue = append(queue, imp)
		}
	}
	return nil
}

// resolveImports mirrors Terramate's import semantics: source is relative to
// the importing file's directory, or to the project root when absolute, and
// the base name may be a glob.
func (p *project) resolveImports(file string, body *hclsyntax.Body) ([]string, error) {
	var out []string
	for _, blk := range body.Blocks {
		if blk.Type != "import" {
			continue
		}
		attr, ok := blk.Body.Attributes["source"]
		if !ok {
			continue
		}
		val, diags := attr.Expr.Value(nil)
		if diags.HasErrors() || val.Type() != cty.String {
			return nil, fmt.Errorf("%s: import.source must be a literal string", attr.SrcRange)
		}
		src := val.AsString()
		srcDir := path.Dir(src)
		if path.IsAbs(srcDir) {
			srcDir = filepath.Join(p.root, srcDir)
		} else {
			srcDir = filepath.Join(filepath.Dir(file), srcDir)
		}
		matches, err := filepath.Glob(filepath.Join(srcDir, path.Base(src)))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", attr.SrcRange, err)
		}
		out = append(out, matches...)
	}
	return out, nil
}

func (p *project) collectStacks() {
	for _, file := range p.files {
		if len(p.imports[file]) > 0 && !p.inProjectTree(file) {
			continue // imported from a skipped dir: not a stack on its own
		}
		for _, blk := range p.bodies[file].Blocks {
			if blk.Type != "stack" {
				continue
			}
			st := &stackInfo{dir: p.projectPath(filepath.Dir(file)), file: file, block: blk}
			if attr, ok := blk.Body.Attributes["tags"]; ok {
				st.tags, _ = literalStrings(attr.Expr)
			}
			p.stacks = append(p.stacks, st)
		}
	}
}

// inProjectTree reports whether file was found by the directory walk (as
// opposed to only being reachable through an import from a skipped dir).
func (p *project) inProjectTree(file string) bool {
	rel, err := filepath.Rel(p.root, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
		if strings.HasPrefix(part, ".") && part != "." {
			return false
		}
	}
	return true
}

// contexts returns the project directories in which the content of file is
// evaluated: its own directory plus (transitively) every directory importing it.
func (p *project) contexts(file string) []string {
	if c, ok := p.ctxCache[file]; ok {
		return c
	}
	seen := map[string]bool{}
	dirs := map[string]bool{}
	var visit func(f string)
	visit = func(f string) {
		if seen[f] {
			return
		}
		seen[f] = true
		dirs[p.projectPath(filepath.Dir(f))] = true
		for _, importer := range p.imports[f] {
			visit(importer)
		}
	}
	visit(file)

	out := make([]string, 0, len(dirs))
	for d := range dirs {
		out = append(out, d)
	}
	sort.Strings(out)
	p.ctxCache[file] = out
	return out
}

func (p *project) projectPath(dir string) string {
	rel, err := filepath.Rel(p.root, dir)
	if err != nil || rel == "." {
		return "/"
	}
	return "/" + filepath.ToSlash(rel)
}

func (p *project) relFile(file string) string {
	rel, err := filepath.Rel(p.root, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(rel)
}

// dirsRelated reports whether any context of a and any context of b lie on the
// same branch of the directory tree (equal, ancestor or descendant).
func dirsRelated(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if isAncestorDir(x, y) || isAncestorDir(y, x) {
				return true
			}
		}
	}
	return false
}

func isAncestorDir(anc, dir string) bool {
	return anc == "/" || anc == dir || strings.HasPrefix(dir, anc+"/")
}

func isPrefix(prefix, full []string) bool {
	if len(prefix) > len(full) {
		return false
	}
	for i := range prefix {
		if prefix[i] != full[i] {
			return false
		}
	}
	return true
}

func isTerramateFile(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	return strings.HasSuffix(name, ".tm.hcl") || strings.HasSuffix(name, ".tm")
}

func sortedAttrs(body *hclsyntax.Body) []*hclsyntax.Attribute {
	attrs := make([]*hclsyntax.Attribute, 0, len(body.Attributes))
	for _, a := range body.Attributes {
		attrs = append(attrs, a)
	}
	sort.Slice(attrs, func(i, j int) bool {
		return attrs[i].SrcRange.Start.Byte < attrs[j].SrcRange.Start.Byte
	})
	return attrs
}

func appendPath(base []string, name string) []string {
	out := make([]string, 0, len(base)+1)
	out = append(out, base...)
	return append(out, name)
}

// literalStrings evaluates expr without any variables and returns it as a list
// of strings. ok is false if the expression is not a literal string list.
func literalStrings(expr hclsyntax.Expression) ([]string, bool) {
	val, diags := expr.Value(nil)
	if diags.HasErrors() || !val.IsWhollyKnown() || val.IsNull() {
		return nil, false
	}
	ty := val.Type()
	if !ty.IsListType() && !ty.IsSetType() && !ty.IsTupleType() {
		return nil, false
	}
	var out []string
	for it := val.ElementIterator(); it.Next(); {
		_, v := it.Element()
		if v.IsNull() || v.Type() != cty.String {
			return nil, false
		}
		out = append(out, v.AsString())
	}
	return out, true
}

// traversalPath turns the steps after the root of a traversal into a path of
// names, stopping at the first step that is not a literal attribute or string
// key. complete is false if it stopped early.
func traversalPath(t hcl.Traversal) (out []string, complete bool) {
	for _, step := range t[1:] {
		switch s := step.(type) {
		case hcl.TraverseAttr:
			out = append(out, s.Name)
		case hcl.TraverseIndex:
			if s.Key.Type() != cty.String || !s.Key.IsKnown() || s.Key.IsNull() {
				return out, false
			}
			out = append(out, s.Key.AsString())
		default:
			return out, false
		}
	}
	return out, true
}
