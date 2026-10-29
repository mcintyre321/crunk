dimension "shard" { size = 8 }
host "worker" { for_each = dimension.shard }
locals { svc_host = null }
container "build" {
  image  = "artifactory.local/dotnet-sdk:8"
  mount "/src" { from = container.source.files.tree }
  script = "cd /src && ./build.sh && ./push-image.sh > /run/result.json"
  files "tests" { path = "/src/out/tests" }
}
container "api" {
  host  = local.svc_host
  image = "artifactory.local/app@${container.build.result.image_digest}"
  port "http" { container = 8080 }
  ready {
    http {
      port = "http"
      path = "/health"
    }
  }
}
container "test" {
  host   = host.worker
  image  = "artifactory.local/dotnet-sdk:8"
  mount "/tests" { from = container.build.files.tests }
  input  = { shard = each.index, total = dimension.shard.size, xs = [for x in range(3) : x * 2] }
  env    = { API_URL = container.api.url.http }
  script = "/tests/run.sh ${shellescape(join(" ", ["a"]))}"
}

variable "regions" {
  description = "number of regional stacks"
  type        = number
  default     = 2
}

dimension "region" { size = var.regions }

module "stack" {
  source   = "./modules/stack"
  for_each = dimension.region
  name     = "svc-${each.key}"
}

container "smoke" {
  image      = "artifactory.local/curl:8"
  depends_on = [container.test]
  when       = "always"
  timeout    = "5m"
  size       = "small"
  script     = "curl ${module.stack[0].container.web.url.http}"
}
