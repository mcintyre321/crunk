# hclsyntax (fork)

This is a copy of `github.com/hashicorp/hcl/v2/hclsyntax` at **v2.17.0**, licensed MPL-2.0
by HashiCorp, Inc. Files keep their original headers. Files changed by crunk say so in
their header.

## Changes

- **Method-call syntax.** `x.f(a, b)` parses as the call `f(x, a, b)`, so any function can be
  chained onto a value: `script.build.stdout.jsondecode().conform(schema.x)`. The parser marks
  such calls with `FunctionCallExpr.Method`, and `Range()` covers the receiver. Evaluation,
  walking and the function allowlist see an ordinary function call. (`parser.go`, `expression.go`)

To update: copy the upstream package over this directory and re-apply the changes above
(search for "crunk").
