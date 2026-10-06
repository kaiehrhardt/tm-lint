// Command tm-lint finds suspicious but valid Terramate configuration.
//
// Usage:
//
//	tm-lint [flags] [path ...]
//
// The whole project is always loaded; only findings in the given paths (and
// in files imported into them) are reported.
//
// Exit codes: 0 = no findings, 1 = findings, 2 = error.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	enableFlag := flag.String("enable", "", "comma-separated rules to run (default: all)")
	disableFlag := flag.String("disable", "", "comma-separated rules to skip")
	ignoreGlobals := flag.String("ignore-globals", "", "comma-separated global paths the global rules ignore; a trailing '*' matches everything below (e.g. 'global.ci.*,global.tags')")
	ignoreFile := flag.String("ignore-file", "", "ignore file to use (default: "+defaultIgnoreFile+" in the project root, if present)")
	rootFlag := flag.String("root", "", "Terramate project root (default: detected like Terramate does, from the first path upwards)")
	listRules := flag.Bool("list-rules", false, "print the available rules and exit")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [flags] [path ...]\n\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "Lints the whole Terramate project, but only reports findings in the given\npaths (default: the current directory) and in files imported into them.\n\n")
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), "\nSuppress a finding with a comment on its line or the line above:\n  # %s [rule, ...]\n", ignoreMarker)
		fmt.Fprintf(flag.CommandLine.Output(), "\nor with an entry in %s in the project root:\n  [rule,...] <path-glob | global.path>\n", defaultIgnoreFile)
	}
	flag.Parse()

	if *listRules {
		for _, r := range rules {
			fmt.Printf("%-18s %s\n", r.name, r.doc)
		}
		return
	}

	enabled, err := selectRules(*enableFlag, *disableFlag)
	if err != nil {
		fatal(err)
	}

	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	root := *rootFlag
	if root == "" {
		root, err = detectRoot(paths[0])
		if err != nil {
			fatal(err)
		}
	}
	p, err := loadProject(root)
	if err != nil {
		fatal(err)
	}
	scopes, err := resolveScopes(p.root, paths)
	if err != nil {
		fatal(err)
	}

	opts, err := newOptions(p, *ignoreFile, *ignoreGlobals)
	if err != nil {
		fatal(err)
	}

	findings := filterScope(p, lint(p, opts, enabled), scopes)

	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	fmt.Print(formatFindings(cwd, findings))
	if len(findings) > 0 {
		os.Exit(1)
	}
}

// formatFindings prints file paths relative to base (the working directory),
// so editors and CI can jump to them.
func formatFindings(base string, findings []finding) string {
	var sb strings.Builder
	for _, f := range findings {
		file, err := filepath.Rel(base, f.file)
		if err != nil {
			file = f.file
		}
		fmt.Fprintf(&sb, "%s:%d:%d: [%s] %s\n", filepath.ToSlash(file), f.rng.Start.Line, f.rng.Start.Column, f.rule, f.msg)
	}
	return sb.String()
}

func selectRules(enable, disable string) (map[string]bool, error) {
	known := map[string]bool{}
	for _, r := range rules {
		known[r.name] = true
	}
	split := func(s string) ([]string, error) {
		var out []string
		for _, n := range strings.Split(s, ",") {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if !known[n] {
				return nil, fmt.Errorf("unknown rule %q (see -list-rules)", n)
			}
			out = append(out, n)
		}
		return out, nil
	}

	en, err := split(enable)
	if err != nil {
		return nil, err
	}
	dis, err := split(disable)
	if err != nil {
		return nil, err
	}

	enabled := map[string]bool{}
	if len(en) == 0 {
		for n := range known {
			enabled[n] = true
		}
	}
	for _, n := range en {
		enabled[n] = true
	}
	for _, n := range dis {
		delete(enabled, n)
	}
	return enabled, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(2)
}
