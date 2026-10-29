locals {
  x = templatefile("/etc/shadow", {})
}
