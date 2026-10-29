# Phase 0 inventory

This is a snapshot of the upstream togomak fork at `54334bd` (module `github.com/srevinsaju/togomak/v1`),
taken before any trust-model changes. Every capability below is something the
pipeline HCL, the repository contents, or the host environment can currently
reach, along with the planned fate of each in the target design.

Legend: **REMOVE** = delete the code; **CHANGE** = keep the concept but rewrite it under
the new trust model; **KEEP** = survives essentially as is.

## Execution model clarification (from the requester)

Logical **hosts** are created on demand as Sysbox-runtime Docker pods (sessions), and
each one runs its own internal dockerd. The engine moves working directories and
artefacts between pods using volume snapshots. One host may run many containers,
in sequence or concurrently. A host may stay alive (for example, running services) while
other hosts run batches of containers that connect back to it. The engine
must not assume one pod with one Docker context. Upstream togomak assumes exactly
that: a single local daemon reached through `DOCKER_HOST`/`FromEnv`, with host
bind mounts and host port publishing. Every Docker item below is marked CHANGE or
REMOVE for that reason.

## Guiding principle (from the requester)

Strip back to the bare bones needed to implement the design. That means a few core primitives
that are easy to reason about, plus a deterministic, safe expression language for transforms.
Avoid a feature explosion. Where an upstream feature is doubtful, it goes. Where the
task brief is more elaborate than the primitives need, I've flagged it in §10 rather than
building it.

## Baseline test status

`go test ./...` is **not green at this commit**:

- `internal/third-party/hashicorp/terraform/lang/funcs`: `TestFileExists` fails.
  It appears to depend on permission-denied behaviour, which doesn't hold when tests run as root.
- `tests/prompt_test.go`: `TestPrompt` panics because it needs a prebuilt
  `./togomak_coverage` binary and `/tmp/quit`.

Phase 1 deletes the code both tests cover (the filesystem functions and
interactive prompts), so the suite goes green with Phase 1. I won't patch them first.

---

## 1. Process execution on the interpreter host

| # | Where | What | Fate |
|---|---|---|---|
| 1.1 | `internal/ci/stage_run.go` `(*Stage).run`, `parseExecCommand`, `parseCommand` | A `stage` without a `container` block runs `exec.CommandContext(shell, -e -u -c script)` or `args[0] args[1:]` on the host. Its env is `os.Environ()` plus the stage env. | REMOVE |
| 1.2 | `stage_run.go` `parseExecCommand` | The `shell` attribute chooses any host binary (default `bash`). | REMOVE (fixed shell inside the image) |
| 1.3 | `stage_run.go` `parseCommand` | The `args` attribute execs argv directly, or becomes container `Cmd`. | REMOVE |
| 1.4 | `stage_run.go` `parseExecCommand` | `dir` sets the host working directory for the process. Relative values resolve against cwd and may escape it. | REMOVE |
| 1.5 | `internal/ci/stage_hooks.go` `BeforeRun`/`AfterRun`, `stage_run.go` `executePreHooks` | `pre_hook { stage {...} }` and `post_hook { stage {...} }` run full stages, including host stages, before and after each stage and on terminate. | REMOVE |
| 1.6 | `internal/ci/hooks.go`, `pipeline.go` `Resolve` (`togomak.pre`/`togomak.post`) | Top-level `pre {}` and `post {}` blocks run as stages. | REMOVE |
| 1.7 | `stage_run.go` `expandMacros` (macro with `source`) | Re-execs `os.Executable()` with `--child --dir <src> --parent <id>` as a host process, so a recursive interpreter runs with the stage's privileges. | REMOVE |
| 1.8 | `stage_run.go` `expandMacros` (macro with `files`) | Writes HCL-supplied file contents into `$TMP/<stage>/` on the host, then re-execs `os.Executable() --child --file …`. | REMOVE |
| 1.9 | `stage_run.go` `expandMacros` (macro with `stage`) | `mergo.Merge`s a macro's stage into the calling stage. No exec, but it is the macro mechanism. | REMOVE |
| 1.10 | `internal/blocks/data/terraform.go` `TfProvider.Attributes` | `data "tf"`: go-getter downloads `source`, then runs `terraform init -upgrade`, `plan`, `apply` and `show` on the host (`exec.LookPath("terraform")`). | REMOVE |
| 1.11 | `internal/blocks/data/git.go` `GitProvider.Attributes` | `data "git"`: `git.CloneWithArgs` from gitea shells out to the host `git` binary. | REMOVE |
| 1.12 | go-getter (`srevinsaju/go-getter` fork) used by `import_expand.go`, `module_run.go`, `terraform.go` | The `git::`/`hg::` getters shell out to host `git`/`hg`. | REMOVE (all go-getter use goes) |
| 1.13 | `internal/ci/conductor_hcl.go` `which()` | `exec.LookPath` probes the host PATH. It doesn't execute anything, but it leaks host state. | REMOVE |
| 1.14 | `internal/ci/stage_terminate.go` `Terminate`/`Kill` | Sends SIGTERM/SIGKILL to host child processes. | REMOVE (host-process half); the container half becomes CHANGE under the executor |
| 1.15 | `cmd/togomak` `--child`, `--parent`, `--parent-param`; `orchestra.ExpandGlobalParams` | Child-process mode exists only for macro recursion. | REMOVE |

