container "c" {
  image = "artifactory.local/alpine:3"
  volume {
    source      = "/var/run/docker.sock"
    destination = "/var/run/docker.sock"
  }
}
