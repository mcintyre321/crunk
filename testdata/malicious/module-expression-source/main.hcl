variable "where" {}

module "m" {
  source = var.where
}