No other `exec`/`os/exec` use exists outside the vendored funcs package.

## 2. Host filesystem access reachable from HCL or the repo

| # | Where | What | Fate |
|---|---|---|---|
| 2.1 | `conductor_hcl.go` → `funcs.MakeFileFunc` etc. | `file`, `filebase64`, `fileexists`, `fileset`, `filemd5`, `filesha1`, `filesha256`, `filesha512`, `filebase64sha256`, `filebase64sha512`. All of these read arbitrary host paths: absolute paths work and nothing confines them to the repo. | REMOVE |
| 2.2 | `conductor_hcl.go` | `abspath`, `pathexpand` (reads `$HOME`) | REMOVE |
| 2.3 | `conductor_hcl.go` | `basename`, `dirname`: pure string operations, but OS-separator dependent and path-oriented | REMOVE (not in the curated subset) |
| 2.4 | `funcs/filesystem.go` `MakeTemplateFileFunc` | `templatefile` is defined but not registered | REMOVE (delete the file) |
| 2.5 | `internal/ci/module_run.go` `(*Module).run` | `module.source` goes through go-getter with `Pwd = paths.Module`. Any local path works, including absolute paths, `..` and symlinks. The module is copied to `$TMP/modules/<id>` and parsed as a separate child pipeline. | CHANGE: relative paths only, `EvalSymlinks` + root confinement, no copy, parsed in-process at plan time |
| 2.6 | `internal/ci/import_expand.go`, `pipeline_import.go`, `pipeline_expand.go` | `import { source = … }` fetches any getter URL or local path and merges it into the pipeline. | REMOVE |
| 2.7 | `internal/ci/pipeline_read.go` `Read`/`ReadDir`/`ReadDirFromPath` | Parses every `*.hcl` in the pipeline directory (except `*.lock.hcl`), with no size limit. The `togomak.version` 1/2 switch lives here. | CHANGE: root-confined, size/count limits, strict schema |
| 2.8 | `cmd/togomak/utils.go` `autoDetectFilePath` | Walks up parent directories until it finds `togomak.hcl` or hits a mount point. | CHANGE: fixed root passed by flag |
| 2.9 | `internal/ci/conductor.go` `NewConductor`/`Chdir`, `NewProcess` | `os.Chdir` to the pipeline directory, plus a per-run `os.MkdirTemp`. | CHANGE: no chdir; interpreter scratch space is engine-owned |
| 2.10 | `stage_run.go` `processEnvironmentVariables` + `pipeline_outputs.go` `ExpandOutputs` | Each stage gets `TOGOMAK_OUTPUTS=$TMP/.togomak.env`. After each layer the host file is parsed with `envparse` into the `output.*` variable. | REMOVE (replaced by `/run/result.json`) |
| 2.11 | `blocks/data/git.go` | `destination` attribute: clones into **any host path** chosen by the HCL. `files` attribute: reads files from the clone into HCL. | REMOVE |
| 2.12 | `blocks/data/terraform.go` | Writes the workdir and plan files under `$TMP`. | REMOVE |
| 2.13 | `stage_run.go` `executeDocker` | Bind-mounts host `cmd.Dir` at `/workspace` (unless `skip_workspace`), plus arbitrary `volume { source, destination }` host bind mounts. | REMOVE (mounts come only from `Snapshotter.Materialise`) |
| 2.14 | `conductor_hcl.go` variables | `owd`, `cwd`, `path.{root,cwd,module}`, `pipeline.{path,tempDir}` expose host paths to HCL. | REMOVE |
| 2.15 | `internal/cache/clean.go` (`togomak cache clean`) | Recursively `RemoveAll`s `.togomak/pipelines/tmp` under a directory tree. | REMOVE (superseded by the result store) |
| 2.16 | `internal/orchestra/init.go` (`togomak init`) | Writes `togomak.hcl`, after a prompt asking whether to overwrite. | REMOVE |
| 2.17 | `internal/orchestra/format.go` (`togomak fmt`) | Reads and rewrites `*.hcl` files in place. Parse-only; it never evaluates anything. | REMOVE (see Q1) |
| 2.18 | `internal/logging/logging.go` `file` sink | `--logging.local.file.path` appends JSON logs to a path. Set by the operator flag, not by HCL. | REMOVE with the other sinks; logging becomes the pluggable emitter in Phase 7 |

