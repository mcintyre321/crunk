module "m" {
  source   = "./m"
  for_each = ["a", "b"]
}
