package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/kaiehrhardt/tm-lint/internal/lint"
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
			for _, r := range lint.Rules {
				if !strings.Contains(out, r.Name) {
					t.Errorf("rule %s not listed\n%s", r.Name, out)
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
		{"unknown format flag", nil, []string{"--format", "bogus", "testdata/example"}, `unknown format "bogus"`},
		{"unknown format env", map[string]string{"TM_LINT_FORMAT": "bogus"}, []string{"testdata/example"}, `unknown format "bogus"`},
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

func TestCLIFormatJSON(t *testing.T) {
	out, err := runCLI(t, nil, "--format", "json", "--enable", "unused-let", "testdata/example")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want errFindings, got %v\n%s", err, out)
	}

	var findings []jsonFinding
	if err := json.Unmarshal([]byte(out), &findings); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(findings) == 0 {
		t.Fatalf("no findings decoded\n%s", out)
	}
	for _, f := range findings {
		if f.Rule != "unused-let" {
			t.Errorf("rule = %q, want unused-let", f.Rule)
		}
		if f.File == "" || f.Message == "" || f.Line == 0 || f.Column == 0 {
			t.Errorf("incomplete finding: %+v", f)
		}
	}
}

func TestCLIFormatJSONNoFindings(t *testing.T) {
	out, err := runCLI(t, nil, "--format", "json", "--enable", "unused-let", "testdata/example/stacks/a")
	if err != nil {
		t.Fatalf("want no error, got %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("got %q, want an empty JSON array", out)
	}
}

func TestCLIFormatSarif(t *testing.T) {
	out, err := runCLI(t, nil, "--format", "sarif", "--enable", "unused-let", "testdata/example")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want errFindings, got %v\n%s", err, out)
	}

	var log sarifLog
	if err := json.Unmarshal([]byte(out), &log); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if log.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", log.Version)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(log.Runs))
	}
	run := log.Runs[0]
	if len(run.Tool.Driver.Rules) != len(lint.Rules) {
		t.Errorf("driver declares %d rules, want %d (all of them)", len(run.Tool.Driver.Rules), len(lint.Rules))
	}
	if len(run.Results) == 0 {
		t.Fatalf("no results decoded\n%s", out)
	}
	for _, r := range run.Results {
		if r.RuleID != "unused-let" {
			t.Errorf("ruleId = %q, want unused-let", r.RuleID)
		}
		loc := r.Locations[0].PhysicalLocation
		if loc.ArtifactLocation.URI == "" || loc.Region.StartLine == 0 {
			t.Errorf("incomplete location: %+v", loc)
		}
	}
}

func TestCLIFormatGitLab(t *testing.T) {
	out, err := runCLI(t, nil, "--format", "gitlab", "--enable", "unused-let", "testdata/example")
	if !errors.Is(err, errFindings) {
		t.Fatalf("want errFindings, got %v\n%s", err, out)
	}

	var issues []gitlabIssue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(issues) == 0 {
		t.Fatalf("no issues decoded\n%s", out)
	}
	seen := map[string]bool{}
	for _, iss := range issues {
		if iss.CheckName != "unused-let" {
			t.Errorf("check_name = %q, want unused-let", iss.CheckName)
		}
		if iss.Severity != "major" {
			t.Errorf("severity = %q, want major", iss.Severity)
		}
		if iss.Description == "" || iss.Location.Path == "" || iss.Location.Lines.Begin == 0 {
			t.Errorf("incomplete issue: %+v", iss)
		}
		if iss.Fingerprint == "" || seen[iss.Fingerprint] {
			t.Errorf("fingerprint missing or duplicate: %+v", iss)
		}
		seen[iss.Fingerprint] = true
	}
}

func TestCLIFormatGitLabNoFindings(t *testing.T) {
	out, err := runCLI(t, nil, "--format", "gitlab", "--enable", "unused-let", "testdata/example/stacks/a")
	if err != nil {
		t.Fatalf("want no error, got %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("got %q, want an empty JSON array", out)
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
