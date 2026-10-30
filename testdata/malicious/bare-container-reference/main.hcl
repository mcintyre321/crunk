container "a" { image = "artifactory.local/alpine:3" }

container "b" {
  image = "artifactory.local/alpine:3"
  stdin = jsonencode({ a = container.a })
}
