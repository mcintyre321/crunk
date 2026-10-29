container "c" {
  image = "artifactory.local/alpine:3"
  post_hook {
    stage {
      script = "id"
    }
  }
}
