# The crunk pipeline language

This is the complete vocabulary. **Anything not listed here is rejected** when the pipeline
is loaded, before anything runs.

> **Status.** `crunk validate` checks everything in this document statically: blocks,
> arguments, nesting, scopes, references and functions. Values (types, bounds, schemas,
> expansion) are checked by the evaluator, which comes next. Functions marked *planned* are
> not implemented yet.

## Principles

- **One primitive.** Only containers run. There are no plugins, providers or host execution.
- **Bindings, not wiring.** Containers connect only through ports, file snapshots and
  JSON results. A reference creates the dependency.
- **Structure says what is shared.** `parallel` and `host` blocks nest. Names resolve to
  the nearest enclosing scope, so what repeats and what is shared is visible from the
  indentation.
- **Deterministic evaluation.** Every input to evaluation is content-addressed: source,
  snapshots, results and variables. The same inputs always give the same graph.
- **No host access.** No host paths, environment, clock or randomness. Files can be read
  only from snapshots.

## Files and modules

A module is a directory. All `*.hcl` files directly inside it form the module; they are
merged, and names must be unique within each scope. The root module is `--dir`
(default `.`) under `--root`. Nothing outside `--root` is read. Symlinks are followed and
then checked.

## Blocks

| Block | Labels | May appear in | May contain |
|---|---|---|---|
| `container` | name | top level, `parallel`, `host` | `port`, `ready`, `files`, `mount` |
| `parallel` | name | top level, `parallel` | `parallel`, `host`, `container`, `module`, `locals` |
| `host` | name | top level, `parallel` | `container`, `module`, `locals` |
| `module` | name | top level, `parallel`, `host` | — |
| `locals` | — | top level, `parallel`, `host` | — |
| `variable` | name | top level | — |
| `schema` | name | top level | — |

Containers outside any `host` block run on the **current host**, the build's root host.
Hosts do not nest. A module called inside a `host` block may not declare hosts of its own.

### `container "<name>"`

| Argument | Required | Meaning |
|---|---|---|
| `image` | yes | `registry/repo[:tag\|@digest]`. The registry must be allowlisted; the image is resolved to a digest before dispatch |
| `script` | no | Run by the platform's fixed shell. Omit it to run the image's default command |
| `env` | no | Map of strings. Nothing is passed through from the host |
| `input` | no | Any value; written as JSON to `/run/input.json` |
| `result_schema` | no | `schema.<name>`. `/run/result.json` must conform, and it also types `.result` before it exists |
| `size` | no | `"small"`, `"medium"` or `"large"` (platform-defined) |
| `timeout` | no | Duration such as `"30m"`, capped by the platform |
| `when` | no | `"on_success"` (default), `"always"` or `"on_failure"` |
| `allow_failure` | no | Bool, default `false` |
| `depends_on` | no | List of container references, for ordering with no data binding |

| Nested block | Labels | Contents |
|---|---|---|
| `port` | name | `container = <number>` (required) |
| `ready` | — | At most one. Exactly one of `tcp = "<port>"` or `http { port = "<port>", path = "/health" }` |
| `files` | name | `path = "<directory>"` (required): an output snapshot |
| `mount` | path | `from = <snapshot>` (required); `mode = "readonly"` (default) or `"copy"` (private writable copy) |

A container may write `/run/result.json`. It must be a JSON object, within the platform's size
limit, and must conform to `result_schema` if one is given. The name `source` is reserved.

### `parallel "<name>"`

Repeats everything inside it.

| Argument | Required | Meaning |
|---|---|---|
| `count` | one of | Number of iterations |
| `for_each` | one of | A list of strings (each string is the key), or a map (its keys are the keys) |
| `min` | no | Fewer iterations is a runtime error. Use `min = 1` for "no tests found means failure" |
| `max` | no | More iterations is a runtime error; also tightens the plan's bounds |

Inside the block, `<name>.index` (0-based number), `<name>.key` (string) and `<name>.value`
(the element, for `for_each`) are in scope. `count` and `for_each` may depend on values that
are known only at runtime; the block expands once they are known.

Keys must be unique, at most 64 characters, and drawn from letters, digits, `.`, `-` and `_`.
The platform caps the number of keys.

