package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/example.expected")

// TestExample lints testdata/example, which contains a case for every rule,
// and compares the output with testdata/example.expected.
// Run `go test -update` after intentional changes.
func TestExample(t *testing.T) {
	golden, err := filepath.Abs("testdata/example.expected")
	if err != nil {
		t.Fatal(err)
	}

	t.Chdir("testdata/example")
	got, err := runCLI(t, nil, "--ignore-globals", "global.net.optional_*")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want errFindings, got %v\n%s", err, got)
	}

	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestScope lints only stacks/a: its own findings are reported, findings in
// files imported into it (imports/common.tm.hcl) too, everything else not.
// Globals and imports still resolve against the whole project.
func TestScope(t *testing.T) {
	t.Chdir("testdata/example")
	got, err := runCLI(t, nil, "--ignore-file", os.DevNull, "stacks/a")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want errFindings, got %v\n%s", err, got)
	}

	for _, want := range []string{
		"stacks/a/stack.tm.hcl:3:33: [invalid-stack-ref]",
		"stacks/a/stack.tm.hcl:17:40: [undefined-global] global.sibling_only is not defined",
		"imports/common.tm.hcl:3:3: [unused-global] global.import_unused is never used",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if !strings.HasPrefix(line, "stacks/a/") && !strings.HasPrefix(line, "imports/common.tm.hcl:") {
			t.Errorf("finding outside scope: %s", line)
		}
	}
	// region is defined in the root globals.tm.hcl, outside the scope; the
	// stack must still see it.
	if strings.Contains(got, "global.region is not defined") {
		t.Errorf("globals from parent directories must resolve:\n%s", got)
	}
}

// TestRunFromSubfolder runs tm-lint without arguments inside a stack: the
// root is detected upwards and paths are printed relative to the stack.
func TestRunFromSubfolder(t *testing.T) {
	t.Chdir("testdata/example/stacks/d")
	got, err := runCLI(t, nil, "--enable", "unused-let")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want errFindings, got %v\n%s", err, got)
	}
	want := "stack.tm.hcl:6:5: [unused-let] let.unused is never used in generate_hcl \"d.tf\"\n" +
		"stack.tm.hcl:8:5: [unused-let] let.m is never used in generate_hcl \"d.tf\"\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
