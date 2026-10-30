container "c" {
  image = "artifactory.local/alpine:3"
  script "a" {
    stdin = script.c.stdout
    run   = "cat"
  }
  script "b" {
    depends_on = [script.a]
    run        = "true"
  }
  script "c" {
    depends_on = [script.b]
    run        = "true"
  }
}
