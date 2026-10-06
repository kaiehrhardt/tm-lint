package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/terramate-io/hcl/v2"
	"github.com/terramate-io/hcl/v2/hclsyntax"
)

// stackRefAttrs are the stack attributes Terramate resolves with the same
// logic (run.BuildDAG): entries are directory paths (absolute = project path,
// otherwise relative to the stack) or "tag:<filter>". Invalid paths only cause
// a warning at run time, and paths or filters matching no stack are silently
// ignored, which is what this rule catches.
var stackRefAttrs = []string{"after", "before", "wants", "wanted_by"}

func ruleInvalidStackRef(p *project, _ *options) []finding {
	var out []finding
	for _, st := range p.stacks {
		for _, attrName := range stackRefAttrs {
			attr, ok := st.block.Body.Attributes[attrName]
			if !ok {
				continue
			}
			entries, ok := literalStrings(attr.Expr)
			if !ok {
				continue // not a literal list; Terramate itself validates the type
			}
			for i, entry := range entries {
				rng := elementRange(attr, i)
				if msg := p.checkStackRef(st, entry); msg != "" {
					out = append(out, finding{
						rule: "invalid-stack-ref",
						file: st.file,
						rng:  rng,
						msg:  fmt.Sprintf("stack.%s entry %q %s", attrName, entry, msg),
					})
				}
			}
		}
	}
	return out
}

// checkStackRef returns a problem description, or "" if the entry is fine.
func (p *project) checkStackRef(st *stackInfo, entry string) string {
	if strings.HasPrefix(entry, "tag:") {
		return p.checkTagFilter(strings.TrimPrefix(entry, "tag:"))
	}

	target := entry
	if !path.IsAbs(target) {
		target = path.Join(st.dir, target)
	}
	target = path.Clean(target)

	hostPath := filepath.Join(p.root, filepath.FromSlash(target))
	fi, err := os.Stat(hostPath)
	if err != nil {
		return "does not exist (Terramate only warns at run time and ignores it)"
	}
	if !fi.IsDir() {
		return "is not a directory (Terramate only warns at run time and ignores it)"
	}
	if target == st.dir {
		return "references the stack itself"
	}
	for _, other := range p.stacks {
		if isAncestorDir(target, other.dir) {
			return ""
		}
	}
	return "contains no stacks (silently ignored by Terramate)"
}

// checkTagFilter implements Terramate's order-entry filter syntax:
// "," is OR, ":" is AND (binds tighter), "~" negates a tag.
func (p *project) checkTagFilter(filter string) string {
	known := map[string]bool{}
	for _, st := range p.stacks {
		for _, t := range st.tags {
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

	for _, st := range p.stacks {
		if matchTagFilter(filter, st.tags) {
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
			if neg := strings.HasPrefix(tag, "~"); neg {
				if has[tag[1:]] {
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
