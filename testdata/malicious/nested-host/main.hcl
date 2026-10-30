host "a" {
  host "b" {
    container "c" { image = "artifactory.local/alpine:3" }
  }
}
