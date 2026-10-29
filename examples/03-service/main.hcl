# A service that other containers talk to over the network.
#
# `db` declares a port and a readiness probe. Because `test` references
# container.db.address.pg, the db is held alive until `test` finishes, and `test`
# starts only once the probe passes. There is no "service" keyword: the lifecycle
# follows from the graph.

container "db" {
  image = "docker.io/library/postgres:16"
  env   = { POSTGRES_PASSWORD = "ci" }
  port "pg" { container = 5432 }
  ready { tcp = "pg" }
}

container "test" {
  image = "docker.io/library/postgres:16"
  env = {
    PGHOST     = container.db.address.pg
    PGPASSWORD = "ci"
  }
  script = "psql -U postgres -c 'select 1'"
}
