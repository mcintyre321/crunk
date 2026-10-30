container "c" {
  image = "artifactory.local/alpine:3"
  script "a" {
    stdin = self.stdout
    run   = "cat"
  }
}
