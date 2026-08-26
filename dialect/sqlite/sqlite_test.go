package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gruberchris/rung"
)

func TestDialectIsRegistered(t *testing.T) {
	for _, name := range []string{"sqlite", "sqlite3", "SQLite"} {
		t.Run(name, func(t *testing.T) {
			d, err := rung.For(name)
			if err != nil {
				t.Fatalf("rung.For(%q) error = %v", name, err)
			}
			if d.Name() != "sqlite" {
				t.Errorf("rung.For(%q).Name() = %q, want sqlite", name, d.Name())
			}
		})
	}
}

func TestMigrationsDir(t *testing.T) {
	if got := (Dialect{}).MigrationsDir(); got != "sqlite" {
		t.Errorf("MigrationsDir() = %q, want sqlite", got)
	}
}

// SQLite accepts an empty DSN and opens an anonymous temporary database, so a
// caller who forgot to configure a path would watch migrations apply
// successfully to a file that is discarded when the process exits.
func TestOpenForMigrationsRefusesAnEmptyDSN(t *testing.T) {
	for _, dsn := range []string{"", "   "} {
		db, err := (Dialect{}).OpenForMigrations(dsn)
		if !errors.Is(err, ErrNoPath) {
			t.Errorf("OpenForMigrations(%q) error = %v, want ErrNoPath", dsn, err)
		}
		if db != nil {
			_ = db.Close()
			t.Errorf("OpenForMigrations(%q) returned a handle as well as an error", dsn)
		}
	}
}

// SQLite takes a single writer. A pool converts what database/sql would have
// queued into SQLITE_BUSY raised at the driver, which arrives as a failed
// migration rather than a slow one.
func TestOpenForMigrationsCapsThePoolAtOneConnection(t *testing.T) {
	db := openTemp(t)
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", got)
	}
}

func TestRebindLeavesQuestionMarks(t *testing.T) {
	const query = "INSERT INTO t (a, b, c) VALUES (?, ?, ?)"
	if got := (Dialect{}).Rebind(query); got != query {
		t.Errorf("Rebind() = %q, want it unchanged", got)
	}
}

// The read-only path must not create the table it claims only to inspect.
func TestLedgerExistsQueryOnlyReads(t *testing.T) {
	query := (Dialect{}).LedgerExistsQuery()
	if strings.Contains(strings.ToUpper(query), "CREATE") {
		t.Errorf("LedgerExistsQuery() = %q, want a read", query)
	}

	db := openTemp(t)
	if exists := ledgerExists(t, db); exists {
		t.Error("a fresh database reports a ledger it does not have")
	}
	// And the check did not bring one into existence.
	if n := countTables(t, db); n != 0 {
		t.Errorf("checking for the ledger created %d tables", n)
	}

	if _, err := db.Exec((Dialect{}).LedgerDDL()); err != nil {
		t.Fatalf("LedgerDDL() error = %v", err)
	}
	if exists := ledgerExists(t, db); !exists {
		t.Error("the ledger exists but the check does not see it")
	}
}

// applied_at is declared DATETIME because the driver reads the declared type to
// decide how to convert. Declared TEXT, the value comes back as a string and
// scanning the ledger into a time.Time fails with "unsupported Scan" -- on the
// first status call, a long way from the CREATE TABLE that caused it.
func TestTheLedgerRoundTripsATime(t *testing.T) {
	db := openTemp(t)
	if _, err := db.Exec((Dialect{}).LedgerDDL()); err != nil {
		t.Fatal(err)
	}

	want := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.Exec(
		`INSERT INTO _migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		1, "init", want); err != nil {
		t.Fatalf("inserting into the ledger: %v", err)
	}

	var got time.Time
	if err := db.QueryRow(`SELECT applied_at FROM _migrations`).Scan(&got); err != nil {
		t.Fatalf("scanning applied_at into a time.Time: %v", err)
	}
	if !got.UTC().Equal(want) {
		t.Errorf("applied_at = %v, want %v", got.UTC(), want)
	}
}

func TestLedgerDDLIsIdempotent(t *testing.T) {
	db := openTemp(t)
	for i := range 2 {
		if _, err := db.Exec((Dialect{}).LedgerDDL()); err != nil {
			t.Fatalf("LedgerDDL() run %d: %v", i+1, err)
		}
	}
}

// SQLite keeps its own bookkeeping tables in sqlite_master alongside the
// user's, and they cannot be dropped: sqlite_sequence appears the moment any
// table uses AUTOINCREMENT, and dropping it fails with "table sqlite_sequence
// may not be dropped", taking the whole reset with it.
func TestListTablesQuerySkipsSQLitesOwnTables(t *testing.T) {
	db := openTemp(t)
	mustExec(t, db, `CREATE TABLE widgets (id INTEGER PRIMARY KEY AUTOINCREMENT)`)
	mustExec(t, db, `INSERT INTO widgets DEFAULT VALUES`)

	// The fixture is only meaningful if sqlite_sequence is actually there.
	var internal int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE name = 'sqlite_sequence'`).Scan(&internal); err != nil {
		t.Fatal(err)
	}
	if internal != 1 {
		t.Fatal("AUTOINCREMENT no longer creates sqlite_sequence; this test proves nothing")
	}

	for _, name := range listTables(t, db) {
		if strings.HasPrefix(name, "sqlite_") {
			t.Errorf("ListTablesQuery returned SQLite's own %q, which cannot be dropped", name)
		}
	}
}

