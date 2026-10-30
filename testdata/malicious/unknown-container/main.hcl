container "c" {
  image = "artifactory.local/alpine:3"
  env   = { U = container.nope.url.http }
}
