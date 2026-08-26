// Package sqlite provides the SQLite dialect.
//
// Importing it registers the dialect under "sqlite" and "sqlite3":
//
//	import _ "github.com/gruberchris/rung/dialect/sqlite"
//
// SQLite is not a server, which changes what a "DSN" means and what a
// migration has to defend against. There is no network, no account, and no
// other database on the same host to be confused with: a DSN is a file path,
// or ":memory:", or a file: URI. What it does have that the others do not is a
// single-writer model, which is why this dialect caps the pool at one
// connection rather than leaving it to the caller.
//
// The driver is modernc.org/sqlite, which is a pure-Go translation of SQLite
// rather than a cgo binding. rung publishes a cross-compiled CLI; a cgo driver
// would make that need a C toolchain per target, and would break the static
// binary. It is also what keeps `CGO_ENABLED=0 go build` working for anyone
// embedding rung.
package sqlite

import (
	"database/sql"
	"errors"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/gruberchris/rung"
)

// Dialect implements [rung.Dialect] for SQLite 3.
type Dialect struct{}

func init() {
	rung.Register(Dialect{}, "sqlite", "sqlite3")
}

// Name returns the canonical driver name.
func (Dialect) Name() string { return "sqlite" }

// MigrationsDir returns "sqlite".
func (Dialect) MigrationsDir() string { return "sqlite" }

// ErrNoPath reports an empty DSN.
var ErrNoPath = errors.New("sqlite: the DSN is empty; want a file path, \":memory:\", or a file: URI")

// OpenForMigrations opens a handle for applying migrations.
//
// Unlike the server dialects, nothing has to be relaxed to permit several
// statements per Exec: this driver already allows them, and SQLite has no
// injection-mitigating single-statement mode to turn off.
//
// The pool is capped at one connection. SQLite takes a single writer, so a
// pool does not buy concurrency -- it converts what database/sql would have
// queued into SQLITE_BUSY errors raised at the driver, arriving as a failed
// migration rather than a slow one. Capping it also makes any PRAGMA the
// caller set in the DSN apply to every statement rung runs, instead of to
// whichever connection happened to serve it.
//
// An empty DSN is refused rather than passed through. SQLite would accept it
// and open an anonymous temporary database, so a caller who forgot to
// configure a path would watch migrations apply successfully to a file that is
// discarded when the process exits.
func (Dialect) OpenForMigrations(dsn string) (*sql.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, ErrNoPath
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// Rebind returns the query unchanged; SQLite already uses ? placeholders.
func (Dialect) Rebind(query string) string { return rung.RebindQuestion(query) }

// LedgerDDL creates the migration ledger, mirroring the other dialects row for
// row.
//
// applied_at is declared DATETIME even though SQLite has no date type and
// stores the value as text. The declared type is not decoration here: the
// driver reads it to decide how to convert, and a column declared TEXT comes
// back as a string, so scanning the ledger into a time.Time fails outright
// with "unsupported Scan". That surfaces on the first status call rather than
// at migration time, a long way from the CREATE TABLE that caused it.
//
// INTEGER PRIMARY KEY, not AUTOINCREMENT: the column is already a rowid alias
// and assigns itself. AUTOINCREMENT only adds the monotonicity guarantee
// nothing here needs, plus an internal sqlite_sequence table.
func (Dialect) LedgerDDL() string {
	return `
		CREATE TABLE IF NOT EXISTS _migrations (
			id         INTEGER PRIMARY KEY,
			version    INTEGER NOT NULL UNIQUE,
			name       TEXT NOT NULL,
			applied_at DATETIME NOT NULL
		)`
}

// LedgerExistsQuery reports whether the ledger exists.
//
// sqlite_master is per-database by construction, so this needs no equivalent
// of the TABLE_SCHEMA predicate the server dialects carry: a SQLite connection
// cannot see another database's tables unless one was deliberately attached.
func (Dialect) LedgerExistsQuery() string {
	return `
		SELECT COUNT(*) > 0
		FROM sqlite_master
		WHERE type = 'table' AND name = '_migrations'`
}

// ListTablesQuery lists the tables in the database, for rung/reset.
//
// The sqlite_% exclusion is load-bearing rather than tidy. SQLite keeps its
// own bookkeeping tables in sqlite_master alongside the user's -- sqlite_
// sequence appears the moment any table uses AUTOINCREMENT -- and they cannot
// be dropped: the attempt fails with "table sqlite_sequence may not be
// dropped" and takes the whole reset with it.
func (Dialect) ListTablesQuery() string {
	return `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`
}

// QuoteIdentifier quotes a SQL identifier with double quotes.
func (Dialect) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// DropTablesStatement drops every named table.
//
// One DROP per table, because SQLite's DROP TABLE takes exactly one name --
// the comma-separated list the other dialects use is a syntax error here. The
// statement relies on the multi-statement Exec this driver already permits.
//
// Foreign keys are disabled first, so the tables can be dropped in the
// arbitrary order sqlite_master returns them rather than a computed safe one.
// They are deliberately not re-enabled afterwards: OFF is SQLite's own
// default, so leaving it there returns the connection to the state it would
// have had, whereas switching it ON would impose a setting the caller never
// asked for.
func (Dialect) DropTablesStatement(quoted []string) string {
	var b strings.Builder
	b.WriteString("PRAGMA foreign_keys = OFF;")
	for _, name := range quoted {
		b.WriteString(" DROP TABLE IF EXISTS ")
		b.WriteString(name)
		b.WriteString(";")
	}
	return b.String()
}
