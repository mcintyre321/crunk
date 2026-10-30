parallel "p" {
  count = 2
  container "c" {
    image = "artifactory.local/alpine:3"
    port "http" { container = 80 }
  }
}

container "d" {
  image = "artifactory.local/alpine:3"
  env   = { U = parallel.p.container.c.url.http }
}
