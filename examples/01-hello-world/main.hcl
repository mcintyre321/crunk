# The smallest pipeline: one container that prints a greeting.
container "hello" {
  image  = "docker.io/library/alpine:3.20"
  script = "echo 'Hello, world!'"
}
