container "c" {
  image  = "artifactory.local/alpine:3"
  script = "echo hi"
  script "a" {
    run = "echo hi"
  }
}
