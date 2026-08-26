// Package rung applies versioned SQL migrations to PostgreSQL, MySQL and MariaDB.
//
// A migration is a pair of files, NNNNNN_name.up.sql and NNNNNN_name.down.sql.
// A ledger table named _migrations records which versions have been applied.
// Files are read from an [io/fs.FS], so they can be embedded in the binary that
// expects them:
//
//	//go:embed postgres mysql
//	var files embed.FS
//
// # The model
//
// Migrations are a deploy step, not something a server does to itself. The
// account that applies them holds DDL privileges; the account the running
// service connects as does not. Keeping the two apart means a bug in a request
// handler cannot alter the schema, and a rolling restart or a crash loop cannot
// change the database underneath a running instance.
//
// A server should therefore report drift rather than fix it:
//
//	pending, err := m.Pending(ctx, db)
//	if len(pending) > 0 {
//	    log.Warn("database schema is out of date; run `migrate up` before serving traffic",
//	        "pending_versions", pending)
//	}
//
// # Dialects
//
// What differs between databases lives behind [Dialect] and nowhere else.
// Importing a dialect package registers it:
//
//	import (
//	    _ "github.com/gruberchris/rung/dialect/mysql"    // mysql, mariadb
//	    _ "github.com/gruberchris/rung/dialect/postgres" // postgres, postgresql, pgx
//	)
//
//	d, err := rung.For(cfg.Driver)
//
// This package itself imports only the standard library; the database drivers
// come with the dialect packages, so a PostgreSQL-only program never compiles
// the MySQL driver into its binary.
//
// # Reporting
//
// A [Migrator] narrates through a [Reporter] rather than through a logger, so
// the caller decides whether a migration starting is a line of prose, a
// structured log record, or nothing at all. See the render package for ready
// implementations, and clicmd for a complete cobra command tree.
//
// # Transactions
//
// Each migration runs in one transaction together with its ledger row, so a
// file that fails halfway leaves no partially-built schema and no ledger row
// claiming it succeeded. MySQL is the caveat worth knowing: it commits
// implicitly on most DDL, so a migration that fails partway through several
// CREATE TABLEs cannot be fully rolled back there. The ledger row is still
// correct, because it is only written on success, so a failed migration is
// re-attempted rather than skipped -- which is why migration files are best
// written with IF NOT EXISTS.
package rung
