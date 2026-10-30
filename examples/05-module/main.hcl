# Reuse a local module, once per iteration of a parallel block.

parallel "lang" {
  for_each = ["Hello", "Hallo"]

  module "greeter" {
    source   = "./modules/greeter"
    greeting = lang.value
  }
}
