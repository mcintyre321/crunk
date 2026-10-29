container "c" {
  image = "artifactory.local/alpine:3"
  port "p" { container = 80 }
  ready {
    tcp = "p"
    http {
      port = "p"
    }
  }
}
