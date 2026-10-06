// Package hclutil contains small helpers for working with Terramate's HCL
// syntax tree.
package hclutil

import (
	"slices"
	"sort"
	"strings"

	"github.com/terramate-io/hcl/v2"
	"github.com/terramate-io/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// IsTerramateFile reports whether a file name is a Terramate configuration
// file (*.tm or *.tm.hcl, not hidden).
func IsTerramateFile(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	return strings.HasSuffix(name, ".tm.hcl") || strings.HasSuffix(name, ".tm")
}

// SortedAttrs returns the attributes of body in source order.
func SortedAttrs(body *hclsyntax.Body) []*hclsyntax.Attribute {
	attrs := make([]*hclsyntax.Attribute, 0, len(body.Attributes))
	for _, a := range body.Attributes {
		attrs = append(attrs, a)
	}
	sort.Slice(attrs, func(i, j int) bool {
		return attrs[i].SrcRange.Start.Byte < attrs[j].SrcRange.Start.Byte
	})
	return attrs
}

// LiteralStrings evaluates expr without any variables and returns it as a list
// of strings. ok is false if the expression is not a literal string list.
func LiteralStrings(expr hclsyntax.Expression) (out []string, ok bool) {
	val, diags := expr.Value(nil)
	if diags.HasErrors() || !val.IsWhollyKnown() || val.IsNull() {
		return nil, false
	}
	ty := val.Type()
	if !ty.IsListType() && !ty.IsSetType() && !ty.IsTupleType() {
		return nil, false
	}
	for it := val.ElementIterator(); it.Next(); {
		_, v := it.Element()
		if v.IsNull() || v.Type() != cty.String {
			return nil, false
		}
		out = append(out, v.AsString())
	}
	return out, true
}

// TraversalPath turns the steps after the root of a traversal into a path of
// names, stopping at the first step that is not a literal attribute or string
// key. complete is false if it stopped early.
func TraversalPath(t hcl.Traversal) (out []string, complete bool) {
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

// IsPrefix reports whether prefix is a prefix of path.
func IsPrefix(prefix, path []string) bool {
	return len(prefix) <= len(path) && slices.Equal(prefix, path[:len(prefix)])
}

// AppendPath returns a new slice with name appended to base, leaving base
// untouched.
func AppendPath(base []string, name string) []string {
	return append(slices.Clone(base), name)
}

// Contains reports whether inner lies completely within outer.
func Contains(outer, inner hcl.Range) bool {
	return inner.Start.Byte >= outer.Start.Byte && inner.End.Byte <= outer.End.Byte
}
