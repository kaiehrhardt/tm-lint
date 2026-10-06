package hclutil

import (
	"reflect"
	"testing"

	"github.com/terramate-io/hcl/v2"
	"github.com/terramate-io/hcl/v2/hclsyntax"
)

func parseExpr(t *testing.T, src string) hclsyntax.Expression {
	t.Helper()
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatal(diags)
	}
	return expr
}

func TestIsTerramateFile(t *testing.T) {
	for name, want := range map[string]bool{
		"stack.tm.hcl":   true,
		"stack.tm":       true,
		".hidden.tm.hcl": false,
		"main.tf":        false,
		"stack.hcl":      false,
	} {
		if got := IsTerramateFile(name); got != want {
			t.Errorf("IsTerramateFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestLiteralStrings(t *testing.T) {
	got, ok := LiteralStrings(parseExpr(t, `["a", "b"]`))
	if !ok || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("got %v, %v", got, ok)
	}
	for _, src := range []string{`"a"`, `[1]`, `[global.x]`, `null`} {
		if _, ok := LiteralStrings(parseExpr(t, src)); ok {
			t.Errorf("%s must not be a literal string list", src)
		}
	}
}

func TestTraversalPath(t *testing.T) {
	cases := []struct {
		src      string
		want     []string
		complete bool
	}{
		{`global.a.b`, []string{"a", "b"}, true},
		{`global["a"].b`, []string{"a", "b"}, true},
		{`global.a[0].b`, []string{"a"}, false},
		{`global`, nil, true},
	}
	for _, c := range cases {
		st, ok := parseExpr(t, c.src).(*hclsyntax.ScopeTraversalExpr)
		if !ok {
			t.Fatalf("%s is not a scope traversal", c.src)
		}
		got, complete := TraversalPath(st.Traversal)
		if !reflect.DeepEqual(got, c.want) || complete != c.complete {
			t.Errorf("TraversalPath(%s) = %v, %v; want %v, %v", c.src, got, complete, c.want, c.complete)
		}
	}
}

func TestPathHelpers(t *testing.T) {
	if !IsPrefix([]string{"a"}, []string{"a", "b"}) || IsPrefix([]string{"a", "b"}, []string{"a"}) || !IsPrefix(nil, []string{"a"}) {
		t.Error("IsPrefix is wrong")
	}
	base := make([]string, 1, 4)
	base[0] = "a"
	x := AppendPath(base, "x")
	y := AppendPath(base, "y")
	if x[1] != "x" || y[1] != "y" {
		t.Errorf("AppendPath must not share the backing array: %v %v", x, y)
	}
}
