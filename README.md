# crunk

A trusted interpreter for untrusted CI pipelines, written in HCL.

Pipelines are built from one primitive, the **container**. Containers are placed on logical
**hosts**, repeated by **parallel** blocks, and connected by **bindings** (network ports,
file snapshots, JSON results). The interpreter evaluates the pipeline into a graph of
closed container specs and dispatches them to Docker endpoints. It never runs pipeline
code itself.

```hcl
container "build" {
  image  = "mcr.microsoft.com/dotnet/sdk:8.0"
  mount "/src" {
    from = container.source.files.tree
    mode = "copy"
  }
  script = "cd /src && dotnet build -c Release && cp -r tests/bin/Release/net8.0/. /out/tests/"
  files "tests" { path = "/out/tests" }
}

parallel "shard" {
  count = 4

  host "runner" {                       # one host per shard
    container "test" {
      image  = "mcr.microsoft.com/dotnet/sdk:8.0"
      mount "/tests" { from = container.build.files.tests }
      env    = { SHARD = tostring(shard.index) }
      script = "/tests/run-shard.sh"
    }
  }
}
```

## Status

Work in progress. Today crunk can load and statically validate a pipeline:

```
go run ./cmd/crunk validate --root path/to/repo
```

- [examples/](examples/): hello world and up
- [docs/DESIGN.md](docs/DESIGN.md): trust model, architecture, decisions
- [docs/LANGUAGE.md](docs/LANGUAGE.md): the complete language (anything not listed is rejected)

## Licence

See [LICENSE](LICENSE) and the provenance notes in [docs/DESIGN.md](docs/DESIGN.md#code-provenance-and-licensing).