## 3. Network access

| # | Where | What | Fate |
|---|---|---|---|
| 3.1 | go-getter in `module_run.go`, `import_expand.go`, `terraform.go` | Fetches http(s), git, hg, s3 and gcs sources, plus `.netrc` credentials (`go-netrc`). | REMOVE |
| 3.2 | `blocks/data/git.go` | Clones any URL, with HCL-supplied username/password/SSH key and `ca_bundle`. | REMOVE |
| 3.3 | `blocks/data/terraform.go` | `terraform init` downloads providers and modules; `apply` can reach anything. | REMOVE |
| 3.4 | `stage_run.go` `executeDocker` | `ImagePull` of any HCL-chosen image from any registry. | CHANGE: registry allowlist, resolved to a digest, pulled via the session's Engine API |
| 3.5 | `stage_run.go`, `stage_terminate.go` | `dockerClient.NewClientWithOpts(FromEnv)` connects to `DOCKER_HOST`, using `DOCKER_CERT_PATH`/`DOCKER_TLS_VERIFY` from the environment. | CHANGE: endpoint + mTLS from `SessionProvider` only; one client per session |
| 3.6 | `internal/logging/google_cloud.go` | Google Cloud Logging sink, enabled by `--logging.remote.google-cloud`. It ships every log line to GCP. | REMOVE |
| 3.7 | `stage_containers_hcl.go` `Nat` + `executeDocker` `PortBindings` | Publishes container ports on the **interpreter host's** interfaces (`host:port:container_port`). | REMOVE; replaced by named `port` blocks with no host publishing. Cross-host reachability goes through `SessionProvider.ExposePort` |

Telemetry and update checks: **none found**. Nothing phones home except the
GCP sink (3.6), and that is opt-in.

## 4. Host environment reads

| # | Where | What | Fate |
|---|---|---|---|
| 4.1 | `conductor_hcl.go` `env(key, default)` | HCL function reading any host env var | REMOVE |
| 4.2 | `blocks/data/env.go` | `data "env"` reads any host env var | REMOVE |
| 4.3 | `ci/variable_run.go` `resolveVar` | `TOGOMAK_VAR_<id>` env var overrides variables | REMOVE (values come from `-var` only; see §9) |
| 4.4 | `blocks/data/prompt.go` | Env var `TOGOMAK__prompt__<id>` | REMOVE |
| 4.5 | `orchestra.ExpandGlobalParams` | `TOGOMAK__param__*` env becomes `param.*` (macro child mode) | REMOVE |
| 4.6 | `stage_run.go` `run` | `cmd.Env = append(os.Environ(), …)` passes the full host env to host stages | REMOVE |
| 4.7 | `cmd/togomak/main.go` | `TOGOMAK_ARGS` env is appended to the positional stage filter | REMOVE |
| 4.8 | `cmd/togomak/main.go`, `conductor_hcl.go` | `$USER` → `hostuser`, `os.Hostname()` → `hostname` exposed to HCL | REMOVE |
| 4.9 | `cmd/togomak/main.go` CLI `EnvVars` | `TOGOMAK_UNATTENDED`, `CI`, `TOGOMAK_CI`, `TOGOMAK_JSON_LOG`, `TOGOMAK_DRY_RUN`, `TOGOMAK_LOGGING_*`, `GOOGLE_CLOUD_PROJECT` | REMOVE (these are operator inputs, not HCL, but the flags themselves go; config comes from the platform config file) |
| 4.10 | Docker `FromEnv` (3.5) | `DOCKER_HOST`, `DOCKER_CERT_PATH`, `DOCKER_TLS_VERIFY`, `DOCKER_API_VERSION` | CHANGE (provider-supplied) |
| 4.11 | `blocks/data/git.go` `init()` | `os.UserHomeDir()` → gitea `setting.Git.HomePath` (package init side effect) | REMOVE |
| 4.12 | `conductor_hcl.go` `togomak.{boot_time,boot_time_unix,pipeline_id}`, `pipeline.id` | Wall clock and random UUID exposed to HCL (nondeterministic) | REMOVE |
| 4.13 | `cmd/togomak/main.go` `--unattended` default | `isatty(stdin)` | REMOVE |