### `host "<name>"`

A logical isolation unit with its own network and filesystem namespace. Everything inside it
runs there. Inside a `parallel` block there is one host per iteration. A host has no arguments.

A `host` block decides **placement only; it is not a naming scope**. Its containers, modules and
locals belong to the enclosing scope, so a container inside `host "services"` at the top level
is still `container.<name>` from anywhere at the top level.

### `module "<name>"`

| Argument | Required | Meaning |
|---|---|---|
| `source` | yes | A literal string starting with `./` or `../` that resolves to a directory inside the root |
| any other | no | Sets the module's variable of the same name |

To repeat a module, put the `module` block inside `parallel`.

### `locals`

Any number of `name = expression` arguments, scoped to the enclosing `parallel` block or module.

### `variable "<name>"`

| Argument | Required | Meaning |
|---|---|---|
| `description` | no | Documentation |
| `default` | no | Used when no value is given |
| `type` | no | HCL type constraint: `string`, `number`, `bool`, `list(T)`, `map(T)`, `object({...})` with `optional(T, default)`, `any` |

Root-module variables are set with `--var name=value` or take their default. Module variables
are set by the `module` block's arguments.

### `schema "<name>"`

Exactly one of:

| Argument | Meaning |
|---|---|
| `type` | An HCL type constraint (as in `variable`) |
| `json` | A JSON Schema, given as a string (for example `file(container.source.files.tree, "ci/x.schema.json")`) or an HCL object |

Schemas are used by `conform()` and `result_schema`.

## References and scope

| Reference | Meaning |
|---|---|
| `var.<name>` | Variable |
| `local.<name>` | Local, from the nearest enclosing scope |
| `container.<name>` | Container in the nearest enclosing scope. It has `.url.<port>`, `.address.<port>`, `.files.<name>` (a snapshot) and `.result.<path>` |
| `container.source.files.tree` | Built-in: a snapshot of a clean export of the source commit |
| `parallel.<name>[<key>].container.<x>` | A specific iteration's container, from outside the block |
| `parallel.<name>[*].container.<x>...` | All iterations: a list. In `depends_on` it means all of them; as a `mount.from`, each iteration's snapshot is mounted at `<path>/<key>/` |
| `<parallel name>.index` / `.key` / `.value` | The current iteration of an enclosing `parallel` block |
| `module.<name>.container.<x>` | A container inside a module |
| `schema.<name>` | A schema |

Only `parallel` blocks and modules create naming scopes. Name resolution goes from the
innermost scope outwards, so an inner name hides an outer one with the same name. Names must
be unique within a scope, and a `parallel` block may not reuse the name of a `parallel` block
that encloses it.

Rules the validator enforces:

- A reference into a `parallel` block you are not inside needs `[<key>]` or `[*]`.
- A container reference must continue with `.url.<port>`, `.address.<port>`, `.files.<name>`
  or `.result...`, and the port or `files` name must be declared. The exception is
  `depends_on`, whose entries are bare container references (including `[*]` ones).
- `mount.from` must be a `files` output, or `subdir(...)` of one.
- `result_schema` must be `schema.<name>`.
- Variable defaults and descriptions may not refer to anything.
- `count`, `for_each`, `min` and `max` are evaluated in the scope *around* the `parallel`
  block, so they cannot use the block's own `<name>.index`.
- `<name>.value` is only available when the block uses `for_each`.
- Names that are used as expression roots (`var`, `local`, `container`, `parallel`, `module`,
  `schema`, `host`, `each`, `self`, `count`) cannot name a `parallel` block.

Iterator names introduced by `for` expressions are also in scope inside them. Inside strings,
`${...}` is interpolation; write `$${...}` for a literal `${...}`, such as a shell variable.

## Values

- **Snapshots** are immutable, content-addressed directory trees: `files` outputs, the source
  tree, and `subdir(...)` of either. They can be passed to `mount.from`, `glob`, `subdir` and `file`.
  Anything else is a type error.
