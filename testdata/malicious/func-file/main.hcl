container "c" {
  image = "artifactory.local/alpine:3"
  env   = { PASSWD = file("/etc/passwd") }
}
