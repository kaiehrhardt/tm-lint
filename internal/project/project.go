// Package project loads a Terramate project from disk: every configuration
// file, the import relationships between them and the stacks.
package project

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

	"github.com/kaiehrhardt/tm-lint/internal/hclutil"
)

// Project holds every parsed Terramate file of a project.
type Project struct {
	// Root is the absolute host path of the project root.
	Root string
	// Files are the absolute paths of all parsed files, sorted.
	Files []string
	// Bodies maps an absolute file path to its parsed body.
	Bodies map[string]*hclsyntax.Body
	// Sources maps an absolute file path to its content.
	Sources map[string][]byte
	// Stacks are all stacks of the project, in file order.
	Stacks []*Stack

	imports  map[string][]string // imported file -> importing files
	ctxCache map[string][]string
}

// Stack is a directory with a stack block.
type Stack struct {
	Dir   string // project path, e.g. /stacks/a
	File  string // absolute path of the file containing the stack block
	Block *hclsyntax.Block
	Tags  []string
}

// Load parses every Terramate file below root plus everything reachable
// through import blocks. Hidden directories and directories containing a
// .tmskip file are skipped, like Terramate does.
func Load(root string) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	p := &Project{
		Root:     abs,
		Bodies:   map[string]*hclsyntax.Body{},
		Sources:  map[string][]byte{},
		imports:  map[string][]string{},
		ctxCache: map[string][]string{},
	}
	if err := p.load(); err != nil {
		return nil, err
	}
	for f := range p.Bodies {
		p.Files = append(p.Files, f)
	}
	sort.Strings(p.Files)
	p.collectStacks()
	return p, nil
}

func (p *Project) load() error {
	var queue []string
	err := filepath.WalkDir(p.Root, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if fpath != p.Root && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(fpath, ".tmskip")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if hclutil.IsTerramateFile(name) {
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
		if _, done := p.Bodies[file]; done {
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
		p.Bodies[file] = body
		p.Sources[file] = src

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
func (p *Project) resolveImports(file string, body *hclsyntax.Body) ([]string, error) {
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
			srcDir = filepath.Join(p.Root, srcDir)
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

func (p *Project) collectStacks() {
	for _, file := range p.Files {
		if p.IsImported(file) && !p.inProjectTree(file) {
			continue // imported from a skipped dir: not a stack on its own
		}
		for _, blk := range p.Bodies[file].Blocks {
			if blk.Type != "stack" {
				continue
			}
			st := &Stack{Dir: p.ProjectPath(filepath.Dir(file)), File: file, Block: blk}
			if attr, ok := blk.Body.Attributes["tags"]; ok {
				st.Tags, _ = hclutil.LiteralStrings(attr.Expr)
			}
			p.Stacks = append(p.Stacks, st)
		}
	}
}

// inProjectTree reports whether file was found by the directory walk, as
// opposed to only being reachable through an import from a skipped dir.
func (p *Project) inProjectTree(file string) bool {
	rel, err := filepath.Rel(p.Root, file)
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

// IsImported reports whether file is imported by another file.
func (p *Project) IsImported(file string) bool {
	return len(p.imports[file]) > 0
}

// Contexts returns the project directories in which the content of file is
// evaluated: its own directory plus, transitively, every directory importing
// it.
func (p *Project) Contexts(file string) []string {
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
		dirs[p.ProjectPath(filepath.Dir(f))] = true
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

// ProjectPath converts an absolute host directory into a project path such as
// "/" or "/stacks/a".
func (p *Project) ProjectPath(dir string) string {
	rel, err := filepath.Rel(p.Root, dir)
	if err != nil || rel == "." {
		return "/"
	}
	return "/" + filepath.ToSlash(rel)
}

// RelFile returns file relative to the project root, with forward slashes.
func (p *Project) RelFile(file string) string {
	rel, err := filepath.Rel(p.Root, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(rel)
}

// DirsRelated reports whether any directory of a and any directory of b lie
// on the same branch of the tree (equal, ancestor or descendant).
func DirsRelated(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if IsAncestorDir(x, y) || IsAncestorDir(y, x) {
				return true
			}
		}
	}
	return false
}

// IsAncestorDir reports whether project path anc is dir or one of its
// ancestors.
func IsAncestorDir(anc, dir string) bool {
	return anc == "/" || anc == dir || strings.HasPrefix(dir, anc+"/")
}
