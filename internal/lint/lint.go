// Package lint implements the tm-lint rules and the ways to suppress their
// findings (inline comments, ignore files, global patterns).
package lint

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/terramate-io/hcl/v2"

	"github.com/kaiehrhardt/tm-lint/internal/hclutil"
	"github.com/kaiehrhardt/tm-lint/internal/project"
)

// IgnoreMarker in a comment on the reported line (or the line above)
// suppresses findings: alone for all rules, or followed by rule names.
//
//	# tm-lint:ignore
//	# tm-lint:ignore unused-global, undefined-global
const IgnoreMarker = "tm-lint:ignore"

// Finding is a single problem reported by a rule.
type Finding struct {
	Rule   string
	File   string // absolute path
	Range  hcl.Range
	Msg    string
	Global []string // global path the finding is about, if any
}

// Rule is a single check.
type Rule struct {
	Name string
	Doc  string
	run  func(c *checker) []Finding
}

// Rules are all available rules, in the order they run.
var Rules = []Rule{
	{"unused-global", "global is defined but never referenced where it is visible", ruleUnusedGlobal},
	{"shadowed-global", "global is read somewhere, but always through a more specific override", ruleShadowedGlobal},
	{"undefined-global", "global is referenced but not defined anywhere it would be visible", ruleUndefinedGlobal},
	{"invalid-stack-ref", "after/before/wants/wanted_by entry matches no stack", ruleInvalidStackRef},
	{"unused-let", "let is defined in a block but never referenced in it", ruleUnusedLet},
}

func knownRules() map[string]bool {
	known := map[string]bool{}
	for _, r := range Rules {
		known[r.Name] = true
	}
	return known
}

// SelectRules returns the set of rules to run: all of them unless enable is
// non-empty, minus the disabled ones. Unknown rule names are an error.
func SelectRules(enable, disable []string) (map[string]bool, error) {
	known := knownRules()
	for _, n := range append(append([]string{}, enable...), disable...) {
		if !known[n] {
			return nil, fmt.Errorf("unknown rule %q (see --list-rules)", n)
		}
	}

	enabled := map[string]bool{}
	if len(enable) == 0 {
		for n := range known {
			enabled[n] = true
		}
	}
	for _, n := range enable {
		enabled[n] = true
	}
	for _, n := range disable {
		delete(enabled, n)
	}
	return enabled, nil
}

// Options configure a lint run.
type Options struct {
	// IgnoreGlobals are global patterns (see ParseGlobalPatterns) that the
	// global rules skip.
	IgnoreGlobals [][]string
	// Ignore is the parsed ignore file; nil means none.
	Ignore *IgnoreFile
}

// NewOptions builds the options for project p. ignoreFile is an explicitly
// requested ignore file; empty means the optional DefaultIgnoreFile in the
// project root.
func NewOptions(p *project.Project, ignoreFile string, ignoreGlobals []string) (*Options, error) {
	required := ignoreFile != ""
	if !required {
		ignoreFile = filepath.Join(p.Root, DefaultIgnoreFile)
	}
	ign, err := LoadIgnoreFile(ignoreFile, required)
	if err != nil {
		return nil, err
	}
	return &Options{
		IgnoreGlobals: ParseGlobalPatterns(strings.Join(ignoreGlobals, ",")),
		Ignore:        ign,
	}, nil
}

// checker carries the state of one lint run that rules share.
type checker struct {
	p    *project.Project
	opts *Options
	idx  *globalIndex
}

func (c *checker) globals() *globalIndex {
	if c.idx == nil {
		c.idx = buildGlobalIndex(c.p)
	}
	return c.idx
}

// Run runs the enabled rules on p and returns the findings that are not
// suppressed, sorted by file and position.
func Run(p *project.Project, opts *Options, enabled map[string]bool) []Finding {
	if opts == nil {
		opts = &Options{}
	}
	c := &checker{p: p, opts: opts}

	var out []Finding
	for _, r := range Rules {
		if !enabled[r.Name] {
			continue
		}
		for _, f := range r.run(c) {
			if !suppressedInline(p, f) && !opts.Ignore.Matches(f, p.RelFile(f.File)) {
				out = append(out, f)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Range.Start.Byte != out[j].Range.Start.Byte {
			return out[i].Range.Start.Byte < out[j].Range.Start.Byte
		}
		return out[i].Rule < out[j].Rule
	})
	return dedupe(out)
}

// FilterScope keeps the findings whose file belongs to one of the scopes.
func FilterScope(p *project.Project, findings []Finding, scopes []project.Scope) []Finding {
	var out []Finding
	for _, f := range findings {
		if p.InScope(f.File, scopes) {
			out = append(out, f)
		}
	}
	return out
}

func dedupe(fs []Finding) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, f := range fs {
		key := f.Rule + "\x00" + f.File + "\x00" + f.Range.String() + "\x00" + f.Msg
		if !seen[key] {
			seen[key] = true
			out = append(out, f)
		}
	}
	return out
}

// suppressedInline reports whether an IgnoreMarker comment on the finding's
// line or the line above suppresses it.
func suppressedInline(p *project.Project, f Finding) bool {
	lines := strings.Split(string(p.Sources[f.File]), "\n")
	for _, ln := range []int{f.Range.Start.Line - 1, f.Range.Start.Line - 2} {
		if ln < 0 || ln >= len(lines) {
			continue
		}
		idx := strings.Index(lines[ln], IgnoreMarker)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(lines[ln][idx+len(IgnoreMarker):])
		if rest == "" {
			return true
		}
		for _, name := range strings.FieldsFunc(rest, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			if name == f.Rule {
				return true
			}
		}
	}
	return false
}

// ParseGlobalPatterns parses comma-separated global paths such as
// "global.ci.*,global.tags". A trailing "*" matches everything below.
func ParseGlobalPatterns(s string) [][]string {
	var out [][]string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		item = strings.TrimPrefix(item, "global.")
		if item == "" {
			continue
		}
		out = append(out, strings.Split(item, "."))
	}
	return out
}

func matchesGlobalPattern(gpath []string, patterns [][]string) bool {
	for _, pat := range patterns {
		if len(pat) > 0 && pat[len(pat)-1] == "*" {
			if hclutil.IsPrefix(pat[:len(pat)-1], gpath) {
				return true
			}
			continue
		}
		if len(pat) == len(gpath) && hclutil.IsPrefix(pat, gpath) {
			return true
		}
	}
	return false
}
