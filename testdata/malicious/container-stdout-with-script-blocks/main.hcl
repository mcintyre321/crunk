container "a" {
  image = "artifactory.local/alpine:3"
  script "x" {
    run = "echo hi"
  }
}

container "b" {
  image = "artifactory.local/alpine:3"
  env   = { V = container.a.stdout }
}
