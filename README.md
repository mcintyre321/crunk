# crunk

A trusted interpreter for untrusted CI pipelines, written in HCL.

Pipelines are built from one primitive, the **container**. Containers are placed on logical
**hosts**, fanned out over **dimensions**, and connected by **bindings** (network ports,
file snapshots, JSON results). The interpreter evaluates the pipeline into a graph of
closed container specs and dispatches them to Docker endpoints. It never runs pipeline
code itself.

```hcl
dimension "shard" { size = 8 }
host "worker" { for_each = dimension.shard }

container "build" {
  image  = "artifactory.local/dotnet-sdk:8"
  mount "/src" { from = container.source.files.tree }
  script = "cd /src && ./build.sh"
  files "tests" { path = "/src/out/tests" }
}

container "test" {
  host   = host.worker
  image  = "artifactory.local/dotnet-sdk:8"
  mount "/tests" { from = container.build.files.tests }
  input  = { shard = each.index, total = dimension.shard.size }
  script = "/tests/run.sh"
}
```

## Status

Work in progress. The design is locked (see below); today crunk can load and statically validate a pipeline in the earlier draft syntax:

```
go run ./cmd/crunk validate --root path/to/repo
```

- [examples/](examples/): hello world and up
- [docs/DESIGN.md](docs/DESIGN.md): trust model, architecture, decisions
- [docs/LANGUAGE.md](docs/LANGUAGE.md): the complete language (anything not listed is rejected)

## Licence

See [LICENSE](LICENSE) and the provenance notes in [docs/DESIGN.md](docs/DESIGN.md#code-provenance-and-licensing).
