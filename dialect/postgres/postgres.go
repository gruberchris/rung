// Package postgres provides the PostgreSQL dialect.
//
// Importing it registers the dialect under "postgres", "postgresql" and "pgx":
//
//	import _ "github.com/gruberchris/rung/dialect/postgres"
//
// It targets PostgreSQL 12 and later and uses github.com/jackc/pgx/v5 through
// database/sql.
package postgres

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/gruberchris/rung"
)

// Dialect implements [rung.Dialect] for PostgreSQL.
type Dialect struct{}

func init() {
	rung.Register(Dialect{}, "postgres", "postgresql", "pgx")
}

// Name returns the canonical driver name.
func (Dialect) Name() string { return "postgres" }

// MigrationsDir returns "postgres".
func (Dialect) MigrationsDir() string { return "postgres" }

// OpenForMigrations opens a handle using the simple query protocol.
//
// That is a requirement, not a preference: migration files hold several
// statements, and pgx's default extended protocol permits exactly one per Exec.
// An application's own pool should keep the default.
func (Dialect) OpenForMigrations(dsn string) (*sql.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing the PostgreSQL DSN: %w", err)
	}
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	return stdlib.OpenDB(*config), nil
}

// Rebind converts ? placeholders to $1, $2, ….
func (Dialect) Rebind(query string) string { return rung.RebindDollar(query) }

// LedgerDDL creates the migration ledger.
func (Dialect) LedgerDDL() string {
	return `
		CREATE TABLE IF NOT EXISTS _migrations (
			id         SERIAL PRIMARY KEY,
			version    INTEGER NOT NULL UNIQUE,
			name       TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL
		)`
}

// LedgerExistsQuery reports whether the ledger exists in the current schema.
func (Dialect) LedgerExistsQuery() string {
	return `
		SELECT EXISTS (
			SELECT 1 FROM pg_tables
			WHERE schemaname = current_schema() AND tablename = '_migrations'
		)`
}

// ListTablesQuery lists the tables in the current schema, for rung/reset.
func (Dialect) ListTablesQuery() string {
	return `SELECT tablename FROM pg_tables WHERE schemaname = current_schema()`
}

// QuoteIdentifier quotes a SQL identifier.
//
// Table names come from the catalogue rather than from user input, but they are
// interpolated into a statement that cannot be parameterised, so they are
// quoted rather than trusted.
func (Dialect) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// DropTablesStatement drops every named table in one statement.
//
// CASCADE means foreign keys and dependency ordering do not matter, so there is
// no need to compute a safe drop order.
func (Dialect) DropTablesStatement(quoted []string) string {
	return "DROP TABLE IF EXISTS " + strings.Join(quoted, ", ") + " CASCADE"
}
