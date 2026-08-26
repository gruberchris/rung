# rung

[![CI](https://github.com/gruberchris/rung/actions/workflows/ci.yml/badge.svg)](https://github.com/gruberchris/rung/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gruberchris/rung.svg)](https://pkg.go.dev/github.com/gruberchris/rung)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Versioned SQL migrations for Go services, across PostgreSQL, MySQL and MariaDB.

Migrations are applied and rolled back one version at a time. Each applied
version is recorded in a ledger table inside the database itself, so the schema
carries its own history.

```go
m := rung.New(dialect, migrations.FS(), rung.WithReporter(render.NewConsole(nil)))

if err := m.Up(ctx, db, 0); err != nil {
    return err
}
```

---

## Design

`rung` treats schema migrations as a step in the deployment process rather than
as a runtime responsibility of the service.

Migrations are applied by a database account holding DDL privileges. The running
service connects using a separate account that does not hold them. Separating
the two means a defect in a request handler cannot alter the schema, and a
rolling restart or a crash loop cannot modify the database beneath a running
instance. A service using `rung` reports that its schema is out of date; it does
not bring it up to date itself.

The remainder of the design follows from that decision: a command-line interface
built for deployment pipelines, a confirmation prompt that fails rather than
assumes an answer when no terminal is attached, and a status format intended to
be read in a CI log.

## Features

- **Three databases, one file set per dialect.** PostgreSQL, MySQL and MariaDB,
  behind a `Dialect` interface that confines every database-specific difference
  to a single place.
- **Isolated driver dependencies.** The root package imports only the standard
  library. Each driver is confined to its dialect subpackage, so a
  PostgreSQL-only service never compiles the MySQL driver into its binary.
- **Embeddable migration files.** Migrations are read from an `io/fs.FS`, so
  `//go:embed` places them in the binary that expects them, leaving no directory
  to distribute and nothing that can drift out of step.
- **Transactional application.** Each migration commits together with its ledger
  row, so a file that fails partway through leaves neither a partially applied
  schema nor a ledger entry recording success.
- **Predictable without a terminal.** A command that requires confirmation fails
  when given neither `--force` nor an attached terminal, rather than assuming an
  answer.
- **A ready-made command-line interface.** `clicmd` provides `up`, `down`,
  `status` and `init` in roughly twenty lines of wiring.

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
`down` would create a state the tool cannot reverse. Any file that does not
parse as a migration name is ignored, so a `README.md` alongside them is
harmless.

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

The service does not apply migrations. It reports when its schema is out of
date:

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

The separation relies on two database roles:

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

The interface has six methods: `Name`, `MigrationsDir`, `OpenForMigrations`,
`Rebind`, `LedgerDDL` and `LedgerExistsQuery`. Implement the optional
`reset.Resetter` as well to support the `init` command.

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
