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
	"runtime/debug"
)

// version is set by release builds via -ldflags "-X main.version=v1.2.3".
var version string

// buildVersion returns the version tm-lint reports for --version: the one
// set at build time, else the module version `go install ...@v1.2.3`
// records, else "dev".
func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		if errors.Is(err, errFindings) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
}
