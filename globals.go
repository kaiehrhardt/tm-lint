package main

import (
	"fmt"
	"strings"

	"github.com/terramate-io/hcl/v2"
	"github.com/terramate-io/hcl/v2/hclsyntax"
)

type globalDef struct {
	path []string
	file string
	rng  hcl.Range
}

type globalUse struct {
	path    []string
	file    string
	rng     hcl.Range
	owner   int  // index of the definition whose expression contains this use, -1 if none
	guarded bool // inside a tm_try/tm_can argument that has a fallback
}

type globalIndex struct {
	defs []globalDef
	uses []globalUse
}

var globalIdxCache = map[*project]*globalIndex{}

func (p *project) globals() *globalIndex {
	if idx, ok := globalIdxCache[p]; ok {
		return idx
	}
	idx := &globalIndex{}
	for _, file := range p.files {
		body := p.bodies[file]
		for _, attr := range sortedAttrs(body) {
			idx.collectUses(file, attr.Expr, -1)
		}
		for _, blk := range body.Blocks {
			if blk.Type == "globals" {
				idx.collectGlobalsBlock(file, blk)
				continue
			}
			idx.collectBody(file, blk.Body, -1)
		}
	}
	globalIdxCache[p] = idx
	return idx
}

func (idx *globalIndex) collectGlobalsBlock(file string, blk *hclsyntax.Block) {
	labels := blk.Labels
	attrs := sortedAttrs(blk.Body)

	if len(labels) > 0 && len(attrs) == 0 && len(blk.Body.Blocks) == 0 {
		// `globals "a" "b" {}` creates an (empty) object at global.a.b
		idx.defs = append(idx.defs, globalDef{path: labels, file: file, rng: blk.DefRange()})
	}

	for _, attr := range attrs {
		id := len(idx.defs)
		idx.defs = append(idx.defs, globalDef{path: appendPath(labels, attr.Name), file: file, rng: attr.NameRange})
		idx.collectUses(file, attr.Expr, id)
	}

	for _, sub := range blk.Body.Blocks {
		if sub.Type != "map" || len(sub.Labels) == 0 {
			idx.collectBody(file, sub.Body, -1)
			continue
		}
		id := len(idx.defs)
		idx.defs = append(idx.defs, globalDef{path: appendPath(labels, sub.Labels[0]), file: file, rng: sub.DefRange()})
		idx.collectBody(file, sub.Body, id)
	}
}

func (idx *globalIndex) collectBody(file string, body *hclsyntax.Body, owner int) {
	for _, attr := range sortedAttrs(body) {
		idx.collectUses(file, attr.Expr, owner)
	}
	for _, blk := range body.Blocks {
		idx.collectBody(file, blk.Body, owner)
	}
}

func (idx *globalIndex) collectUses(file string, expr hclsyntax.Expression, owner int) {
	guards := guardedRanges(expr)
	_ = hclsyntax.VisitAll(expr, func(n hclsyntax.Node) hcl.Diagnostics {
		st, ok := n.(*hclsyntax.ScopeTraversalExpr)
		if !ok || st.Traversal.RootName() != "global" {
			return nil
		}
		gpath, _ := traversalPath(st.Traversal)
		idx.uses = append(idx.uses, globalUse{
			path:    gpath,
			file:    file,
			rng:     st.SrcRange,
			owner:   owner,
			guarded: containedIn(st.SrcRange, guards),
		})
		return nil
	})
}

// guardedRanges returns the ranges of expressions whose evaluation errors are
// swallowed: every tm_try argument except the last (the fallback) and every
// tm_can argument.
func guardedRanges(expr hclsyntax.Expression) []hcl.Range {
	var out []hcl.Range
	_ = hclsyntax.VisitAll(expr, func(n hclsyntax.Node) hcl.Diagnostics {
		call, ok := n.(*hclsyntax.FunctionCallExpr)
		if !ok {
			return nil
		}
		switch call.Name {
		case "tm_try", "try":
			for i := 0; i < len(call.Args)-1; i++ {
				out = append(out, call.Args[i].Range())
			}
		case "tm_can", "can":
			for _, a := range call.Args {
				out = append(out, a.Range())
			}
		}
		return nil
	})
	return out
}

func containedIn(r hcl.Range, ranges []hcl.Range) bool {
	for _, g := range ranges {
		if r.Start.Byte >= g.Start.Byte && r.End.Byte <= g.End.Byte {
			return true
		}
	}
	return false
}

