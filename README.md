# tm-lint

A linter for Terramate configuration. It finds things that are valid, and therefore not reported by Terramate, but are almost always a mistake or dead code.

```
$ tm-lint .
config.tm.hcl:16:3: [unused-global] global.environments is never used
stacks/a/stack.tm.hcl:3:33: [invalid-stack-ref] stack.after entry "/stacks/nope" does not exist (Terramate only warns at run time and ignores it)
stacks/d/stack.tm.hcl:21:21: [undefined-global] global.regoin is not defined anywhere, the tm_try/tm_can fallback is always used, did you mean global.region?
stacks/d/stack.tm.hcl:6:5: [unused-let] let.unused is never used in generate_hcl "d.tf"
```

Exit codes: `0` = no findings, `1` = findings, `2` = error (e.g. a parse error).

## Installation

```sh
go mod tidy
go build -o tm-lint .
go test ./...
```

Dependencies: Terramate's HCL fork (`github.com/terramate-io/hcl/v2`, the same version Terramate itself uses), `go-cty`, and [cobra](https://github.com/spf13/cobra) / [viper](https://github.com/spf13/viper) for the CLI.

## Usage

```sh
tm-lint [path ...] [flags]           # path defaults to the current directory
```

| Flag | Environment variable | Description |
|---|---|---|
| `--root path` | `TM_LINT_ROOT` | Terramate project root (default: detected, see below) |
| `--enable rule,...` | `TM_LINT_ENABLE` | run only these rules (default: all) |
| `--disable rule,...` | `TM_LINT_DISABLE` | skip these rules |
| `--ignore-globals pattern,...` | `TM_LINT_IGNORE_GLOBALS` | globals ignored by `unused-global`/`undefined-global`, e.g. `global.ci.*` |
| `--ignore-file path` | `TM_LINT_IGNORE_FILE` | ignore file (default: `.tmlintignore` in the project root, if present) |
| `--list-rules` | `TM_LINT_LIST_RULES` | print the available rules and exit |
| `--format text\|json\|sarif\|gitlab` | `TM_LINT_FORMAT` | output format (default: `text`) |
| `[path ...]` | `TM_LINT_PATHS` | paths whose findings are reported (default: current directory) |

- **Lists:** list flags accept comma-separated values or can be repeated (`--enable a,b` or `--enable a --enable b`). In environment variables, lists are comma- or space-separated.
- **Precedence:** a flag wins over its environment variable, and positional paths win over `TM_LINT_PATHS`.
- **Output:** file paths are relative to the current working directory.
- **Shell completion:** `tm-lint completion bash|zsh|fish|powershell` prints a completion script. Because `completion` and `help` are subcommands, pass a folder with one of these names as `./completion`.

```sh
# CI: the same configuration via environment variables
export TM_LINT_DISABLE=unused-let
export TM_LINT_IGNORE_GLOBALS="global.ci.* global.tags"
tm-lint stacks/prod
```

### Output formats

