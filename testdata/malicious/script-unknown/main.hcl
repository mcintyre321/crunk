container "c" {
  image = "artifactory.local/alpine:3"
  script "a" {
    stdin = script.nope.stdout
    run   = "cat"
  }
}