## 5. Interactive prompts / TTY

| # | Where | What | Fate |
|---|---|---|---|
| 5.1 | `blocks/data/prompt.go` | `data "prompt"` uses `survey.AskOne` | REMOVE |
| 5.2 | `ci/variable_run.go` `resolveVar` | A variable with no value and no default prompts on stdin | REMOVE (becomes a plan-time error) |
| 5.3 | `ui/prompt.go` `PromptYesNo` (used by `togomak init`) | Overwrite confirmation | REMOVE |
| 5.4 | `stage_schema.go` `container.stdin` | Attaches the container's stdin (`AttachStdin`/`OpenStdin`/`StdinOnce`) | REMOVE |
| 5.5 | `ci/pipeline_run.go` | On failure in attended mode, waits for daemons: "hit Ctrl+C to force stop them" | REMOVE (teardown is unconditional) |
| 5.6 | `ci/handler.go` `Interrupt`/`Kill` | SIGINT/SIGTERM handling, with "press CTRL+C again" double-interrupt force quit | CHANGE: signal → cancel context → scheduler tears everything down |
| 5.7 | `ui/progress.go`, `ui/passive.go`, `ui/getter.go` | Terminal progress bars for pulls and downloads | REMOVE (getter bar); others CHANGE or REMOVE with the log emitter |
| 5.8 | `tests/prompt_test.go` | pty/expect tests for prompts and interrupt | REMOVE |

## 6. User-controlled values reaching Docker container config

Everything in `stage_run.go` `executeDocker`. Today `HostConfig` sets only `Binds` and
`PortBindings`, so privilege, capability, network, user and device settings are not
expressible. They silently take **Docker defaults**: root user, the default capability set,
the default bridge network and no resource limits.

| Docker field | Source today | Fate |
|---|---|---|
| `Image` | `container.image` (any string, any registry, tag only) | CHANGE: allowlisted registry, resolved to `@sha256:` before dispatch |
| `HostConfig.Binds` | `/workspace` ← host `cmd.Dir` (from `dir`/cwd) unless `skip_workspace`; `volume { source, destination }` | REMOVE; only `Snapshotter.Materialise` volumes |
| `ExposedPorts`, `HostConfig.PortBindings` | `port { host, container_port, port }` → `nat.ParsePortSpecs` (host interface binds) | REMOVE; named `port "<n>" { container = N }`, no host publishing |
| `Entrypoint` | `container.entrypoint` (list) | REMOVE (not expressible) |
| `Cmd` | `args`, or `[shell, -e, -u, -c, script]` | CHANGE: fixed shell + `script`, or the image default when `script` is omitted |
| `Env` | `env { name, value }` blocks + `TOGOMAK_OUTPUTS=<host path>` + `TOGOMAK__param__*` | CHANGE: `env = { K = V }` map only. Nothing from the host, no injected host paths |
| `WorkingDir` | hard-coded `/workspace` | CHANGE: platform-defined |
| `Tty` | hard-coded `true` | CHANGE: platform-defined (`false`, so stdout and stderr can be demultiplexed) |
| `AttachStdin`/`OpenStdin`/`StdinOnce` | `container.stdin` | REMOVE |
| `User` | not set (commented out) → image default, usually root | CHANGE: platform sets it |
| `Privileged`, `CapAdd/CapDrop`, `Devices`, `NetworkMode`, `SecurityOpt`, resources | not set → Docker defaults | CHANGE: platform sets no-privileged, drop ALL caps, the logical host's network only, limits from the size class |
| Removal | `ContainerRemove(RemoveVolumes)` after logs reach EOF; no wait on exit code, so **container exit status is ignored** | CHANGE: wait, collect exit code, read `/run/result.json`, capture `files`, then remove |
| Client | `FromEnv` | CHANGE (§3.5) |

