package lint

import (
	"fmt"

	"github.com/terramate-io/hcl/v2"
	"github.com/terramate-io/hcl/v2/hclsyntax"

	"github.com/kaiehrhardt/tm-lint/internal/hclutil"
)

// ruleUnusedLet checks every block that contains `lets` blocks (generate_hcl,
// generate_file, ...): lets are only visible inside that block, so each let
// must be referenced somewhere in it.
func ruleUnusedLet(c *checker) []Finding {
	var out []Finding
	for _, file := range c.p.Files {
		for _, blk := range c.p.Bodies[file].Blocks {
			out = append(out, unusedLetsIn(file, blk)...)
		}
	}
	return out
}

type letDef struct {
	name  string
	rng   hcl.Range
	owner hcl.Range // range of the attribute or map block defining it
}

func unusedLetsIn(file string, blk *hclsyntax.Block) []Finding {
	var out []Finding

	var defs []letDef
	for _, sub := range blk.Body.Blocks {
		if sub.Type != "lets" {
			continue
		}
		for _, attr := range hclutil.SortedAttrs(sub.Body) {
			defs = append(defs, letDef{name: attr.Name, rng: attr.NameRange, owner: attr.SrcRange})
		}
		for _, m := range sub.Body.Blocks {
			if m.Type == "map" && len(m.Labels) > 0 {
				defs = append(defs, letDef{name: m.Labels[0], rng: m.DefRange(), owner: m.Range()})
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
			path, _ := hclutil.TraversalPath(st.Traversal)
			if len(path) == 0 {
				usesAll = true // `let` as a whole or dynamic access
				return nil
			}
			for _, d := range defs {
				if d.name == path[0] && !hclutil.Contains(d.owner, st.SrcRange) {
					used[d.name] = true
				}
			}
			return nil
		})
		if !usesAll {
			for _, d := range defs {
				if !used[d.name] {
					out = append(out, Finding{
						Rule:  "unused-let",
						File:  file,
						Range: d.rng,
						Msg:   fmt.Sprintf("let.%s is never used in %s", d.name, blockName(blk)),
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

func blockName(b *hclsyntax.Block) string {
	s := b.Type
	for _, l := range b.Labels {
		s += fmt.Sprintf(" %q", l)
	}
	return s
}
