# crunk design

crunk is a trusted interpreter for untrusted CI pipelines written in HCL. It evaluates a
pipeline into a stream of closed container specs and runs them on Docker endpoints. It never
runs pipeline code itself.

This document records the **locked design**. `docs/LANGUAGE.md` is the language reference.

## Trust model

- **Pipeline HCL is untrusted.** Any developer, on any branch, can write it.
- **Repository contents are untrusted:** file names, symlinks, sizes and contents.
- **Everything that comes back from a session is untrusted:** logs, archives, result files and file listings.
- **The interpreter is trusted.** It evaluates HCL and dispatches containers; user code runs
  only inside containers on sessions.
- **Only data in a closed schema crosses into the interpreter.** That data is: `result.json`
  (a JSON object, size-capped, optionally schema-checked), snapshot listings (validated paths),
  and snapshot file contents read through `file()` (size-capped UTF-8).
- **Platform limits come from a platform config file passed by flag, never from the repository.**
  They cover allowed registries, size classes, timeouts, caps on keys, containers, hosts,
  rounds and file sizes.
- **The interpreter holds no Kubernetes credentials.** A separate controller process creates
  sessions. Its only inputs are the build ID, a size class and port-expose requests.

## Primitives

- **container**: the only thing that runs.
- **Bindings** are the only connections between containers:
  - network: `.url`/`.address`
  - files: `files` snapshots, consumed by `mount`
  - data: `input` in, `result` out

  Referencing a binding creates a dependency edge.
- **parallel**: repeats what it contains (`count` or `for_each`). It nests, and scoping
  replaces any key-matching rules.
- **host**: a logical isolation unit, with its own network and volume namespace.
- **snapshot**: an immutable, content-addressed directory tree, which can be queried with
  `glob`/`subdir`/`file`.
- **schema**: an HCL type or a JSON Schema, applied with `conform()` and `result_schema`.

## Execution model

### Streaming evaluation, not plan-then-apply

There is no separate apply step:

1. Evaluate the pipeline with cty. Values that depend on containers that haven't run are
   **unknown**, carrying their type and refinements.
2. Every container whose inputs are fully known, and whose dependencies have finished, is
   **ready**. Hand those to the scheduler.
3. When a container finishes, record its outcome in the **journal** and re-evaluate.
   `parallel` blocks whose `count`/`for_each` just became known expand; newly ready containers start.
4. Repeat until nothing is pending or running. Caps on rounds and total containers apply throughout.

Data-driven expansion (a count from a result, `for_each` over a `glob` of a build output)
is not a special case. It's a node whose inputs haven't arrived yet.

### The possibility plan

`crunk graph` runs step 1 without dispatching anything and prints the graph **template**:

- every container that could run
- counts as numbers or intervals (`test × [1..20]`)
- conditions (`only if publish fails`)
- result types
- worst-case bounds: containers, hosts, concurrent width and longest chain

It is not a promise of what will happen; it's the space of what could. Its uses:

- **Admission control:** upper bounds are checked against platform limits before anything is
  dispatched.
- **Early errors:** typos in result paths, type mismatches, and a `min` that can't be met.
- **Scheduling hints:** expected width.
- **Golden tests** for the examples.

Bounds come from cty refinements: JSON Schema `minItems`/`maxItems`/`minimum`/`maximum`,
`parallel` `min`/`max`, `glob` caps and platform caps. Bounds over-approximate, but they are
always sound.

### Journal, resume and cache

- **Journal.** Each completed container records:
  - its instance address
  - its spec hash
  - its exit status
  - its result digest
  - its snapshot digests
- **Resume** is replay. Evaluation is a pure function of the HCL, the variables and the journal,
  so a restarted engine re-evaluates and skips completed work. Held-alive containers are never
  journalled as complete; they are restarted.
- **Cross-build cache.** A container's identity hash covers:
  - its instance address
  - its canonical spec
  - its image digest
  - the digests of its mounted snapshots
  - its input JSON
  - the source commit

  A previous success with the same hash is reused, both its result and its snapshots.
  Held-alive containers are never cached.

### Hosts, sessions and scheduling

- **Logical hosts** map onto **sessions**. In production a session is a Sysbox pod with its own
  dockerd, created on demand by the controller; on a laptop it is the local daemon.
