container "c" {
  image = "artifactory.local/alpine:3"
  env   = { TOKEN = env("GITHUB_TOKEN") }
}
