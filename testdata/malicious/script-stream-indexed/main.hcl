container "c" {
  image = "artifactory.local/alpine:3"
  script "a" {
    run = "echo hi"
  }
  script "b" {
    stdin = script.a.stdout.first
    run   = "cat"
  }
}