- **A host never spans sessions,** but several small hosts may share one, each with its own
  network. The root host maps to the build's pre-existing main session.
- **The scheduler sees the whole graph.** It maps a host to a session when the host's first
  container is ready.
- **The initial policy is deterministic and testable:**
  1. prefer a session with locality (images or snapshots already present);
  2. otherwise pack onto a session with spare capacity;
  3. otherwise create a session within the provider's limits;
  4. otherwise wait.

  Every decision is logged with its reason. Locality is an optimisation, never a semantic.
- **Idle sessions are destroyed** once the remaining graph can't use them, and everything is
  destroyed at build end, on failure or cancel.
- **Session loss:** affected containers are marked failed-retryable, their hosts are re-mapped,
  and they are rescheduled. The journal and cache keep this idempotent.

### Executor and snapshots

- **The Docker Engine API only.** The endpoint and mTLS material come from the session provider.
- **Fixed container settings:**
  - no privileged mode
  - all capabilities dropped
  - the logical host's network only
  - resource limits from the size class
  - no host bind mounts
- **Inputs and results:** `/run/input.json` is written before start. `/run/result.json` is
  read after exit and validated.
- **Snapshots** sit behind a `Snapshotter` interface (`Capture`, `Materialise`, listing, and a
  bounded file read). The first implementation streams tar through the Engine API. Archive
  extraction rejects absolute paths, `..`, links that escape the root and device files, and
  enforces size and count limits.

## Differences from Terraform

| | Terraform | crunk |
|---|---|---|
| Manages | Long-lived infrastructure, full CRUD | Short-lived immutable container runs; create only |
| Trust | Config trusted | Config untrusted, allowlisted language |
| Extensibility | Providers | None: containers only |
| Execution | Plan, then apply; graph fixed first | Streaming; expands as results arrive |
| Expansion | Must be known at plan time | Runtime values allowed |
| Plan | A promise | A possibility plan with bounds |
| Fan-out | `for_each` per resource, keys matched by hand | Nested `parallel` with lexical scope |
| Lifetimes | Until destroyed | Derived from bindings |
| Files | `file()` on host disk | Snapshot-only `file`/`glob`/`subdir` |
| State | Persistent, refreshed, diffed | Per-build journal; replay to resume; content-hash cache |
| Schemas | Type constraints | Type constraints plus JSON Schema at the container boundary |

## Code provenance and licensing

- **All code in this repository is original to crunk.** The project began as a fork of
  togomak; Phase 1 deleted all of togomak's code, and nothing from it is reused.
- **Libraries:** `github.com/hashicorp/hcl/v2` (MPL-2.0) and `github.com/zclconf/go-cty`
  (MIT) are used as dependencies. `github.com/alessio/shellescape` is MIT.
- **OpenTofu (MPL-2.0)** is the reference for semantics and the only permitted source of
  borrowed code, such as type-constraint handling or DAG walking. A borrowed file keeps its
  MPL-2.0 header and is listed here.
- **No code from Terraform 1.6 or later (BUSL-1.1).**

Borrowed files: none so far.

## Roadmap

1. **Done:** strip upstream; root-confined loader; static validation; malicious corpus.
2. **Language redesign in the validator:**
   - nested `parallel`/`host` blocks and scope resolution
   - `schema`
   - `[*]` references
   - new rejection cases
3. **Evaluator:**
   - `ContainerSpec` (closed, strict)
   - platform config
   - evaluation with unknowns and refinements
   - snapshot functions over listings
   - `conform`, YAML
   - `crunk graph` with golden tests, and admission control
4. **Sessions, scheduler and executor:**
   - `Session`/`SessionProvider`/`Snapshotter` interfaces
   - `StaticProvider`
   - tar snapshotter with safe extraction
   - derived lifecycles and ready probes
   - Docker integration tests behind a build tag
5. **Journal, resume and cross-build result store:** filesystem implementation first.
6. **Credential split:** a startup check that refuses Kubernetes credentials outside laptop
   mode; the controller client.
7. **Hardening:** expression evaluation limits; log streaming with instance prefixes and
   neutralised CI service messages; a pluggable status emitter.

Out of scope for now: the real Kubernetes controller, reflink and block snapshots
(only the interface), secrets, a UI, YAML front ends.
