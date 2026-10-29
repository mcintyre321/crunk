container "c" {
  image  = "artifactory.local/alpine:3"
  script = "cat ${path.root}/secret"
}
