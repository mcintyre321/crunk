parallel "p" {
  count = 2
  container "inner" {
    image = "artifactory.local/alpine:3"
    port "http" { container = 80 }
  }
}

container "outer" {
  image = "artifactory.local/alpine:3"
  env   = { U = container.inner.url.http }
}
