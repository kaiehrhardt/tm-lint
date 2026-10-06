package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

// runCLI executes the root command like main does and returns its output.
func runCLI(t *testing.T, env map[string]string, args ...string) (string, error) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// rulesIn returns the set of rules that appear in lint output.
func rulesIn(out string) map[string]bool {
	got := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if i := strings.Index(line, "["); i >= 0 {
			if j := strings.Index(line[i:], "]"); j > 0 {
				got[line[i+1:i+j]] = true
			}
		}
	}
	return got
}

func TestCLIFlagsAndEnv(t *testing.T) {
	const example = "testdata/example"

	cases := []struct {
		name     string
		env      map[string]string
		args     []string
		rules    []string // exactly these rules must appear
		contains []string
		excludes []string
	}{
		{
			name:  "enable flag",
			args:  []string{"--enable", "unused-let", example},
			rules: []string{"unused-let"},
		},
		{
			name:  "enable env",
			env:   map[string]string{"TM_LINT_ENABLE": "unused-let"},
			args:  []string{example},
			rules: []string{"unused-let"},
		},
		{
			name:  "repeated enable flag",
			args:  []string{"--enable", "unused-let", "--enable", "invalid-stack-ref", example},
			rules: []string{"unused-let", "invalid-stack-ref"},
		},
		{
			name:  "disable env, comma and space separated",
			env:   map[string]string{"TM_LINT_DISABLE": "unused-global,undefined-global invalid-stack-ref"},
			args:  []string{example},
			rules: []string{"unused-let"},
		},
		{
			name:  "flag takes precedence over env",
			env:   map[string]string{"TM_LINT_ENABLE": "unused-let"},
			args:  []string{"--enable", "invalid-stack-ref", example},
			rules: []string{"invalid-stack-ref"},
		},
		{
			name:     "paths env",
			env:      map[string]string{"TM_LINT_PATHS": example + "/stacks/d"},
			contains: []string{"testdata/example/stacks/d/stack.tm.hcl:"},
			excludes: []string{"stacks/a/", "globals.tm.hcl:"},
		},
		{
			name:     "positional args take precedence over paths env",
			env:      map[string]string{"TM_LINT_PATHS": example + "/stacks/d"},
			args:     []string{example + "/stacks/b"},
			contains: []string{"testdata/example/stacks/b/stack.tm.hcl:"},
			excludes: []string{"stacks/d/"},
		},
		{
			name:     "ignore-file env",
			env:      map[string]string{"TM_LINT_IGNORE_FILE": os.DevNull},
			args:     []string{example},
			contains: []string{"global.ci_token is never used", "stacks/c/stack.tm.hcl:"},
		},
		{
			name:     "ignore-globals env",
			env:      map[string]string{"TM_LINT_IGNORE_GLOBALS": "global.unused_root global.net.*"},
			args:     []string{example},
			excludes: []string{"global.unused_root", "global.net.unused_child", "global.net.cidrr"},
		},
		{
			name:     "root env",
			env:      map[string]string{"TM_LINT_ROOT": example},
			args:     []string{example + "/stacks/b"},
			contains: []string{"global.sibling_only is never used"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := runCLI(t, c.env, c.args...)
			if !errors.Is(err, errFindings) {
				t.Fatalf("want errFindings, got %v\n%s", err, out)
			}
			if c.rules != nil {
				got := rulesIn(out)
				if len(got) != len(c.rules) {
					t.Errorf("rules = %v, want %v\n%s", got, c.rules, out)
				}
				for _, r := range c.rules {
					if !got[r] {
						t.Errorf("rule %s missing\n%s", r, out)
					}
				}
			}
			for _, s := range c.contains {
				if !strings.Contains(out, s) {
					t.Errorf("output does not contain %q\n%s", s, out)
				}
			}
			for _, s := range c.excludes {
				if strings.Contains(out, s) {
					t.Errorf("output contains %q\n%s", s, out)
				}
			}
		})
	}
}

func TestCLIListRules(t *testing.T) {
	for name, run := range map[string]func() (string, error){
		"flag": func() (string, error) { return runCLI(t, nil, "--list-rules") },
		"env":  func() (string, error) { return runCLI(t, map[string]string{"TM_LINT_LIST_RULES": "true"}) },
	} {
		t.Run(name, func(t *testing.T) {
			out, err := run()
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rules {
				if !strings.Contains(out, r.name) {
					t.Errorf("rule %s not listed\n%s", r.name, out)
				}
			}
		})
	}
}

func TestCLIErrors(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		args []string
		want string
	}{
		{"unknown rule flag", nil, []string{"--enable", "nope", "testdata/example"}, `unknown rule "nope"`},
		{"unknown rule env", map[string]string{"TM_LINT_DISABLE": "nope"}, []string{"testdata/example"}, `unknown rule "nope"`},
		{"unknown flag", nil, []string{"--nope"}, "unknown flag"},
		{"missing ignore file", map[string]string{"TM_LINT_IGNORE_FILE": "does-not-exist"}, []string{"testdata/example"}, "does-not-exist"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runCLI(t, c.env, c.args...)
			if err == nil || errors.Is(err, errFindings) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestCLIHelpMentionsEnv(t *testing.T) {
	out, err := runCLI(t, nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"TM_LINT_<FLAG>", "TM_LINT_PATHS", "--ignore-file"} {
		if !strings.Contains(out, s) {
			t.Errorf("help does not mention %q", s)
		}
	}
}
