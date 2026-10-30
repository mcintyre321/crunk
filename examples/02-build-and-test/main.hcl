# Two containers connected by bindings.
#
# `build` mounts the source tree, writes an output directory (`files`) and prints
# its version on stdout. `test` mounts that output as a private snapshot and gets
# the version through env. Referencing container.build creates the dependency;
# no depends_on is needed.

container "build" {
  image = "docker.io/library/golang:1.22"
  mount "/src" { from = container.source.files.tree }
  script = <<-EOT
    cd /src
    go build -o /out/hello ./cmd/hello >&2
    /out/hello --version
  EOT
  output = self.stdout.trimspace()
  files "bin" { path = "/out" }
}

container "test" {
  image = "docker.io/library/alpine:3.20"
  mount "/bin-under-test" { from = container.build.files.bin }
  env   = { VERSION = container.build.output }
  script = <<-EOT
    /bin-under-test/hello
    echo "tested version $${VERSION}"
  EOT
}
