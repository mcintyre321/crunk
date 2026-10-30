container "a" { image = "artifactory.local/alpine:3" }

container "b" {
  image = "artifactory.local/alpine:3"
  env   = { V = container.a.result.version }
}
