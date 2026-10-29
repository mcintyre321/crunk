# Reuse a local module, once per key of a dimension.

dimension "lang" { size = 2 }

module "greeter" {
  source   = "./modules/greeter"
  for_each = dimension.lang
  greeting = ["Hello", "Hallo"][each.index]
}
