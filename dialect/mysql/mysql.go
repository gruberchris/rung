// Package mysql provides the MySQL dialect, which also serves MariaDB.
//
// Importing it registers the dialect under "mysql" and "mariadb":
//
//	import _ "github.com/gruberchris/rung/dialect/mysql"
//
// MariaDB is served by this dialect because it speaks the same wire protocol
// and the same SQL. It is a fork rather than a version, though, so it is worth
// verifying separately in CI.
package mysql

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	driver "github.com/go-sql-driver/mysql"

	"github.com/gruberchris/rung"
)

// Dialect implements [rung.Dialect] for MySQL 8.0+ and MariaDB 10.5+.
type Dialect struct{}

func init() {
	rung.Register(Dialect{}, "mysql", "mariadb")
}

// Name returns the canonical driver name.
func (Dialect) Name() string { return "mysql" }

// MigrationsDir returns "mysql". MariaDB runs the same file set.
func (Dialect) MigrationsDir() string { return "mysql" }

// OpenForMigrations opens a handle permitting several statements per Exec.
//
// A migration file needs that and ordinary traffic must never have it: it is
// the difference between a driver that can reject an injected statement and one
// that cannot.
//
// ParseTime and a UTC location are forced rather than left to the caller's DSN.
// Without ParseTime the driver returns DATETIME columns as []byte and every
// scan into a time.Time fails; without Loc it reads them in the server's local
// zone, silently shifting every timestamp by the deployment's UTC offset. Both
// are configuration mistakes that surface far from their cause.
func (Dialect) OpenForMigrations(dsn string) (*sql.DB, error) {
	config, err := migrationConfig(dsn)
	if err != nil {
		return nil, err
	}
	return sql.Open("mysql", config.FormatDSN())
}

// migrationConfig parses a DSN and applies the settings a migration connection
// requires, overriding whatever the DSN asked for.
func migrationConfig(dsn string) (*driver.Config, error) {
	config, err := driver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing the MySQL DSN: %w", err)
	}

	config.ParseTime = true
	config.Loc = time.UTC
	config.MultiStatements = true

	return config, nil
}

// Rebind returns the query unchanged; MySQL already uses ? placeholders.
func (Dialect) Rebind(query string) string { return rung.RebindQuestion(query) }

// LedgerDDL creates the migration ledger, mirroring the PostgreSQL one row for
// row.
//
// DATETIME(6) rather than TIMESTAMP: MySQL's TIMESTAMP converts to and from the
// session time zone and tops out in 2038, and neither is wanted for a record of
// what has been applied to a schema.
func (Dialect) LedgerDDL() string {
	return `
		CREATE TABLE IF NOT EXISTS _migrations (
			id         INT AUTO_INCREMENT PRIMARY KEY,
			version    INT NOT NULL UNIQUE,
			name       VARCHAR(255) NOT NULL,
			applied_at DATETIME(6) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
}

// LedgerExistsQuery reports whether the ledger exists in the connected
// database.
//
// The TABLE_SCHEMA = DATABASE() predicate is load-bearing: without it the query
// reports a _migrations table belonging to some other database on the same
// server.
func (Dialect) LedgerExistsQuery() string {
	return `
		SELECT COUNT(*) > 0
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = '_migrations'`
}

// ListTablesQuery lists the tables in the connected database, for rung/reset.
func (Dialect) ListTablesQuery() string {
	return `
		SELECT TABLE_NAME
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'`
}

// QuoteIdentifier quotes a SQL identifier with backticks.
func (Dialect) QuoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// DropTablesStatement drops every named table.
//
// MySQL has no CASCADE, so foreign key checks are suspended for the duration
// rather than a safe drop order being computed. The statement relies on the
// multi-statement connection [Dialect.OpenForMigrations] returns.
func (Dialect) DropTablesStatement(quoted []string) string {
	return "SET FOREIGN_KEY_CHECKS = 0; " +
		"DROP TABLE IF EXISTS " + strings.Join(quoted, ", ") + "; " +
		"SET FOREIGN_KEY_CHECKS = 1"
}
