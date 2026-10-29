container "c" {
  image = "artifactory.local/alpine:3"
  args  = ["/bin/sh", "-c", "id"]
}
