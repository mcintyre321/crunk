parallel "p" {
  count    = 2
  for_each = ["a", "b"]
  container "c" { image = "artifactory.local/alpine:3" }
}
