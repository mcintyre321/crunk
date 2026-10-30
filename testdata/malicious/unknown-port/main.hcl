container "a" {
  image = "artifactory.local/alpine:3"
  port "http" { container = 80 }
}

container "b" {
  image = "artifactory.local/alpine:3"
  env   = { U = container.a.url.grpc }
}
