container "c" {
  image = "artifactory.local/alpine:3"
  pre_hook {
    stage {
      script = "id"
    }
  }
}
