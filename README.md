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

Dependencies: only Terramate's HCL fork (`github.com/terramate-io/hcl/v2`, the same version Terramate itself uses) and `go-cty`.

## Usage

```sh
tm-lint [flags] [project-root]       # project-root defaults to .

  -list-rules                        show the available rules
  -enable  rule1,rule2               run only these rules (default: all)
  -disable rule1,rule2               skip these rules
  -ignore-globals 'global.ci.*,...'  globals ignored by unused-/undefined-global
  -ignore-file path                  ignore file (default: .tmlintignore in the project root, if present)
```

Run it from the project root, because absolute paths (`/imports/...`, `/stacks/...`) are resolved relative to it.

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

A `.tmlintignore` file in the project root is read automatically. Use `-ignore-file` to point to a different file; an explicitly given file must exist.

One entry per line, `#` starts a comment:

```
<target>                       suppress all rules for target
<rule>[,<rule>...] <target>    suppress only the listed rules
```

A target is either
- a **path glob** relative to the project root: `*` matches within a directory, `**` spans directories, and a directory matches everything below it, or
- a **global path** like `global.ci_token` or `global.ci.*`, which applies to the findings of `unused-global` and `undefined-global`.

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
- **Shadowing:** a root global that is overridden in every child directory is not reported as dead.
- **Hierarchy in `undefined-global`:** a global used in a parent `generate_hcl` but defined in only *some* of the stacks below counts as defined.
- **Non-literal stack attributes:** `stack` attributes that are not literal lists are skipped.

## Adding rules

A rule is a function `func(p *project, opts *options) []finding` registered in `rules` in `lint.go`. `project` provides:
- all parsed files (`files`, `bodies`, `sources`),
- the stacks with their tags (`stacks`),
- the evaluation contexts of a file, taking imports into account (`contexts`),
- the global index (`globals()`).

Add a case for every new rule to `testdata/example` and run `go test -update`.
