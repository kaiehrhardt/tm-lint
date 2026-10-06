// Command tm-lint finds suspicious but valid Terramate configuration.
//
// Usage:
//
//	tm-lint [flags] [project-root]
//
// Exit codes: 0 = no findings, 1 = findings, 2 = error.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	enableFlag := flag.String("enable", "", "comma-separated rules to run (default: all)")
	disableFlag := flag.String("disable", "", "comma-separated rules to skip")
	ignoreGlobals := flag.String("ignore-globals", "", "comma-separated global paths the global rules ignore; a trailing '*' matches everything below (e.g. 'global.ci.*,global.tags')")
	ignoreFile := flag.String("ignore-file", "", "ignore file to use (default: "+defaultIgnoreFile+" in the project root, if present)")
	listRules := flag.Bool("list-rules", false, "print the available rules and exit")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [flags] [project-root]\n\n", os.Args[0])
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

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	p, err := loadProject(root)
	if err != nil {
		fatal(err)
	}

	opts, err := newOptions(p, *ignoreFile, *ignoreGlobals)
	if err != nil {
		fatal(err)
	}

	findings := lint(p, opts, enabled)
	fmt.Print(formatFindings(p, findings))
	if len(findings) > 0 {
		os.Exit(1)
	}
}

func formatFindings(p *project, findings []finding) string {
	var sb strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&sb, "%s:%d:%d: [%s] %s\n", p.relFile(f.file), f.rng.Start.Line, f.rng.Start.Column, f.rule, f.msg)
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