- **`text`** (default): `path:line:column: [rule] message`, for editors and terminals.
- **`json`**: an array of findings, `[]` when there are none, each with `rule`, `file`, `line`, `column`, `endLine`, `endColumn` and `message`.
- **`sarif`**: a [SARIF 2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/os/sarif-v2.1.0-os.html) log with one run, for tools that consume it, e.g. [GitHub code scanning](https://docs.github.com/en/code-security/code-scanning/integrating-with-code-scanning/sarif-support-for-code-scanning#uploading-a-sarif-file-to-github):

  ```sh
  tm-lint --format sarif . > tm-lint.sarif
  ```

- **`gitlab`**: a [GitLab Code Quality report](https://docs.gitlab.com/ci/testing/code_quality/#integrate-common-tools-with-code-quality) (every finding reported as severity `major`; tm-lint has no severity levels):

  ```yaml
  # .gitlab-ci.yml
  tm-lint:
    image: golang:1.24
    script:
      - go run github.com/kaiehrhardt/tm-lint@latest --format gitlab . > gl-code-quality-report.json
    artifacts:
      reports:
        codequality: gl-code-quality-report.json
  ```

Exit codes are unaffected by `--format`: `1` on findings, `2` on errors, even for `json`/`sarif`/`gitlab`, so CI steps that only look at the exit code keep working. In the GitLab example above, `script` fails the job on findings; add `|| true` to the `tm-lint` line if you want the report uploaded without failing the pipeline.

### Linting a subfolder

tm-lint always loads the **whole project**, so imports, globals from parent directories and stack references resolve exactly as when linting everything. The paths you pass only limit which findings are **reported**:

```sh
tm-lint                          # everything below the current directory
tm-lint stacks/prod stacks/stg   # only these folders
tm-lint stacks/prod/stack.tm.hcl # only this file

cd stacks/prod && tm-lint        # same as `tm-lint stacks/prod` from the root
```

A finding is reported if its file is inside one of the paths, or if the file is imported into one of them. For example, an unused global in `/imports/common.tm.hcl` shows up when linting a stack that imports it.

The project root is detected the same way Terramate does it: starting at the first path, tm-lint walks upwards to the nearest directory whose Terramate files contain `terramate { required_version = ... }`. Without such a directory, it uses the nearest git repository root, and failing that, the path itself. Use `--root` (or `TM_LINT_ROOT`) to set it explicitly. The `.tmlintignore` is always read from the project root.

### Suppressing findings

There are two ways to suppress findings, and both can be combined.

#### Inline comments

A comment on the same line or the line above suppresses findings, either for all rules or only for the rules listed:

```hcl
globals {
  # tm-lint:ignore unused-global
  ci_token = "..."   # only read by `terramate get-config-value` in CI
}
```

#### `.tmlintignore`

A `.tmlintignore` file in the project root is read automatically. Use `--ignore-file` (or `TM_LINT_IGNORE_FILE`) to point to a different file; an explicitly given file must exist.

One entry per line, `#` starts a comment:

```
<target>                       suppress all rules for target
<rule>[,<rule>...] <target>    suppress only the listed rules
```

A target is either
- a **path glob** relative to the project root: `*` matches within a directory, `**` spans directories, and a directory matches everything below it, or
- a **global path** like `global.ci_token` or `global.ci.*` (or just `global` for every global), which applies to the findings of `unused-global` and `undefined-global`.

```
# Legacy stacks that are being decommissioned.
stacks/legacy

# Read by CI via `terramate get-config-value`.
unused-global global.ci.*

# Generated components are checked upstream.
unused-let,undefined-global components/**/*.tm.hcl
```

Unknown rule names or invalid lines are an error (exit code `2`), so typos in the ignore file do not go unnoticed. See `testdata/example/.tmlintignore` for a complete example.

## Rules

### `unused-global`

A global that is defined but never used anywhere it would be visible.

- **Definitions:**
  - attributes in `globals` blocks, including labels: `globals "a" "b" { c = 1 }` defines `global.a.b.c`
  - `map` blocks
  - empty labeled blocks
- **Uses:** every `global.*` reference in any Terramate expression.
- **Paths:**
  - `global.a` (the whole object) counts as a use of `global.a.b`.
  - `global["x"]` is treated like `global.x`.
  - Dynamic access such as `global.a[var]` counts for everything below `global.a`.
- **Hierarchy:** the definition and the use must be on the same branch of the directory tree.
  - Globals are inherited downwards.
  - Expressions in parent directories are evaluated in the context of every stack below them.
  - A global used only by a sibling stack therefore counts as unused.
- **Imports:** the content of an imported file also applies in every directory importing it, transitively.
- **Self-references:** an override like `region = "${global.region}-c"` counts as a use of the parent value, not as a use of itself.

### `shadowed-global`

A global that is read somewhere, but every read resolves to a more specific override defined closer to it, so this particular definition's value is never actually used. Typically a root default that every stack below it overrides:

```hcl
# globals.tm.hcl
globals {
  region = "eu-west-1"   # shadowed-global: every stack below overrides it
}
```

```hcl
# stacks/a/stack.tm.hcl
globals {
  region = "us-east-1"   # this is the value generate_hcl below actually reads
}
generate_hcl "main.tf" {
  content {
    region = global.region
  }
}
```

- Only the override must use the exact same global path; overriding just one field of an object (`net.cidr`) does not shadow a sibling field (`net.other`).
- Only the common direction is checked: a definition read from its own directory or below. A global read from one of its *own* definition's ancestors (an expression in a parent directory, inherited back down into a descendant stack) is conservatively treated as used, without checking whether that read is itself shadowed.
- As soon as at least one reachable read is not shadowed (e.g. one stack does not override), the definition counts as used.

### `undefined-global`

A global that is referenced but not defined anywhere it would be visible. The same hierarchy and import rules apply as for `unused-global`. If a similarly named global exists, the finding includes a "did you mean" suggestion.

References inside `tm_try(...)`/`tm_can(...)` are usually the "optional value with a default" idiom, e.g. `tm_try(global.app.replicas, 1)`. They are only reported
- when nothing at all is defined under the root name (the fallback is then always used), or
- when the missing name looks like a typo of a defined sibling, e.g. `tm_try(global.net.cidrr, …)` next to `global.net.cidr`.

### `invalid-stack-ref`

Entries in `stack.after`, `before`, `wants` and `wanted_by` that match no stack. For paths that do not exist, Terramate only prints a warning to stderr at run time. Paths without stacks and tag filters without matches are ignored completely silently.

The rule reports:
- paths that do not exist or are not a directory,
- directories with no stacks below them,
- a stack referencing itself,
- `tag:` filters with tags no stack has (typos),
- `tag:` filters that match no stack.

The filter syntax is the same as Terramate's: `,` = OR, `:` = AND, `~` = NOT.

### `unused-let`

A `let` in a `lets` block (e.g. in `generate_hcl`/`generate_file`) that is never used in the surrounding block. Use by another `let` counts, use in commented-out code does not.

## Known limitations

- **No full validation:** the tool only parses files, it does not load the complete Terramate configuration. It complements `terramate fmt --check` and `terramate generate` followed by `git diff --exit-code`, it does not replace them.
- **Other file types:** references outside `.tm`/`.tm.hcl` files are not detected, e.g. `terramate get-config-value` in shell scripts. Use the suppression options for those.
- **Shadowing:** `shadowed-global` only catches an override with the exact same global path, and only checks the "defined above, read at or below" direction; see its section above for both.
- **Hierarchy in `undefined-global`:** a global used in a parent `generate_hcl` but defined in only *some* of the stacks below counts as defined.
- **Non-literal stack attributes:** `stack` attributes that are not literal lists are skipped.

## Development

The layout follows the [official Go recommendation for a command with supporting packages](https://go.dev/doc/modules/layout#command-with-supporting-packages): the command lives in the module root, so `go install github.com/kaiehrhardt/tm-lint@latest` works, and everything else is in `internal/`.

```
.
├── main.go, cli.go         command: cobra/viper CLI, flag wiring
├── format.go               output formatting: text, json, sarif, gitlab
├── *_test.go               CLI and end-to-end tests against testdata/
├── testdata/               example project and its expected output
└── internal/
    ├── project/            loads a Terramate project: files, imports, stacks,
    │                       evaluation contexts, root detection, scopes
    ├── lint/               rules, findings, ignore file, inline suppression
    └── hclutil/            small helpers for the HCL syntax tree
```

```sh
go test ./...           # all tests
go test -update .       # rewrite testdata/example.expected after intentional changes
```

### Adding rules

A rule is a function `func(c *checker) []Finding` in `internal/lint`, registered in `Rules` in `internal/lint/lint.go`. The checker gives access to:
- the project (`c.p`): parsed files (`Files`, `Bodies`, `Sources`), stacks with their tags (`Stacks`), and the evaluation contexts of a file, taking imports into account (`Contexts`),
- the options (`c.opts`),
- the global index (`c.globals()`), built once per run.

Add a unit test next to the rule, and a case to `testdata/example`, then run `go test -update .`.