// related reports whether a definition and a use can see each other: their
// paths overlap (one is a prefix of the other) and their directories lie on
// the same branch of the tree. Globals are inherited downwards, and
// expressions in a parent directory are evaluated in the context of each
// stack below it, so both directions count.
func (p *project) globalRelated(d globalDef, u globalUse) bool {
	if !isPrefix(u.path, d.path) && !isPrefix(d.path, u.path) {
		return false
	}
	return dirsRelated(p.contexts(d.file), p.contexts(u.file))
}

func ruleUnusedGlobal(p *project, opts *options) []finding {
	idx := p.globals()
	var out []finding
	for id, d := range idx.defs {
		if matchesGlobalPattern(d.path, opts.ignoreGlobals) {
			continue
		}
		used := false
		for _, u := range idx.uses {
			if u.owner == id {
				continue // a global referencing itself (e.g. overriding a parent value)
			}
			if p.globalRelated(d, u) {
				used = true
				break
			}
		}
		if !used {
			out = append(out, finding{
				rule: "unused-global",
				file: d.file,
				rng:  d.rng,
				msg:  fmt.Sprintf("global.%s is never used", strings.Join(d.path, ".")),
			})
		}
	}
	return out
}

// ruleUndefinedGlobal reports references to globals that no visible
// definition provides.
//
// References inside tm_try/tm_can are the common "optional value with a
// default" idiom, e.g. tm_try(global.app.replicas, 1). They are only reported
// when nothing at all is defined under the root name (the fallback is always
// used, likely a typo or a leftover), or when the missing name looks like a
// typo of a defined sibling.
func ruleUndefinedGlobal(p *project, opts *options) []finding {
	idx := p.globals()
	var out []finding
	for _, u := range idx.uses {
		if len(u.path) == 0 || matchesGlobalPattern(u.path, opts.ignoreGlobals) {
			continue // `global` as a whole or fully dynamic access
		}

		// Visible definitions, excluding the one this use is part of:
		// `x = global.x` needs x from somewhere else.
		var visible []globalDef
		for id, d := range idx.defs {
			if id != u.owner && dirsRelated(p.contexts(d.file), p.contexts(u.file)) {
				visible = append(visible, d)
			}
		}

		defined := false
		for _, d := range visible {
			if isPrefix(u.path, d.path) || isPrefix(d.path, u.path) {
				defined = true
				break
			}
		}
		if defined {
			continue
		}

		// Deepest level of the path that is known, and the names defined there.
		level, siblings := knownLevel(u.path, visible)
		missing := "global." + strings.Join(u.path[:level+1], ".")
		suggestion := closestName(u.path[level], siblings)

		var msg string
		switch {
		case !u.guarded:
			msg = missing + " is not defined"
		case level == 0:
			msg = missing + " is not defined anywhere, the tm_try/tm_can fallback is always used"
		case suggestion != "":
			msg = missing + " is not defined (silently masked by tm_try/tm_can)"
		default:
			continue // optional field with a default
		}
		if suggestion != "" {
			msg += fmt.Sprintf(", did you mean global.%s?", strings.Join(appendPath(u.path[:level], suggestion), "."))
		}
		out = append(out, finding{rule: "undefined-global", file: u.file, rng: u.rng, msg: msg})
	}
	return out
}

// knownLevel returns the index of the first path segment that no visible
// definition provides, and the names that are defined at that level.
func knownLevel(gpath []string, visible []globalDef) (int, []string) {
	level := 0
	for level < len(gpath)-1 {
		found := false
		for _, d := range visible {
			if isPrefix(gpath[:level+1], d.path) {
				found = true
				break
			}
		}
		if !found {
			break
		}
		level++
	}
	seen := map[string]bool{}
	var names []string
	for _, d := range visible {
		if len(d.path) > level && isPrefix(gpath[:level], d.path) && !seen[d.path[level]] {
			seen[d.path[level]] = true
			names = append(names, d.path[level])
		}
	}
	return level, names
}

// closestName returns the candidate within a small edit distance of name, or "".
func closestName(name string, candidates []string) string {
	best, bestDist := "", 0
	maxDist := 2
	if len(name) <= 4 {
		maxDist = 1
	}
	for _, c := range candidates {
		if c == name {
			continue
		}
		d := levenshtein(name, c)
		if d <= maxDist && (best == "" || d < bestDist || (d == bestDist && c < best)) {
			best, bestDist = c, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
