# Build a .NET solution once, then run the tests in 4 shards on 4 hosts,
# all talking to one shared API, and collect every shard's results.

container "build" {
  image = "mcr.microsoft.com/dotnet/sdk:8.0"
  size  = "large"
  mount "/src" {
    from = container.source.files.tree
    mode = "copy"
  }
  script = <<-EOT
    cd /src
    dotnet build MyApp.sln -c Release
    mkdir -p /out/tests
    cp -r tests/MyApp.Tests/bin/Release/net8.0/. /out/tests/
  EOT
  files "tests" { path = "/out/tests" }
}

container "api" { # outside the parallel block: one, shared
  image = "registry.local/myapp-api:latest"
  port "http" { container = 8080 }
  ready {
    http {
      port = "http"
      path = "/health"
    }
  }
}

parallel "shard" {
  count = 4

  host "runner" {
    container "test" {
      image   = "mcr.microsoft.com/dotnet/sdk:8.0"
      timeout = "20m"
      mount "/tests" { from = container.build.files.tests }
      env = {
        SHARD   = tostring(shard.index)
        TOTAL   = "4"
        API_URL = container.api.url.http
      }
      script = <<-EOT
        cd /tests
        dotnet test MyApp.Tests.dll \
          --filter "$(./shard-filter.sh "$SHARD" "$TOTAL")" \
          --logger trx --results-directory /results
      EOT
      files "results" { path = "/results" }
    }
  }
}

container "report" {
  image  = "registry.local/trx-merge:1"
  when   = "always"
  mount "/results" { from = parallel.shard[*].container.test.files.results }
  script = "trx-merge /results/*/*.trx"
}
