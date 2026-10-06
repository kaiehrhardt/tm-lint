package main

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/terramate-io/hcl/v2"
)

// ignoreMarker in a comment on the reported line (or the line above)
// suppresses findings: alone for all rules, or followed by rule names.
//
//	# tm-lint:ignore
//	# tm-lint:ignore unused-global, undefined-global
const ignoreMarker = "tm-lint:ignore"

type finding struct {
	rule   string
	file   string // absolute path
	rng    hcl.Range
	msg    string
	global []string // global path the finding is about, if any
}

type rule struct {
	name string
	doc  string
	run  func(p *project, opts *options) []finding
}

type options struct {
	ignoreGlobals [][]string
	ignore        *ignoreFile
}

// newOptions builds the lint options. ignoreFile is the path given with
// -ignore-file; empty means the optional .tmlintignore in the project root.
func newOptions(p *project, ignoreFilePath, ignoreGlobals string) (*options, error) {
	required := ignoreFilePath != ""
	if !required {
		ignoreFilePath = filepath.Join(p.root, defaultIgnoreFile)
	}
	ign, err := loadIgnoreFile(ignoreFilePath, required)
	if err != nil {
		return nil, err
	}
	return &options{ignoreGlobals: parseGlobalPatterns(ignoreGlobals), ignore: ign}, nil
}

var rules = []rule{
	{"unused-global", "global is defined but never referenced where it is visible", ruleUnusedGlobal},
	{"undefined-global", "global is referenced but not defined anywhere it would be visible", ruleUndefinedGlobal},
	{"invalid-stack-ref", "after/before/wants/wanted_by entry matches no stack", ruleInvalidStackRef},
	{"unused-let", "let is defined in a block but never referenced in it", ruleUnusedLet},
}

func lint(p *project, opts *options, enabled map[string]bool) []finding {
	var out []finding
	for _, r := range rules {
		if !enabled[r.name] {
			continue
		}
		for _, f := range r.run(p, opts) {
			if !p.suppressed(f) && !opts.ignore.matches(f, p.relFile(f.file)) {
				out = append(out, f)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		if out[i].rng.Start.Byte != out[j].rng.Start.Byte {
			return out[i].rng.Start.Byte < out[j].rng.Start.Byte
		}
		return out[i].rule < out[j].rule
	})
	return dedupe(out)
}

func dedupe(fs []finding) []finding {
	var out []finding
	seen := map[string]bool{}
	for _, f := range fs {
		key := f.rule + "\x00" + f.file + "\x00" + f.rng.String() + "\x00" + f.msg
		if !seen[key] {
			seen[key] = true
			out = append(out, f)
		}
	}
	return out
}

func (p *project) suppressed(f finding) bool {
	lines := strings.Split(string(p.sources[f.file]), "\n")
	for _, ln := range []int{f.rng.Start.Line - 1, f.rng.Start.Line - 2} {
		if ln < 0 || ln >= len(lines) {
			continue
		}
		idx := strings.Index(lines[ln], ignoreMarker)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(lines[ln][idx+len(ignoreMarker):])
		if rest == "" {
			return true
		}
		for _, name := range strings.FieldsFunc(rest, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			if name == f.rule {
				return true
			}
		}
	}
	return false
}

func parseGlobalPatterns(s string) [][]string {
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

// matchesGlobalPattern: a trailing "*" matches anything below the prefix.
func matchesGlobalPattern(gpath []string, patterns [][]string) bool {
	for _, pat := range patterns {
		if len(pat) > 0 && pat[len(pat)-1] == "*" {
			if isPrefix(pat[:len(pat)-1], gpath) {
				return true
			}
			continue
		}
		if len(pat) == len(gpath) && isPrefix(pat, gpath) {
			return true
		}
	}
	return false
}