---

## 7. Block types → target language

Target top-level blocks: `container`, `host`, `dimension`, `locals`, `variable`, `module`.

| Upstream block / attribute | Fate | Notes |
|---|---|---|
| `togomak { version, behavior { disable_concurrency } }` | REMOVE | Version is implicit. Concurrency belongs to the scheduler and platform config |
| `stage "<id>"` | **FOLD → `container "<name>"`** | |
|  `name` | REMOVE | The label is the name |
|  `depends_on` | KEEP | Only `container.x` references allowed |
|  `if` | REMOVE | Not in the target. `when` replaces the status-based part |
|  `for_each` (any map/set) | CHANGE | `for_each = dimension.<name>` only; `each.index`/`each.key` replace `each.key`/`each.value` |
|  `script` | KEEP | Run by a fixed shell inside the image |
|  `shell`, `args`, `dir` | REMOVE | §1 |
|  `env { name, value }` (repeatable block) | CHANGE | `env = { … }` map attribute |
|  `container { image, volume, skip_workspace, port, entrypoint, stdin }` | CHANGE | `image` moves to top-level `container.image`; `port` becomes the named `port` block; the rest are REMOVED |
|  `use { macro, parameters, chdir }` | REMOVE | Macros are gone |
|  `daemon { enabled, timeout, lifecycle { stop_when_complete } }` | REMOVE | Lifecycle is derived from `port` bindings. `ready {}` and `timeout` cover the rest |
|  `retry { enabled, attempts, exponential_backoff, min_backoff, max_backoff }` | REMOVE | Only engine-level retry on session loss |
|  `lifecycle { phase, timeout }` | REMOVE `phase`; `timeout` → `container.timeout` | Maven-style phases and `-q` lifecycle queries go away |
|  `pre_hook`, `post_hook` | REMOVE | Use separate containers with `when = "always"` / `"on_failure"` |
|  *(new)* `input`, `host`, `size`, `when`, `allow_failure`, `port`, `ready`, `files`, `mount` | ADD | Phase 2+ |
| `pre {}`, `post {}` | REMOVE | |
| `data "env" / "prompt" / "git" / "tf"` | REMOVE | All four execute or read the host |
| `provider "<name>" { url }` | REMOVE | Decoded but unused |
| `macro "<id>" { source, files, parameters, stage }` | REMOVE | |
| `import { source }` | REMOVE | Local reuse goes through `module` |
| `module "<id>" { source, name, depends_on, if, for_each, lifecycle, retry, daemon, <inputs> }` | CHANGE | Keep `source` (relative, root-confined) and input attributes. REMOVE `name`, `if`, `lifecycle`, `retry`, `daemon`. `for_each`/`depends_on`: see §9. It is no longer a runnable child pipeline with its own conductor; it is inlined into the graph at plan time |
| `variable "<id>" { description, depends_on, value, default, type }` | CHANGE | Keep `description`, `default`, `type`. REMOVE `depends_on`, `value`, env/prompt sourcing |
| `locals { … }` | KEEP | |
| `output` (`ci/output.go` constant; the `output.*` variable from `TOGOMAK_OUTPUTS`) | REMOVE | Replaced by `container.x.result.*` |
| *(new)* `host "<n>" { for_each }`, `dimension "<n>" { size }` | ADD | Phase 2/3 |

### Expression roots (variables in the eval context)

