variable "greeting" {
  type    = string
  default = "Hello"
}

container "greet" {
  image  = "docker.io/library/alpine:3.20"
  script = "echo ${shellescape("${var.greeting}, world!")}"
}