func TestQuoteIdentifier(t *testing.T) {
	cases := map[string]string{
		"widgets":    `"widgets"`,
		"order":      `"order"`,
		`we"ird`:     `"we""ird"`,
		"with space": `"with space"`,
	}
	for in, want := range cases {
		if got := (Dialect{}).QuoteIdentifier(in); got != want {
			t.Errorf("QuoteIdentifier(%q) = %q, want %q", in, got, want)
		}
	}
}

// SQLite's DROP TABLE takes exactly one name; the comma-separated list the
// other dialects emit is a syntax error here.
func TestDropTablesStatementDropsOneTablePerStatement(t *testing.T) {
	stmt := (Dialect{}).DropTablesStatement([]string{`"a"`, `"b"`})

	if strings.Contains(stmt, `"a", "b"`) {
		t.Errorf("DropTablesStatement() = %q, want one DROP per table", stmt)
	}
	if n := strings.Count(strings.ToUpper(stmt), "DROP TABLE"); n != 2 {
		t.Errorf("DropTablesStatement() has %d DROPs, want 2: %q", n, stmt)
	}
	if !strings.Contains(stmt, "PRAGMA foreign_keys = OFF") {
		t.Errorf("DropTablesStatement() = %q, want foreign keys disabled first", stmt)
	}
}

// The statement has to survive a real parser, in the order sqlite_master
// returns tables rather than a computed safe one.
func TestDropTablesStatementRunsAgainstAForeignKeyGraph(t *testing.T) {
	db := openTemp(t)
	mustExec(t, db, `PRAGMA foreign_keys = ON`)
	mustExec(t, db, `CREATE TABLE parent (id INTEGER PRIMARY KEY)`)
	mustExec(t, db, `CREATE TABLE child (
		id INTEGER PRIMARY KEY,
		parent_id INTEGER NOT NULL REFERENCES parent(id))`)

	var quoted []string
	for _, name := range listTables(t, db) {
		quoted = append(quoted, (Dialect{}).QuoteIdentifier(name))
	}
	if len(quoted) != 2 {
		t.Fatalf("listed %d tables, want 2", len(quoted))
	}

	if _, err := db.Exec((Dialect{}).DropTablesStatement(quoted)); err != nil {
		t.Fatalf("DropTablesStatement() error = %v", err)
	}
	if n := countTables(t, db); n != 0 {
		t.Errorf("%d tables survived the drop", n)
	}
}

// --- helpers -----------------------------------------------------------------

func openTemp(t *testing.T) *sql.DB {
	t.Helper()
	db, err := (Dialect{}).OpenForMigrations(filepath.Join(t.TempDir(), "rung.db"))
	if err != nil {
		t.Fatalf("OpenForMigrations() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("connecting: %v", err)
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func ledgerExists(t *testing.T, db *sql.DB) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRow((Dialect{}).LedgerExistsQuery()).Scan(&exists); err != nil {
		t.Fatalf("LedgerExistsQuery(): %v", err)
	}
	return exists
}

func listTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query((Dialect{}).ListTablesQuery())
	if err != nil {
		t.Fatalf("ListTablesQuery(): %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func countTables(t *testing.T, db *sql.DB) int {
	t.Helper()
	return len(listTables(t, db))
}
