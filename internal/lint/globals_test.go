package lint

import (
	"strings"
	"testing"
)

func TestShadowedGlobal(t *testing.T) {
	p := writeProject(t, map[string]string{
		"globals.tm.hcl": `
globals {
  shadowed_root = "root"
}
`,
		"stacks/a/stack.tm.hcl": `
stack {}

globals {
  shadowed_root = "a"
}

generate_hcl "main.tf" {
  content {
    sr = global.shadowed_root
  }
}
`,
	})
	got := messages(t, p, nil, "shadowed-global")
	want := []string{"globals.tm.hcl:3: global.shadowed_root is always overridden before it is read, this definition is never used"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestShadowedGlobalOneBranchUsesRoot checks that a root definition is not
// flagged as long as at least one stack reads the root value directly,
// without going through an override.
func TestShadowedGlobalOneBranchUsesRoot(t *testing.T) {
	p := writeProject(t, map[string]string{
		"globals.tm.hcl": `
globals {
  v = "root"
}
`,
		"stacks/a/stack.tm.hcl": `
stack {}

globals {
  v = "a"
}

generate_hcl "main.tf" {
  content {
    x = global.v
  }
}
`,
		"stacks/b/stack.tm.hcl": `
stack {}

generate_hcl "main.tf" {
  content {
    x = global.v
  }
}
`,
	})
	got := messages(t, p, nil, "shadowed-global")
	if len(got) != 0 {
		t.Errorf("got %v, want no findings: a root read directly by stacks/b must count as used", got)
	}
}

// TestShadowedGlobalNeverRead checks that a global overridden everywhere but
// never read anywhere is left to unused-global, not double-reported.
func TestShadowedGlobalNeverRead(t *testing.T) {
	p := writeProject(t, map[string]string{
		"globals.tm.hcl": `
globals {
  v = "root"
}
`,
		"stacks/a/stack.tm.hcl": `
stack {}

globals {
  v = "a"
}
`,
	})
	if got := messages(t, p, nil, "shadowed-global"); len(got) != 0 {
		t.Errorf("got %v, want no shadowed-global findings", got)
	}
	got := messages(t, p, nil, "unused-global")
	want := []string{
		`globals.tm.hcl:3: global.v is never used`,
		`stacks/a/stack.tm.hcl:5: global.v is never used`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestShadowedGlobalReverseUse checks that a global read from an ancestor of
// its own definition (inherited back down into a descendant stack) is
// conservatively treated as used, without a shadow check in that direction.
func TestShadowedGlobalReverseUse(t *testing.T) {
	p := writeProject(t, map[string]string{
		"globals.tm.hcl": `
generate_hcl "root.tf" {
  content {
    v = global.deep
  }
}
`,
		"stacks/a/stack.tm.hcl": `
stack {}

globals {
  deep = "a"
}
`,
	})
	if got := messages(t, p, nil, "shadowed-global"); len(got) != 0 {
		t.Errorf("got %v, want no findings for a reverse (ancestor-reads-descendant) use", got)
	}
}

// TestShadowedGlobalSelfReference checks that an override reading the parent
// value (region = "${global.region}-c") counts as a read of that parent, so
// the parent is not reported as shadowed.
func TestShadowedGlobalSelfReference(t *testing.T) {
	p := writeProject(t, map[string]string{
		"globals.tm.hcl": `
globals {
  region = "eu-west-1"
}
`,
		"stacks/a/stack.tm.hcl": `
stack {}

globals {
  region = "${global.region}-c"
}

generate_hcl "main.tf" {
  content {
    r = global.region
  }
}
`,
	})
	if got := messages(t, p, nil, "shadowed-global"); len(got) != 0 {
		t.Errorf("got %v, want no findings: the override reads the root value", got)
	}
}

// TestShadowedGlobalSelfReferenceSkipsLevel checks that a self-referencing
// override reads the nearest definition above it, so a root default hidden
// behind an intermediate override is still reported.
func TestShadowedGlobalSelfReferenceSkipsLevel(t *testing.T) {
	p := writeProject(t, map[string]string{
		"globals.tm.hcl": `
globals {
  region = "eu-west-1"
}
`,
		"stacks/globals.tm.hcl": `
globals {
  region = "us-east-1"
}
`,
		"stacks/a/stack.tm.hcl": `
stack {}

globals {
  region = "${global.region}-c"
}

generate_hcl "main.tf" {
  content {
    r = global.region
  }
}
`,
	})
	got := messages(t, p, nil, "shadowed-global")
	want := []string{"globals.tm.hcl:3: global.region is always overridden before it is read, this definition is never used"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", got, want)
	}
}
