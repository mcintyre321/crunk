container "a" { image = "artifactory.local/alpine:3" }

container "b" {
  image = "artifactory.local/alpine:3"
  mount "/x" { from = container.a.result.path }
}
