# Several scripts inside one container, as a small DAG.
#
# Scripts share the container's filesystem. Data flows between them on
# stdout/stdin, parsed with functions; `output` parses once so everyone else
# reads a value. Methods chain: x.f(y) is f(x, y).

container "build" {
  image = "docker.io/library/golang:1.22"
  mount "/src" {
    from = container.source.files.tree
    mode = "copy"
  }

  script "version" {
    run    = "cd /src && git describe --tags --always"
    output = self.stdout.trimspace()
  }

  script "packages" {
    run    = "cd /src && go list ./..."
    output = self.stdout.lines()
  }

  script "vet" { # independent of version
    run = "cd /src && go vet ./..."
  }

  script "compile" {
    depends_on = [script.vet]
    stdin      = script.version.output
    run        = "cd /src && go build -ldflags \"-X main.version=$(cat)\" -o /out/app . >&2"
  }

  script "summary" {
    when  = "always"
    stdin = jsonencode({
      version  = script.version.output
      packages = script.packages.output.length()
      built    = script.compile.exit_code == 0
    })
    run = "cat"
  }

  files "bin" { path = "/out" }
}

parallel "package" {
  for_each = container.build.script.packages.output.slice(0, 3) # the first three packages

  container "test" {
    image  = "docker.io/library/golang:1.22"
    mount "/src" { from = container.source.files.tree }
    env    = { PKG = package.value }
    script = "cd /src && go test \"$PKG\""
  }
}
