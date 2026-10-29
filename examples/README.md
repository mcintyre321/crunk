# Examples

Small pipelines, each building on the last. Each one is a pipeline root:

```
go run ./cmd/crunk validate --root examples/01-hello-world
```

| Example | Shows |
|---|---|
| `01-hello-world` | One container |
| `02-build-and-test` | File and result bindings between containers; the built-in source tree |
| `03-service` | A long-running service, kept alive by a network binding and gated by a ready probe |
| `04-fan-out` | Dimensions, expanded hosts, `each.index` |
| `05-module` | A local module, expanded over a dimension |

crunk can only validate pipelines at the moment; running them arrives in Phase 3.
