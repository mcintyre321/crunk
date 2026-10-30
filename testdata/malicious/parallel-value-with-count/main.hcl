parallel "p" {
  count = 2
  container "c" {
    image = "artifactory.local/alpine:3"
    env   = { V = p.value }
  }
}
