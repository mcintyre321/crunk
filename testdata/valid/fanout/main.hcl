# Exercises every construct of the language the validator knows about.

variable "regions" {
  description = "number of regional stacks"
  type        = number
  default     = 2
}

schema "build_result" {
  type = object({ image_digest = string, test_projects = list(string) })
}

locals {
  registry = "artifactory.local"
}

container "build" {
  image         = "${local.registry}/dotnet-sdk:8"
  result_schema = schema.build_result
  mount "/src" {
    from = container.source.files.tree
    mode = "copy"
  }
  script = "cd /src && ./build.sh"
  files "tests" { path = "/src/out/tests" }
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
  image = "${local.registry}/app@${container.build.result.image_digest}"
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
  for_each = container.build.result.test_projects
  min      = 1
  max      = 20

  locals {
    label = "tests-${project.key}"
  }

  host "runner" {
    container "test" {
      image = "${local.registry}/dotnet-sdk:8"
      mount "/tests" { from = container.build.files.tests }
      input = { project = project.value, index = project.index }
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
}
