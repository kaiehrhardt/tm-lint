package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

// defaultIgnoreFile is read from the project root when it exists.
const defaultIgnoreFile = ".tmlintignore"

// ignoreFile holds the entries of a .tmlintignore file.
//
// Format, one entry per line, '#' starts a comment:
//
//	<target>                  suppress all rules for target
//	<rule>[,<rule>...] <target>  suppress only the listed rules
//
// A target is either a path glob relative to the project root
// (`*` within a segment, `**` across segments; a directory matches everything
// below it) or a global path (`global.ci_token`, `global.ci.*`), which applies
// to the findings of unused-global and undefined-global.
type ignoreFile struct {
	path    string
	entries []ignoreEntry
}

type ignoreEntry struct {
	line   int
	rules  map[string]bool // nil = all rules
	pathRe *regexp.Regexp  // set for path targets
	global [][]string      // set for global targets
}

// loadIgnoreFile parses the ignore file at path. A missing file is only an
// error when required is true.
func loadIgnoreFile(path string, required bool) (*ignoreFile, error) {
	f, err := os.Open(path)
	if err != nil {
		if !required && errors.Is(err, fs.ErrNotExist) {
			return &ignoreFile{}, nil
		}
		return nil, err
	}
	defer f.Close()

	known := map[string]bool{}
	for _, r := range rules {
		known[r.name] = true
	}

	ign := &ignoreFile{path: path}
	sc := bufio.NewScanner(f)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) > 2 {
			return nil, fmt.Errorf("%s:%d: expected `[rule,...] target`, got %d fields", path, lineNo, len(fields))
		}

		entry := ignoreEntry{line: lineNo}
		target := fields[len(fields)-1]
		if len(fields) == 2 {
			entry.rules = map[string]bool{}
			for _, r := range strings.Split(fields[0], ",") {
				if r == "" {
					continue
				}
				if !known[r] {
					return nil, fmt.Errorf("%s:%d: unknown rule %q (see -list-rules)", path, lineNo, r)
				}
				entry.rules[r] = true
			}
		}

		if target == "global" || strings.HasPrefix(target, "global.") {
			entry.global = parseGlobalPatterns(target)
			if len(entry.global) == 0 {
				entry.global = [][]string{{"*"}}
			}
		} else {
			re, err := globToRegexp(target)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid path pattern %q: %w", path, lineNo, target, err)
			}
			entry.pathRe = re
		}
		ign.entries = append(ign.entries, entry)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return ign, nil
}

// matches reports whether finding f (in project-relative file relFile) is
// suppressed by any entry.
func (ign *ignoreFile) matches(f finding, relFile string) bool {
	if ign == nil {
		return false
	}
	for _, e := range ign.entries {
		if e.rules != nil && !e.rules[f.rule] {
			continue
		}
		switch {
		case e.pathRe != nil:
			if e.pathRe.MatchString(relFile) {
				return true
			}
		case e.global != nil:
			if f.global != nil && matchesGlobalPattern(f.global, e.global) {
				return true
			}
		}
	}
	return false
}

// globToRegexp converts a gitignore-like path glob into an anchored regexp.
// A pattern also matches everything below it, so `stacks/legacy` covers
// `stacks/legacy/stack.tm.hcl`.
func globToRegexp(glob string) (*regexp.Regexp, error) {
	glob = strings.TrimPrefix(glob, "./")
	glob = strings.TrimPrefix(glob, "/")
	glob = strings.TrimSuffix(glob, "/")
	if glob == "" {
		return nil, fmt.Errorf("empty pattern")
	}

	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			i++
			if i+1 < len(glob) && glob[i+1] == '/' {
				i++
				sb.WriteString("(?:.*/)?") // `**/` = zero or more directories
			} else {
				sb.WriteString(".*")
			}
		case c == '*':
			sb.WriteString("[^/]*")
		case c == '?':
			sb.WriteString("[^/]")
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("(?:/.*)?$")
	return regexp.Compile(sb.String())
}
