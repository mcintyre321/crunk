container "c" {
  image    = "artifactory.local/alpine:3"
  for_each = ["a", "b"]
}
