// Command tm-lint finds suspicious but valid Terramate configuration.
//
// Usage:
//
//	tm-lint [flags] [path ...]
//
// The whole project is always loaded; only findings in the given paths (and
// in files imported into them) are reported. Every flag can also be set as an
// environment variable TM_LINT_<FLAG>, see `tm-lint --help`.
//
// Exit codes: 0 = no findings, 1 = findings, 2 = error.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		if errors.Is(err, errFindings) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
}
