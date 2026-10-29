variable "name" {
  type = string
}

container "db" {
  image = "artifactory.local/postgres:16"
  env   = { POSTGRES_PASSWORD = "ci" }
  port "pg" { container = 5432 }
  ready { tcp = "pg" }
  files "dump" { path = "/var/lib/postgresql/dump" }
}

container "web" {
  image = "artifactory.local/web:1"
  env   = { DB = container.db.address.pg, NAME = var.name }
  port "http" { container = 8080 }
  ready {
    http {
      port = "http"
    }
  }
  mount "/data" {
    from = container.db.files.dump
    mode = "copy"
  }
  allow_failure = true
}
