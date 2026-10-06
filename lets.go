package main

import (
	"fmt"

	"github.com/terramate-io/hcl/v2"
	"github.com/terramate-io/hcl/v2/hclsyntax"
)

// ruleUnusedLet checks every block that contains `lets` blocks (generate_hcl,
// generate_file, ...): lets are only visible inside that block, so each let
// must be referenced somewhere in it.
func ruleUnusedLet(p *project, _ *options) []finding {
	var out []finding
	for _, file := range p.files {
		for _, blk := range p.bodies[file].Blocks {
			out = append(out, unusedLetsIn(file, blk)...)
		}
	}
	return out
}

type letDef struct {
	name  string
	rng   hcl.Range
	owner hclsyntax.Node // the attribute or map block defining it
}

func unusedLetsIn(file string, blk *hclsyntax.Block) []finding {
	var out []finding

	var defs []letDef
	for _, sub := range blk.Body.Blocks {
		if sub.Type != "lets" {
			continue
		}
		for _, attr := range sortedAttrs(sub.Body) {
			defs = append(defs, letDef{name: attr.Name, rng: attr.NameRange, owner: attr})
		}
		for _, m := range sub.Body.Blocks {
			if m.Type == "map" && len(m.Labels) > 0 {
				defs = append(defs, letDef{name: m.Labels[0], rng: m.DefRange(), owner: m})
			}
		}
	}

	if len(defs) > 0 {
		used := map[string]bool{}
		usesAll := false
		_ = hclsyntax.VisitAll(blk.Body, func(n hclsyntax.Node) hcl.Diagnostics {
			st, ok := n.(*hclsyntax.ScopeTraversalExpr)
			if !ok || st.Traversal.RootName() != "let" {
				return nil
			}
			gpath, _ := traversalPath(st.Traversal)
			if len(gpath) == 0 {
				usesAll = true // `let` as a whole or dynamic access
				return nil
			}
			for _, d := range defs {
				if d.name == gpath[0] && !nodeContains(d.owner, st.SrcRange) {
					used[d.name] = true
				}
			}
			return nil
		})
		if !usesAll {
			for _, d := range defs {
				if !used[d.name] {
					out = append(out, finding{
						rule: "unused-let",
						file: file,
						rng:  d.rng,
						msg:  fmt.Sprintf("let.%s is never used in %s", d.name, blockName(blk)),
					})
				}
			}
		}
	}

	for _, sub := range blk.Body.Blocks {
		if sub.Type != "lets" {
			out = append(out, unusedLetsIn(file, sub)...)
		}
	}
	return out
}

func nodeContains(n hclsyntax.Node, r hcl.Range) bool {
	var outer hcl.Range
	switch v := n.(type) {
	case *hclsyntax.Attribute:
		outer = v.SrcRange
	case *hclsyntax.Block:
		outer = v.Range()
	default:
		return false
	}
	return r.Start.Byte >= outer.Start.Byte && r.End.Byte <= outer.End.Byte
}

func blockName(b *hclsyntax.Block) string {
	s := b.Type
	for _, l := range b.Labels {
		s += fmt.Sprintf(" %q", l)
	}
	return s
}
