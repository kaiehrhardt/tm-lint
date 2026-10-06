package lint

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/terramate-io/hcl/v2"
	"github.com/terramate-io/hcl/v2/hclsyntax"

	"github.com/kaiehrhardt/tm-lint/internal/hclutil"
	"github.com/kaiehrhardt/tm-lint/internal/project"
)

// stackRefAttrs are the stack attributes Terramate resolves with the same
// logic (run.BuildDAG): entries are directory paths (absolute = project path,
// otherwise relative to the stack) or "tag:<filter>". Invalid paths only cause
// a warning at run time, and paths or filters matching no stack are silently
// ignored, which is what this rule catches.
var stackRefAttrs = []string{"after", "before", "wants", "wanted_by"}

func ruleInvalidStackRef(c *checker) []Finding {
	var out []Finding
	for _, st := range c.p.Stacks {
		for _, attrName := range stackRefAttrs {
			attr, ok := st.Block.Body.Attributes[attrName]
			if !ok {
				continue
			}
			entries, ok := hclutil.LiteralStrings(attr.Expr)
			if !ok {
				continue // not a literal list; Terramate itself validates the type
			}
			for i, entry := range entries {
				if msg := checkStackRef(c.p, st, entry); msg != "" {
					out = append(out, Finding{
						Rule:  "invalid-stack-ref",
						File:  st.File,
						Range: elementRange(attr, i),
						Msg:   fmt.Sprintf("stack.%s entry %q %s", attrName, entry, msg),
					})
				}
			}
		}
	}
	return out
}

// checkStackRef returns a problem description, or "" if the entry is fine.
func checkStackRef(p *project.Project, st *project.Stack, entry string) string {
	if filter, ok := strings.CutPrefix(entry, "tag:"); ok {
		return checkTagFilter(p.Stacks, filter)
	}

	target := entry
	if !path.IsAbs(target) {
		target = path.Join(st.Dir, target)
	}
	target = path.Clean(target)

	fi, err := os.Stat(filepath.Join(p.Root, filepath.FromSlash(target)))
	if err != nil {
		return "does not exist (Terramate only warns at run time and ignores it)"
	}
	if !fi.IsDir() {
		return "is not a directory (Terramate only warns at run time and ignores it)"
	}
	if target == st.Dir {
		return "references the stack itself"
	}
	for _, other := range p.Stacks {
		if project.IsAncestorDir(target, other.Dir) {
			return ""
		}
	}
	return "contains no stacks (silently ignored by Terramate)"
}

// checkTagFilter implements Terramate's order-entry filter syntax:
// "," is OR, ":" is AND (binds tighter), "~" negates a tag.
func checkTagFilter(stacks []*project.Stack, filter string) string {
	known := map[string]bool{}
	for _, st := range stacks {
		for _, t := range st.Tags {
			known[t] = true
		}
	}

	var unknown []string
	for _, orClause := range strings.Split(filter, ",") {
		for _, tag := range strings.Split(orClause, ":") {
			tag = strings.TrimPrefix(tag, "~")
			if tag == "" {
				return "has an empty tag in its filter"
			}
			if !known[tag] {
				unknown = append(unknown, tag)
			}
		}
	}
	if len(unknown) > 0 {
		return fmt.Sprintf("uses tag(s) no stack has: %s", strings.Join(unknown, ", "))
	}

	for _, st := range stacks {
		if matchTagFilter(filter, st.Tags) {
			return ""
		}
	}
	return "matches no stack (silently ignored by Terramate)"
}

func matchTagFilter(filter string, tags []string) bool {
	has := map[string]bool{}
	for _, t := range tags {
		has[t] = true
	}
	for _, orClause := range strings.Split(filter, ",") {
		if orClause == "" {
			continue
		}
		all := true
		for _, tag := range strings.Split(orClause, ":") {
			if neg, ok := strings.CutPrefix(tag, "~"); ok {
				if has[neg] {
					all = false
				}
			} else if !has[tag] {
				all = false
			}
		}
		if all {
			return true
		}
	}
	return false
}

func elementRange(attr *hclsyntax.Attribute, i int) hcl.Range {
	if tup, ok := attr.Expr.(*hclsyntax.TupleConsExpr); ok && i < len(tup.Exprs) {
		return tup.Exprs[i].Range()
	}
	return attr.SrcRange
}
