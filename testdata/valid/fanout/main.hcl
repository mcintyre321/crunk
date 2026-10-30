# Exercises every construct of the language the validator knows about.

variable "regions" {
  description = "number of regional stacks"
  type        = number
  default     = 2
}

schema "dotnet_info" {
  type = object({ sdk = object({ version = string }) })
}

locals {
  registry = "artifactory.local"
}

container "build" {
  image           = "${local.registry}/dotnet-sdk:8"
  max_concurrency = 2
  mount "/src" {
    from = container.source.files.tree
    mode = "copy"
  }

  script "restore" {
    run = "cd /src && dotnet restore"
  }

  script "compile" {
    depends_on = [script.restore]
    run        = "cd /src && dotnet build --no-restore -c Release > /dev/null"
  }

  script "image" {
    depends_on = [script.compile]
    run        = "cat /src/out/image-digest"
  }

  script "projects" {
    depends_on = [script.compile]
    run        = "ls /src/out/tests"
    output     = self.stdout.lines()
  }

  script "info" {
    run    = "dotnet --info --format json"
    output = self.stdout.jsondecode()
  }

  script "summary" {
    when  = "always"
    stdin = jsonencode({ projects = script.projects.output, sdk = script.info.output.sdk.version })
    run   = "jq ."
  }

  files "tests" { path = "/src/out/tests" }
}

container "version" {
  image  = "${local.registry}/git:2"
  mount "/src" { from = container.source.files.tree }
  script = "cd /src && git describe --tags"
  output = self.stdout.trimspace()
}

host "services" {
  container "db" {
    image = "${local.registry}/postgres:16"
    env   = { POSTGRES_PASSWORD = "ci" }
    port "pg" { container = 5432 }
    ready { tcp = "pg" }
  }
}

container "api" {
  image = "${local.registry}/app@${container.build.script.image.stdout.trimspace()}"
  env   = { DB = container.db.address.pg }
  port "http" { container = 8080 }
  ready {
    http {
      port = "http"
      path = "/health"
    }
  }
}

parallel "project" {
  for_each = container.build.script.projects.output
  min      = 1
  max      = 20

  locals {
    label = "tests-${project.key}"
  }

  host "runner" {
    container "test" {
      image = "${local.registry}/dotnet-sdk:8"
      mount "/tests" { from = container.build.files.tests }
      stdin = jsonencode({ project = project.value, index = project.index, version = container.version.output })
      env = {
        API_URL = container.api.url.http
        LABEL   = local.label
        ESCAPED = "$${HOME}"
      }
      script = "/tests/run.sh ${shellescape(project.value)}"
      files "results" { path = "/results" }
    }
  }

  parallel "retry" {
    count = 2
    container "flaky" {
      image  = "${local.registry}/alpine:3"
      env    = { N = "${project.key}-${retry.index}" }
      script = "true"
    }
  }
}

parallel "region" {
  count = var.regions
  module "stack" {
    source = "./modules/stack"
    name   = "svc-${region.index}"
  }
}

container "report" {
  image      = "${local.registry}/trx-merge:1"
  when       = "always"
  depends_on = [container.api, parallel.region[*].module.stack.container.web]
  mount "/results" { from = parallel.project[*].container.test.files.results }
  env = {
    FIRST = parallel.project["MyApp.Api.Tests"].container.test.files.results != null ? "y" : "n"
    WEB   = parallel.region[0].module.stack.container.web.url.http
    PICK  = parallel.region[length(var.regions) % 2].module.stack.container.web.url.http
    XS    = jsonencode([for i in range(3) : i * 2])
  }
  script = "trx-merge /results/*/*.trx"
  output = { exit = self.exit_code, warnings = [for l in self.stderr.lines() : l if l.startswith("warn")] }
}
