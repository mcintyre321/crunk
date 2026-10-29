# The crunk pipeline language

This is the complete vocabulary. **Anything not listed here is rejected** when the
pipeline is loaded, before anything runs. `internal/lang/schema.go` is the same list
in code, and a test checks the function table below against the code.

Status: Phase 1. Structure, names, functions and references are checked when the
pipeline is loaded. The meaning of each value (types, bindings, expansion) is checked
from Phase 2 onwards.

## Files and modules

A module is a directory. All `*.hcl` files directly inside it form the module; they
are merged, and block names must be unique across them. The root module is the
directory given by `--dir` (default `.`) under `--root`.

Nothing outside `--root` is ever read. Symlinks are followed and then checked, and
anything that resolves outside the root is rejected.

## Top-level blocks

| Block | Labels | Purpose |
|---|---|---|
| `container` | name | The single unit of work |
| `host` | name | A logical isolation unit (its own network and filesystem namespace) |
| `dimension` | name | A named fan-out axis |
| `locals` | — | Named expressions |
| `variable` | name | An input to the pipeline or module |
| `module` | name | Includes a local module directory |

Top-level arguments are not allowed.

### `container "<name>"`

| Argument | Required | Meaning |
|---|---|---|
| `image` | yes | `registry/repo[:tag\|@digest]`; registry must be allowlisted; resolved to a digest before dispatch |
| `script` | no | Shell script run by the platform's fixed shell. Omit to run the image's default command |
| `env` | no | Map of environment variables. Nothing is passed through from the host |
| `input` | no | Any value; written as JSON to `/run/input.json` |
| `host` | no | `host.<name>`, or `null` for the current host (the default) |
| `for_each` | no | `dimension.<name>`; also implied by an expanded `host` |
| `size` | no | `"small"`, `"medium"` or `"large"` (platform-defined) |
| `timeout` | no | Duration such as `"30m"`, capped by the platform |
| `when` | no | `"on_success"` (default), `"always"` or `"on_failure"` |
| `allow_failure` | no | Bool, default `false` |
| `depends_on` | no | List of `container.<name>`; only for ordering with no data binding |

| Nested block | Labels | Contents |
|---|---|---|
| `port` | name | `container = <number>` (required) |
| `ready` | — | At most one. Exactly one of `tcp = "<port name>"` or `http { port = "<port name>", path = "/health" }` (`path` optional) |
| `files` | name | `path = "<directory>"` (required): a directory this container provides |
| `mount` | path | `from = container.<x>.files.<name>` (required), `mode = "readonly"` (default) or `"copy"` |

Port names, `files` names and `mount` paths must be unique within a container.
The container name `source` is reserved for the built-in source tree.

### `host "<name>"`

| Argument | Required | Meaning |
|---|---|---|
| `for_each` | no | `dimension.<name>`: one logical host per key |

### `dimension "<name>"`

| Argument | Required | Meaning |
|---|---|---|
| `size` | yes | Number of keys, capped by the platform |

### `locals`

Any number of `name = expression` arguments. Names must be unique within a module.

### `variable "<name>"`

| Argument | Required | Meaning |
|---|---|---|
| `description` | no | Documentation |
| `default` | no | Value used when none is given |
| `type` | no | A type constraint (`string`, `number`, `bool`, `list(...)`, `map(...)`, `object({...})`, `any`, ...) |

Root-module variables are set with `--var name=value` on the command line, or take their
`default`. Module variables are set by the arguments of the `module` block.

### `module "<name>"`

| Argument | Required | Meaning |
|---|---|---|
| `source` | yes | Literal string starting with `./` or `../`; resolves to a directory inside the root |
| `for_each` | no | `dimension.<name>`: every block in the module is expanded over it |
| any other | no | Sets the module's variable of the same name |

Remote sources (git, https, s3, gcs) are not supported. Inside a module expanded with
`for_each`, blocks may not expand over a second dimension.

## References

Expressions may refer only to these roots:

| Root | Meaning |
|---|---|
| `var.<name>` | Variable value |
| `local.<name>` | Local value |
| `container.<name>` | `.url.<port>`, `.address.<port>`, `.files.<name>`, `.result.<path>`; `[key]` selects an instance |
| `container.source.files.tree` | Built-in: a clean snapshot of the source commit |
| `host.<name>` | A logical host |
| `dimension.<name>.size` | Size of a dimension |
| `each.index`, `each.key` | Inside an expanded block: 0-based index (number) and key (string) |
| `module.<name>` | `.container.<x>...` inside a module; `[key]` selects an instance |

Iterator names introduced by `for` expressions are also in scope inside them.

## Functions

Every function is pure and deterministic. There is nothing that reads files, the
environment or the clock, and nothing random.

| Function | Group |
|---|---|
| `chomp`, `endswith`, `format`, `indent`, `join`, `lower`, `regex`, `regexall`, `replace`, `shellescape`, `split`, `startswith`, `strcontains`, `substr`, `trim`, `trimprefix`, `trimspace`, `trimsuffix`, `upper` | string |
| `alltrue`, `anytrue`, `coalesce`, `compact`, `concat`, `contains`, `distinct`, `element`, `flatten`, `keys`, `length`, `lookup`, `merge`, `one`, `range`, `slice`, `sort`, `values`, `zipmap` | collection |
| `abs`, `ceil`, `floor`, `max`, `min`, `parseint`, `sum` | numeric |
| `can`, `try`, `tobool`, `tolist`, `tomap`, `tonumber`, `toset`, `tostring` | conversion |
| `base64decode`, `base64encode`, `jsondecode`, `jsonencode`, `sha256`, `urlencode` | encoding |

Notes:
- `length` accepts a string (counts characters), a collection or an object.
- `coalesce` returns its first non-null argument; an empty string counts as a value.
- `replace(str, substr, replacement)` replaces plain substrings. Use `regex`/`regexall` to match patterns.
- `range` refuses to produce more than 10,000 elements.
- `shellescape` quotes a string for safe use inside `script`.
