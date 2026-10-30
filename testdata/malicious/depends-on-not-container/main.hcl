container "c" {
  image      = "artifactory.local/alpine:3"
  depends_on = ["build"]
}