- **Unknown values** are values that depend on containers that haven't run. They carry their
  type (from `result_schema`, or as inferred) and **refinements** where known: a number range,
  a collection length range, not-null, or a string prefix. Refinements come from JSON Schema
  bounds, `parallel` `min`/`max`, `glob` caps and platform caps. They propagate through
  expressions and nesting (nested `parallel` counts multiply).

## Lifecycle

Lifecycle is derived, not declared:

- A container reached by another container through `.url`/`.address` is **held alive**. Its
  dependents start only after its `ready` probe passes, and it is stopped after its last
  dependent finishes, and always at build end, failure or cancel.
- Every other container **runs to completion**; its exit code is its status.
- `when` and `allow_failure` decide what runs after a failure. There are no hooks.

## Functions

Every function is pure and deterministic. The functions that read files take a snapshot,
never a bare path.

| Function | Group |
|---|---|
| `chomp`, `endswith`, `format`, `indent`, `join`, `lower`, `regex`, `regexall`, `replace`, `shellescape`, `split`, `startswith`, `strcontains`, `substr`, `trim`, `trimprefix`, `trimspace`, `trimsuffix`, `upper` | string |
| `alltrue`, `anytrue`, `coalesce`, `compact`, `concat`, `contains`, `distinct`, `element`, `flatten`, `keys`, `length`, `lookup`, `merge`, `one`, `range`, `slice`, `sort`, `values`, `zipmap` | collection |
| `abs`, `ceil`, `floor`, `max`, `min`, `parseint`, `sum` | numeric |
| `can`, `try`, `tobool`, `tolist`, `tomap`, `tonumber`, `toset`, `tostring` | conversion |
| `base64decode`, `base64encode`, `jsondecode`, `jsonencode`, `sha256`, `urlencode` | encoding |

### Planned

| Function | Group | Meaning |
|---|---|---|
| `glob(snapshot, pattern)` | snapshot | Sorted list of relative paths matching `*`, `?`, `**`, `[...]`. Symlinks are listed, never followed |
| `subdir(snapshot, path)` | snapshot | A snapshot of one directory |
| `file(snapshot, path)` | snapshot | The file's contents as UTF-8, size-capped |
| `basename(path)`, `dirname(path)` | string | `/`-separated path helpers; they touch no filesystem |
| `yamldecode`, `yamlencode` | encoding | YAML, with alias/anchor expansion capped |
| `conform(value, schema)` | conversion | Returns `value` converted to the schema's type; an error if it doesn't conform |

Notes:
- `length` accepts a string (counts characters), a collection or an object.
- `coalesce` returns its first non-null argument; an empty string counts as a value.
- `replace(str, substr, replacement)` replaces plain substrings. Use `regex`/`regexall` for patterns.
- `range` refuses to produce more than 10,000 elements.
- `shellescape` quotes a string for safe use inside `script`.

## Example

```hcl
container "build" {
  image = "mcr.microsoft.com/dotnet/sdk:8.0"
  size  = "large"
  mount "/src" {
    from = container.source.files.tree
    mode = "copy"
  }
  script = "cd /src && dotnet build MyApp.sln -c Release"
  files "out" { path = "/src" }
}

container "api" {                                   # one, shared
  image = "registry.local/app:latest"
  port "http" { container = 8080 }
  ready {
    http {
      port = "http"
      path = "/health"
    }
  }
}

parallel "project" {                                # one per test assembly, known after build
  for_each = {
    for dll in glob(container.build.files.out, "**/bin/Release/*/*.Tests.dll") :
    trimsuffix(basename(dll), ".dll") => dll
  }
  min = 1

  host "runner" {
    container "test" {
      image = "mcr.microsoft.com/dotnet/sdk:8.0"
      mount "/test" { from = subdir(container.build.files.out, dirname(project.value)) }
      env = {
        DLL     = basename(project.value)
        API_URL = container.api.url.http
      }
      script = "dotnet test /test/$DLL --logger trx --results-directory /results"
      files "results" { path = "/results" }
    }
  }
}

container "report" {
  image  = "registry.local/trx-merge:1"
  mount "/results" { from = parallel.project[*].container.test.files.results }
  when   = "always"
  script = "trx-merge /results/*/*.trx > /out/summary.md"
}
```
