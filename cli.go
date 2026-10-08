package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kaiehrhardt/tm-lint/internal/lint"
	"github.com/kaiehrhardt/tm-lint/internal/project"
)

// envPrefix is the prefix of the environment variables. Every flag can be set
// as TM_LINT_<FLAG>, with dashes replaced by underscores (e.g. --ignore-file
// becomes TM_LINT_IGNORE_FILE). Flags take precedence over the environment.
const envPrefix = "TM_LINT"

// Configuration keys. Each one is a flag and an environment variable, except
// keyPaths, which is the environment counterpart of the positional arguments.
const (
	keyRoot          = "root"
	keyEnable        = "enable"
	keyDisable       = "disable"
	keyIgnoreGlobals = "ignore-globals"
	keyIgnoreFile    = "ignore-file"
	keyListRules     = "list-rules"
	keyFormat        = "format"
	keyPaths         = "paths"
)

// errFindings signals that the lint run succeeded but reported findings, so
// main exits with 1 instead of 2.
var errFindings = errors.New("findings reported")

func newRootCmd() *cobra.Command {
	v := viper.New()
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	cmd := &cobra.Command{
		Use:   "tm-lint [path ...]",
		Short: "Find suspicious but valid Terramate configuration",
		Long: `tm-lint finds Terramate configuration that is valid, and therefore not
reported by Terramate, but is almost always a mistake or dead code.

The whole project is always loaded, so imports, globals from parent
directories and stack references resolve as usual. The paths only limit
which findings are reported (default: the current directory); files
imported into a path are reported as well.

Exit codes: 0 = no findings, 1 = findings, 2 = error.`,
		Example: `  tm-lint                                  # lint below the current directory
  tm-lint stacks/prod stacks/stg           # only these folders
  tm-lint --disable unused-let             # skip a rule
  tm-lint --format sarif . > tm-lint.sarif # SARIF for GitHub code scanning
  tm-lint --format gitlab . > gl-code-quality-report.json # GitLab Code Quality
  TM_LINT_ENABLE=unused-global tm-lint     # same flags as environment variables`,
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.OutOrStdout(), v, args)
		},
	}

	f := cmd.Flags()
	f.String(keyRoot, "", "Terramate project root (default: detected like Terramate does, from the first path upwards)")
	f.StringSlice(keyEnable, nil, "rules to run, comma-separated or repeated (default: all)")
	f.StringSlice(keyDisable, nil, "rules to skip, comma-separated or repeated")
	f.StringSlice(keyIgnoreGlobals, nil, "global paths ignored by unused-global and undefined-global; a trailing '*' matches everything below (e.g. global.ci.*)")
	f.String(keyIgnoreFile, "", "ignore file (default: "+lint.DefaultIgnoreFile+" in the project root, if present)")
	f.Bool(keyListRules, false, "print the available rules and exit")
	f.String(keyFormat, formatText, "output format: "+formatText+", "+formatJSON+", "+formatSarif+" or "+formatGitLab)
	if err := v.BindPFlags(f); err != nil {
		panic(err) // only fails for a nil flag set
	}
	if err := v.BindEnv(keyPaths); err != nil {
		panic(err)
	}

	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w\nRun '%s --help' for usage", err, c.CommandPath())
	})
	cmd.SetHelpTemplate(cmd.HelpTemplate() + envHelp())
	return cmd
}

func envHelp() string {
	return fmt.Sprintf(`
Environment:
  Every flag can also be set as %[1]s_<FLAG>, e.g. %[1]s_IGNORE_FILE for
  --ignore-file. Lists are comma- or space-separated. Flags take precedence.
  %[1]s_PATHS sets the paths when none are given as arguments.

Suppressing findings:
  # %[2]s [rule, ...]             comment on the line or the line above
  [rule,...] <path-glob | global.path>     entry in %[3]s in the project root
`, envPrefix, lint.IgnoreMarker, lint.DefaultIgnoreFile)
}

func run(out io.Writer, v *viper.Viper, args []string) error {
	if v.GetBool(keyListRules) {
		for _, r := range lint.Rules {
			fmt.Fprintf(out, "%-18s %s\n", r.Name, r.Doc)
		}
		return nil
	}

	format := v.GetString(keyFormat)
	if !outputFormats[format] {
		return fmt.Errorf("unknown format %q (want %s, %s, %s or %s)", format, formatText, formatJSON, formatSarif, formatGitLab)
	}

	enabled, err := lint.SelectRules(listValue(v, keyEnable), listValue(v, keyDisable))
	if err != nil {
		return err
	}

	paths := args
	if len(paths) == 0 {
		paths = listValue(v, keyPaths)
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}

	root := v.GetString(keyRoot)
	if root == "" {
		if root, err = project.DetectRoot(paths[0]); err != nil {
			return err
		}
	}
	p, err := project.Load(root)
	if err != nil {
		return err
	}
	scopes, err := p.ResolveScopes(paths)
	if err != nil {
		return err
	}

	opts, err := lint.NewOptions(p, v.GetString(keyIgnoreFile), listValue(v, keyIgnoreGlobals))
	if err != nil {
		return err
	}

	findings := lint.FilterScope(p, lint.Run(p, opts, enabled), scopes)

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	rendered, err := render(format, cwd, findings)
	if err != nil {
		return err
	}
	fmt.Fprint(out, rendered)
	if len(findings) > 0 {
		return errFindings
	}
	return nil
}

// listValue reads a list from a flag or an environment variable. Flags arrive
// already split; environment values are split on commas and whitespace.
func listValue(v *viper.Viper, key string) []string {
	var out []string
	for _, item := range v.GetStringSlice(key) {
		for _, part := range strings.FieldsFunc(item, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}) {
			out = append(out, part)
		}
	}
	return out
}