| Root | Fate |
|---|---|
| `true`, `false`, `null` | KEEP |
| `var.*` | KEEP |
| `local.*` | KEEP |
| `each.key` / `each.value` | CHANGE → `each.index`, `each.key` |
| `stage.*` | CHANGE → `container.<n>[.url/.address/.files/.result]` |
| `module.*` | CHANGE (module outputs, if any; see §9) |
| `this.{name,id,hook,status,output}` | REMOVE (hook plumbing, including the previous stage's captured stdout) |
| `param.*` | REMOVE |
| `data.*`, `macro.*`, `output.*` | REMOVE |
| `togomak.*`, `pipeline.*`, `path.*`, `owd`, `cwd`, `hostname`, `hostuser`, `ansi.*` | REMOVE |
| `lifecycle.*`, `if`, `id`, `name` (only in `-q` query evaluation) | REMOVE |
| *(new)* `host.*`, `dimension.<n>.size`, `container.source.files.tree` | ADD |

## 8. Registered HCL functions (`internal/ci/conductor_hcl.go`)

Upstream registers about 120 functions. The target keeps a **small** pure, deterministic set:
enough for string building, collection shaping, numbers and JSON. Everything else goes,
including functions that are pure but niche. More can be added later if a real
pipeline needs them; removing them later is harder.

| Function | Fate | Reason |
|---|---|---|
| `format`, `join`, `split`, `replace`, `lower`, `upper`, `trim`, `trimprefix`, `trimsuffix`, `trimspace`, `substr`, `startswith`, `endswith`, `strcontains`, `regex`, `regexall`, `chomp`, `indent` | KEEP | string (RE2 runs in linear time) |
| `shellescape` | KEEP | quoting values into `script` safely |
| `length`, `keys`, `values`, `lookup`, `merge`, `concat`, `contains`, `distinct`, `element`, `flatten`, `range`, `slice`, `sort`, `zipmap`, `compact`, `coalesce`, `index`, `one`, `alltrue`, `anytrue` | KEEP | collection (`range` is bounded by the Phase 7 evaluation limits) |
| `abs`, `ceil`, `floor`, `max`, `min`, `parseint`, `sum` | KEEP | numeric |
| `tostring`, `tonumber`, `tobool`, `tolist`, `tomap`, `toset`, `can`, `try` | KEEP | conversion / error handling |
| `jsonencode`, `jsondecode`, `base64encode`, `base64decode`, `urlencode`, `sha256` | KEEP | encoding / hashing |
| `chunklist`, `coalescelist`, `matchkeys`, `transpose`, `reverse`, `setintersection`, `setproduct`, `setsubtract`, `setunion`, `strrev`, `title`, `formatlist`, `log`, `pow`, `signum` | REMOVE | pure but niche; trimmed to keep the vocabulary small |
| `yamlencode`, `yamldecode`, `csvdecode`, `base64gzip`, `textencodebase64`, `textdecodebase64`, `md5`, `sha1`, `sha512`, `base64sha256`, `base64sha512`, `uuidv5`, `stripansi` | REMOVE | pure but niche (drops the `go-cty-yaml` dependency) |
| `list`, `map` | REMOVE | Deprecated Terraform stubs that always error |
| `file`, `filebase64`, `fileexists`, `fileset`, `filemd5`, `filesha1`, `filesha256`, `filesha512`, `filebase64sha256`, `filebase64sha512` | REMOVE | filesystem |
| `abspath`, `pathexpand`, `basename`, `dirname` | REMOVE | filesystem/path |
| `env`, `which` | REMOVE | host env / PATH |
| `timestamp`, `timeadd`, `timecmp`, `formatdate` | REMOVE | time |
| `uuid`, `bcrypt` | REMOVE | random |
| `rsadecrypt`, `sensitive`, `nonsensitive` | REMOVE | Secret handling is out of scope |
| `ansifmt` | REMOVE | terminal decoration |
| *(defined, unregistered)* `templatefile`, static timestamp, `type` | REMOVE | The vendored `third-party/.../funcs` package is replaced by a thin registration of cty `stdlib` plus the few Terraform funcs still needed (`index`, `length`, `lookup`, `coalesce`, `one`, `sum`, `alltrue`, `anytrue`, `startswith`, `endswith`, `strcontains`, `replace`, `sha256`, base64, `urlencode`, `parseint`) |

## 9. CLI surface and other machinery

| Item | Fate |
|---|---|
| `togomak run` (default action) | CHANGE → `run` with `--platform-config`, `--root`, `--var` |
| `--dry-run` / `-n` | REMOVE → replaced by `plan` (Phase 5) |
| positional stage filters (`+stage`, `^stage`), `internal/rules`, `internal/filter` | REMOVE |
| `-q/--query` HCL expressions over lifecycle phases (`ci/query_model.go`, `query_slice.go`) | REMOVE |
| `--disable-concurrency`, `--unattended`, `--ci`, `--child`, `--parent`, `--parent-param` | REMOVE |
| `-f/--file`, `-C/--dir` | CHANGE → a single `--root` (repo snapshot); pipeline file at a fixed name under it |
| `--json`, `-v` | KEEP |
| `togomak list` | REMOVE (superseded by `plan`) |
| `togomak init`, `togomak cache clean` | REMOVE |
| `togomak fmt` | REMOVE (not needed to implement the design; see Q1) |
| Retry machinery (`*_retry.go`, `BlockRunWithRetries`) | REMOVE |
| Daemon tracker / watchdog (`handler.go` `Daemons`, `daemon.go`) | REMOVE → derived lifecycle in the scheduler |
| `depgraph` layered topo sort (`graph.go`, `pipeline_run.go`) | CHANGE → expanded instance graph + list scheduler |
| `Conductor` child conductors per module | REMOVE → single evaluator, modules inlined |
| Dependencies to drop once the code is gone: `code.gitea.io/gitea`, `hashicorp/terraform-exec`, `srevinsaju/go-getter`, `AlecAivazis/survey`, `Netflix/go-expect`, `creack/pty`, `hinshun/vt10x`, `cloud.google.com/go/logging`, `rifflock/lfshook`, `imdario/mergo`, `moby/sys/mountinfo`, `spf13/afero`, `mitchellh/go-homedir`, `hashicorp/go-envparse`, `kendru/darwin/go/depgraph` | REMOVE |

### Open questions (each has a bare-bones default I'll use unless told otherwise)

1. **`togomak fmt`**: remove. Developers can use any HCL formatter; it's not part of the interpreter.
2. **Variable values**: `--var k=v` on the trusted CLI, plus `default`. Nothing from env, prompts or the platform config.
3. **`module`**: a module is a namespace. It takes input attributes (its `variable`s) and contributes
   its blocks to the one graph, which you reference as `module.x.container.y.…`. There is no
   `for_each`, no `depends_on` and no `output` block.
4. **Static `if` on containers**: drop it. Conditional work can be expressed as `for_each` over a dimension of size 0 or 1.
5. **Migration errors**: removed constructs get a specific message where it's cheap. For example,
   "`stage` was replaced by `container`" and "`volume` is not supported". Anything else gets the
   generic "unsupported block/argument" error.

## 10. Parts of the task brief I'd simplify or question

The brief is a bot's reading of a conversation, so I've taken the design intent from it
rather than every detail. These are the places where it seems to add more machinery than the
primitives need. I'd start with the simple version and grow only if a real pipeline needs more.

| Brief item | Concern | Proposed simplification |
|---|---|---|
| Scheduler cost model: estimated wait from historical durations vs creation latency + cold start | A lot of policy before anything runs. It needs a duration history store, and it's hard to test meaningfully | v1 policy: pack onto an existing session with capacity → otherwise create one if under `Limits()` → otherwise wait. Locality as a tie-break. It stays deterministic and logs each decision. Keep `Policy` as an interface so a cost model can come later |
| `ControllerProvider` + fake controller in this repo | A protocol with no real server yet | Define the `SessionProvider` interface and implement only `StaticProvider` (laptop/tests). Add the controller client when the controller exists |
| HTTP `ResultStore` stub "for Artifactory" | A placeholder file with no behaviour | Filesystem store only; the interface is the seam |
| Snapshotter `mode = copy` vs `readonly` | Two mount modes from day one | Keep both only if cheap with tar streaming (copy = writable volume, readonly = `:ro` volume). Both are natural with tar, so keep |
| `size` classes + `timeout` cap + `max hosts` + `max containers` + `max dimension size` + rounds limit + module depth + file size/count limits | Many knobs | Keep them: they're the platform's guard rails against untrusted HCL, not user features. One platform config struct |
| TeamCity service messages | A specific CI integration | Keep, behind a one-method emitter interface. Plain-text prefixing is the default |
| Deferred expansion (Phase 6) | Big feature | Needed for example E. Implement as "evaluate with unknowns → run what's ready → re-evaluate", with a round cap and no special cases |
| `depends_on` | Redundant with bindings in most cases | Keep. It's tiny and the brief explicitly wants ordering without data |
| `when` + `allow_failure` | Two status knobs | Keep. They replace hooks and pre/post blocks entirely |
