container "c" {
  image = "artifactory.local/alpine:3"
  env   = { V = script.a.stdout }
  script "a" {
    run = "echo hi"
  }
}
