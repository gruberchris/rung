# rung

[![CI](https://github.com/gruberchris/rung/actions/workflows/ci.yml/badge.svg)](https://github.com/gruberchris/rung/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gruberchris/rung.svg)](https://pkg.go.dev/github.com/gruberchris/rung)
[![Go Report Card](https://goreportcard.com/badge/github.com/gruberchris/rung)](https://goreportcard.com/report/github.com/gruberchris/rung)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Versioned SQL migrations for Go services, across PostgreSQL, MySQL and MariaDB.

Migrations go up and down a ladder one rung at a time.

```go
m := rung.New(dialect, migrations.FS(), rung.WithReporter(render.NewConsole(nil)))

if err := m.Up(ctx, db, 0); err != nil {
    return err
}
```

---

## Why another one

`rung` is opinionated about one thing: **migrations are a deploy step, not
something a server does to itself.**

The account that applies them holds DDL privileges. The account the running
service connects as does not. Keeping the two apart means a bug in a request
handler cannot alter your schema, and a rolling restart or a crash loop cannot
change the database underneath a running instance. A server using `rung` reports
that it is behind; it does not fix it.

Everything else follows from that: a CLI built for a deploy pipeline, a
confirmation prompt that **refuses rather than guesses** when there is no
terminal, and a status format meant to be read in a CI log.

## Features

- **Three databases, one file set per dialect.** PostgreSQL, MySQL and MariaDB,
  behind a `Dialect` seam so nothing else in your code branches on which is in
  use.
- **No driver you did not ask for.** The root package imports only the standard
  library. Drivers come with the dialect subpackages, so a PostgreSQL-only
  service never compiles the MySQL driver into its binary.
- **Embeddable.** Migrations are read from an `io/fs.FS`, so `//go:embed` puts
  them in the binary that expects them. No directory to copy, nothing that can
  drift.
- **Transactional.** Each migration and its ledger row commit together, so a
  file that fails halfway leaves no half-built schema and no ledger row claiming
  success.
- **Safe in CI.** No terminal and no `--force` is an error, not a silent "no".
- **A CLI you can drop in.** `clicmd` gives you `up`, `down`, `status` and
  `init` in about twenty lines.

## Install

As a library:

```sh
go get github.com/gruberchris/rung
```

As a standalone tool:

```sh
go install github.com/gruberchris/rung/cmd/rung@latest
```

Or download a binary from [Releases](https://github.com/gruberchris/rung/releases).

## Migration files

Files are named `NNNNNN_name.up.sql` and `NNNNNN_name.down.sql`, in a directory
per dialect:

```
migrations/
├── embed.go
├── postgres/
│   ├── 000001_initial_schema.up.sql
│   └── 000001_initial_schema.down.sql
└── mysql/                      # MariaDB runs this set too
    ├── 000001_initial_schema.up.sql
    └── 000001_initial_schema.down.sql
```

A version with only one half is skipped: applying an `up` with no matching
`down` would create a state the tool cannot reverse. Anything that does not
parse as a migration name is ignored, so a `README.md` alongside them is fine.

Applied versions are recorded in a `_migrations` table:

| column | | |
|---|---|---|
| `id` | serial | |
| `version` | integer | unique |
| `name` | text | |
| `applied_at` | timestamp | UTC |

## Using it

### Embed the files

```go
// migrations/embed.go
package migrations

import (
    "embed"
    "io/fs"
)

//go:embed postgres mysql
var files embed.FS

func FS() fs.FS { return files }
```

### Build a migrate binary

```go
package main

import (
    "os"

    "github.com/gruberchris/rung/clicmd"

    _ "github.com/gruberchris/rung/dialect/mysql"    // mysql, mariadb
    _ "github.com/gruberchris/rung/dialect/postgres" // postgres, postgresql, pgx

    "github.com/example/service/migrations"
)

func main() {
    cmd := clicmd.New(clicmd.Options{
        Use:       "migrate",
        Short:     "example database migration tool",
        EnvPrefix: "EXAMPLE",
        FS:        migrations.FS(),
    })
    if err := cmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

The driver and DSN are read from `--driver` / `--database-uri`, then
`EXAMPLE_DATABASE_DRIVER` / `EXAMPLE_DATABASE_URI`, then an optional
`Options.Config` callback.

### Report drift from your server

The server does not migrate. It says so when it is behind:

```go
pending, err := m.Pending(ctx, db)
switch {
case err != nil:
    log.Warn("could not determine migration status", "error", err)
case len(pending) > 0:
    log.Warn("database schema is out of date; run `migrate up` before serving traffic",
        "pending_versions", pending)
default:
    log.Info("database schema is up to date")
}
```

`Pending` never creates the ledger table, so a read stays a read.

### Use the engine directly

```go
d, err := rung.For(cfg.Driver)          // "postgres", "mysql", "mariadb", …
db, err := d.OpenForMigrations(cfg.DSN) // multi-statement; never for serving traffic
m := rung.New(d, migrations.FS())

err = m.Up(ctx, db, 0)   // 0 applies everything; N stops at version N
err = m.Down(ctx, db)    // rolls back the newest applied migration
```

## The CLI

```
migrate up                  # Apply every pending migration
migrate up --target 5       # Apply up to and including version 5
migrate up --dry-run        # Report what would be applied
migrate down                # Roll back the most recent migration
migrate down --steps 2      # Roll back the last two
migrate down --all          # Roll back everything
migrate status              # Show applied and pending migrations
migrate status --format json
migrate init                # Drop every table and re-apply (destructive)
```

```console
$ migrate up --force
📊 Checking migration status...
Migration Status:
================
Version 1: initial_schema [Applied] 2026-01-02 01:46:55
Version 2: create_indexes [Pending]

⚡ Running migrations...
  applying      000002_create_indexes
  applied       000002_create_indexes
✅ All migrations completed successfully!
```

Colour is dropped automatically when the output is not a terminal, so a CI log
gets the same text without escape sequences. `--no-emoji` and `--no-color` force
it.

## In a deploy pipeline

```bash
# --force is REQUIRED. Without a terminal there is nobody to answer the
# confirmation, and rung exits non-zero rather than assuming an answer.
#
# Note the DSN: this runs as the owner role, which holds DDL privileges. The
# service connects as a different account that does not.
docker run --rm \
  --network "${DOCKER_NETWORK}" \
  -e EXAMPLE_DATABASE_DRIVER=postgres \
  -e EXAMPLE_DATABASE_URI="${MIGRATION_DATABASE_URI}" \
  --entrypoint /app/bin/migrate \
  "${IMAGE}" \
  up --force
```

Two database roles, deliberately:

```sql
-- Applies migrations. Never used by the service.
CREATE ROLE example_owner LOGIN PASSWORD '…';

-- Used by the service. Can read and write rows; cannot create, alter or drop.
CREATE ROLE example_app LOGIN PASSWORD '…';
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO example_app;
```

## Supported databases

| Database | Driver names | Directory | Driver |
|---|---|---|---|
| PostgreSQL 12+ | `postgres`, `postgresql`, `pgx` | `postgres/` | `jackc/pgx/v5` |
| MySQL 8.0+ | `mysql` | `mysql/` | `go-sql-driver/mysql` |
| MariaDB 10.5+ | `mariadb` | `mysql/` | `go-sql-driver/mysql` |

MariaDB is served by the MySQL dialect — same wire protocol, same SQL — but it
is a fork rather than a version, so CI verifies it separately.

### Adding a dialect

Implement `rung.Dialect` and register it:

```go
func init() { rung.Register(Dialect{}, "sqlite", "sqlite3") }
```

Six methods: `Name`, `MigrationsDir`, `OpenForMigrations`, `Rebind`,
`LedgerDDL`, `LedgerExistsQuery`. Implement the optional `reset.Resetter` as
well if you want `init` to work.

## A note on MySQL and DDL

MySQL commits implicitly on most DDL, so a migration that fails partway through
several `CREATE TABLE`s cannot be fully rolled back there. The ledger row is
still correct — it is only written on success — so a failed migration is
re-attempted rather than skipped. Write MySQL migrations with `IF NOT EXISTS`.

PostgreSQL has transactional DDL and does not have this caveat.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Issues and pull requests are welcome.

## License

[MIT](LICENSE) © Christopher Gruber
