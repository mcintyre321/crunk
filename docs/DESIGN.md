# crunk design

crunk is a trusted interpreter for untrusted CI pipelines. It started as a fork of
[togomak](https://github.com/srevinsaju/togomak). Almost all of togomak has been
deleted; what remains is the idea of a Terraform-style HCL pipeline evaluated into a graph.

## Trust model

- **Pipeline HCL is untrusted.** Any developer, on any branch, can write it.
- **Repository contents are untrusted.** That includes file names, symlinks and file sizes.
- **Everything returned from a session is untrusted.** That includes logs, archives and result files.
- **The interpreter is trusted.** It evaluates HCL into a graph of closed container specs
  and dispatches them to Docker endpoints. It never executes user code itself.
- **Only data in a closed schema crosses from user space into the interpreter.** User code
  runs only inside containers on sessions.

Platform limits (allowed registries, size classes, timeouts, caps) come from a
platform config file passed by flag, never from the repository. (Phase 2. Phase 1
uses the built-in `lang.DefaultLimits`.)

## Primitives

There are a few core primitives, and everything else falls out of them:

- **container**: the single unit of work. It has an image, a script, env, and an input JSON.
- **bindings**: the only contract between containers.
  - network: `container.x.url.<port>` and `container.x.address.<port>`
  - files: `files` outputs, consumed as private `mount` snapshots
  - data: `input` goes in, `/run/result.json` comes out as `container.x.result`

  Referencing a binding creates a dependency edge.
- **host**: a logical isolation unit with its own Docker network and filesystem namespace.
- **dimension**: a named fan-out axis. `for_each` on hosts, containers and modules accepts
  only dimensions, and references between blocks expanded over the same dimension broadcast by key.
- **module**: a local directory of the same blocks, used as a namespace, optionally
  expanded over a dimension.

Lifecycle is derived, not declared. A container that another container reaches
through a `port` binding is held alive while its dependents run. Every other container
runs to completion.

## Execution model

Logical hosts map onto physical **sessions**. In production a session is a Sysbox-runtime
pod running its own dockerd, created on demand. On a laptop it is the local daemon.
Developers never see sessions.

- A host can run many containers, in sequence or concurrently.
- A host can stay up (for example, running services) while other hosts run batches of
  containers that connect back to it.
- Several small hosts can share a session, each with its own network. A host never spans sessions.
- Working directories and artefacts move between hosts as volume snapshots, behind a
  `Snapshotter` interface.

## Architecture (current state: Phase 1)

```
cmd/crunk          CLI: `crunk validate --root <repo> [--dir <pipeline dir>]`
internal/lang      Load: read, parse and statically validate pipeline HCL
  schema.go        the vocabulary (blocks, arguments, removed-construct messages)
  load.go          root-confined module loader, limits, structural and reference checks
  funcs.go         the pure function table
testdata/          malicious corpus (must be rejected) and valid pipelines
```

`lang.Load` does not evaluate any expressions. It rejects, at load time:

- unknown blocks and arguments, and removed upstream constructs (with a specific message)
- calls to functions outside the allowlist
- references to roots outside `var`, `local`, `container`, `host`, `dimension`, `each` and `module`
- module sources that are not literal `./`/`../` paths, or that resolve outside the root
  (symlinks are followed first)
- `.hcl` files that are symlinks pointing outside the root, or not regular files
- files over the size limit, too many files, modules nested too deeply, and syntax nested too deeply

Later phases add: `ContainerSpec` evaluation (2), sessions, scheduler and executor (3),
the credential split (4), result store and `plan` (5), deferred expansion (6), and
hardening (7).

## Decisions

- **Remove by deleting.** Upstream host execution, hooks, macros, data sources (env,
  prompt, git, tf), imports, remote modules, pre/post blocks, retries, daemons,
  lifecycle phases, filters and queries, `init`/`list`/`fmt`/`cache`, GCP logging and
  prompts are deleted, not hidden behind flags. See `docs/INVENTORY.md` for the full list.
- **A small function set** (about 60 functions). String, collection, numeric, conversion,
  JSON/base64/url and `sha256`. Built on cty's stdlib plus a few hand-written functions. The
  vendored Terraform function package is gone.
- **One loader rule for modules.** A module is a directory: all of its `*.hcl` files,
  non-recursive. The source must be a literal string, so modules can be resolved before any
  evaluation.
- **Static checks walk the syntax tree** (`hclsyntax`). They don't rely on evaluation
  failing, so errors are early, precise and include the reason.
- **Nesting is bounded twice.** A cheap bracket-depth bound runs before parsing (the HCL
  parser is recursive), and an exact syntax-tree depth check runs after.
- **Variables** take values only from `--var` or `default`. Nothing comes from the
  environment, and nothing is prompted for.
- **Simplifications to the original brief** (see `docs/INVENTORY.md` §10):
  - The scheduler starts with a simple deterministic policy rather than a historical cost model.
  - Only `StaticProvider` is built until a real controller exists.
  - The result store is filesystem-only.

## Future work

- Secrets: none can be expressed yet.
