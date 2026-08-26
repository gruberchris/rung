// Integration tests run against real databases.
//
// Each engine is skipped unless its DSN is exported, so `go test ./...` on a
// workstation with no databases still passes. CI exports all three:
//
//	RUNG_TEST_POSTGRES_DSN="postgres://rung:rung@127.0.0.1:5432/rung?sslmode=disable"
//	RUNG_TEST_MYSQL_DSN="rung:rung@tcp(127.0.0.1:3306)/rung"
//	RUNG_TEST_MARIADB_DSN="rung:rung@tcp(127.0.0.1:3307)/rung"
//
// MariaDB is exercised separately from MySQL because it is a fork rather than a
// version: it runs the same file set through the same dialect, and the point of
// the third target is to notice the day that stops being true.
//
// SQLite is the exception and needs no DSN: it has no server to point at, so
// it runs against a file in a temporary directory and is never skipped. That
// makes `go test ./...` on a workstation with no databases exercise the Up,
// Down, Statuses and reset paths against a real engine rather than none.
//
// These tests drop every table in the database they connect to. Point them at a
// throwaway one.
package rung_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gruberchris/rung"
	"github.com/gruberchris/rung/reset"

	_ "github.com/gruberchris/rung/dialect/mysql"
	_ "github.com/gruberchris/rung/dialect/postgres"
	_ "github.com/gruberchris/rung/dialect/sqlite"
)

type engine struct {
	name   string
	env    string
	driver string
	// local supplies a DSN when env is unset, for an engine that needs no
	// server. Nil means skip, which is the right answer for anything with a
	// service container behind it.
	local func(t *testing.T) string
}

var engines = []engine{
	{name: "postgres", env: "RUNG_TEST_POSTGRES_DSN", driver: "postgres"},
	{name: "mysql", env: "RUNG_TEST_MYSQL_DSN", driver: "mysql"},
	{name: "mariadb", env: "RUNG_TEST_MARIADB_DSN", driver: "mariadb"},
	{name: "sqlite", env: "RUNG_TEST_SQLITE_DSN", driver: "sqlite", local: tempDatabase},
}

// tempDatabase gives SQLite a throwaway file, so it needs no service container
// and no environment variable to be exercised.
func tempDatabase(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "rung.db")
}

// migrations carries an equivalent schema for each dialect. The two file sets
// deliberately share version numbers and names: a version has to mean the same
// thing whichever database it is applied to.
var migrations = fstest.MapFS{
	"postgres/000001_widgets.up.sql": file(
		`CREATE TABLE IF NOT EXISTS widgets (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`),
	"postgres/000001_widgets.down.sql": file(`DROP TABLE IF EXISTS widgets`),
	"postgres/000002_gadgets.up.sql": file(
		`CREATE TABLE IF NOT EXISTS gadgets (id INTEGER PRIMARY KEY, label TEXT NOT NULL)`),
	"postgres/000002_gadgets.down.sql": file(`DROP TABLE IF EXISTS gadgets`),
	"postgres/000003_sprockets.up.sql": file(
		`CREATE TABLE IF NOT EXISTS sprockets (id INTEGER PRIMARY KEY)`),
	"postgres/000003_sprockets.down.sql": file(`DROP TABLE IF EXISTS sprockets`),

	"mysql/000001_widgets.up.sql": file(
		`CREATE TABLE IF NOT EXISTS widgets (id INT PRIMARY KEY, name VARCHAR(255) NOT NULL)`),
	"mysql/000001_widgets.down.sql": file(`DROP TABLE IF EXISTS widgets`),
	"mysql/000002_gadgets.up.sql": file(
		`CREATE TABLE IF NOT EXISTS gadgets (id INT PRIMARY KEY, label VARCHAR(255) NOT NULL)`),
	"mysql/000002_gadgets.down.sql": file(`DROP TABLE IF EXISTS gadgets`),
	"mysql/000003_sprockets.up.sql": file(
		`CREATE TABLE IF NOT EXISTS sprockets (id INT PRIMARY KEY)`),
	"mysql/000003_sprockets.down.sql": file(`DROP TABLE IF EXISTS sprockets`),

	"sqlite/000001_widgets.up.sql": file(
		`CREATE TABLE IF NOT EXISTS widgets (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`),
	"sqlite/000001_widgets.down.sql": file(`DROP TABLE IF EXISTS widgets`),
	"sqlite/000002_gadgets.up.sql": file(
		`CREATE TABLE IF NOT EXISTS gadgets (id INTEGER PRIMARY KEY, label TEXT NOT NULL)`),
	"sqlite/000002_gadgets.down.sql": file(`DROP TABLE IF EXISTS gadgets`),
	"sqlite/000003_sprockets.up.sql": file(
		`CREATE TABLE IF NOT EXISTS sprockets (id INTEGER PRIMARY KEY)`),
	"sqlite/000003_sprockets.down.sql": file(`DROP TABLE IF EXISTS sprockets`),
}

// broken carries a migration whose SQL cannot succeed.
var broken = fstest.MapFS{
	"postgres/000001_broken.up.sql":   file(`CREATE TABLE definitely not valid sql`),
	"postgres/000001_broken.down.sql": file(`SELECT 1`),
	"mysql/000001_broken.up.sql":      file(`CREATE TABLE definitely not valid sql`),
	"mysql/000001_broken.down.sql":    file(`SELECT 1`),
	"sqlite/000001_broken.up.sql":     file(`CREATE TABLE definitely not valid sql`),
	"sqlite/000001_broken.down.sql":   file(`SELECT 1`),
}

func file(body string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(body)} }

// connect opens the engine's database, or skips the test when it is not
// configured.
func (e engine) connect(t *testing.T) (rung.Dialect, *sql.DB) {
	t.Helper()

	dsn := e.dsn(t)

	dialect, err := rung.For(e.driver)
	if err != nil {
		t.Fatalf("rung.For(%q) error = %v", e.driver, err)
	}

	db, err := dialect.OpenForMigrations(dsn)
	if err != nil {
		t.Fatalf("OpenForMigrations() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("connecting to %s: %v", e.name, err)
	}

	// Each test starts from an empty database, and leaves one behind.
	clean := func() {
		if _, err := reset.DropAll(context.Background(), db, dialect); err != nil {
			t.Fatalf("reset.DropAll() error = %v", err)
		}
	}
	clean()
	t.Cleanup(clean)

	return dialect, db
}

// dsn resolves an engine's DSN, skipping the engine when it needs a server
// that has not been provided.
//
// The environment always wins, so SQLite can still be pointed at a particular
// file when someone wants to inspect the result afterwards.
func (e engine) dsn(t *testing.T) string {
	t.Helper()
	if value := os.Getenv(e.env); value != "" {
		return value
	}
	if e.local == nil {
		t.Skipf("%s is not set; skipping", e.env)
	}
	return e.local(t)
}

func eachEngine(t *testing.T, run func(t *testing.T, e engine)) {
	t.Helper()
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) { run(t, e) })
	}
}

func TestUpAppliesEveryMigration(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dialect, db := e.connect(t)
		m := rung.New(dialect, migrations)
		ctx := context.Background()

		if err := m.Up(ctx, db, 0); err != nil {
			t.Fatalf("Up() error = %v", err)
		}

		pending, err := m.Pending(ctx, db)
		if err != nil {
			t.Fatalf("Pending() error = %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("Pending() = %v, want none", pending)
		}

		statuses, err := m.Statuses(ctx, db)
		if err != nil {
			t.Fatalf("Statuses() error = %v", err)
		}
		if len(statuses) != 3 {
			t.Fatalf("Statuses() returned %d rows, want 3", len(statuses))
		}
		for _, status := range statuses {
			if !status.Applied {
				t.Errorf("version %d reported as pending after Up()", status.Version)
			}
			if status.AppliedAt.IsZero() {
				t.Errorf("version %d has no applied_at timestamp", status.Version)
			}
		}
	})
}

func TestUpIsIdempotent(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dialect, db := e.connect(t)
		m := rung.New(dialect, migrations)
		ctx := context.Background()

		if err := m.Up(ctx, db, 0); err != nil {
			t.Fatalf("first Up() error = %v", err)
		}
		if err := m.Up(ctx, db, 0); err != nil {
			t.Fatalf("second Up() error = %v", err)
		}

		var rows int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+rung.LedgerTable).Scan(&rows); err != nil {
			t.Fatalf("counting ledger rows: %v", err)
		}
		if rows != 3 {
			t.Errorf("ledger holds %d rows after two runs, want 3", rows)
		}
	})
}

// The bound is what --target promises. Applying everything when an operator
// asked to stop at a known-good version is the failure this guards.
func TestUpStopsAtTheTarget(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dialect, db := e.connect(t)
		m := rung.New(dialect, migrations)
		ctx := context.Background()

		if err := m.Up(ctx, db, 2); err != nil {
			t.Fatalf("Up(target=2) error = %v", err)
		}

		pending, err := m.Pending(ctx, db)
		if err != nil {
			t.Fatalf("Pending() error = %v", err)
		}
		if len(pending) != 1 || pending[0] != 3 {
			t.Errorf("Pending() = %v, want [3]", pending)
		}
	})
}

func TestDownRollsBackTheNewest(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dialect, db := e.connect(t)
		m := rung.New(dialect, migrations)
		ctx := context.Background()

		if err := m.Up(ctx, db, 0); err != nil {
			t.Fatalf("Up() error = %v", err)
		}
		if err := m.Down(ctx, db); err != nil {
			t.Fatalf("Down() error = %v", err)
		}

		pending, err := m.Pending(ctx, db)
		if err != nil {
			t.Fatalf("Pending() error = %v", err)
		}
		if len(pending) != 1 || pending[0] != 3 {
			t.Errorf("Pending() = %v, want [3]", pending)
		}
	})
}

// An exhausted ledger is the answer to "undo one more", not a failure, and a
// caller looping needs to be able to tell.
func TestDownReportsAnExhaustedLedger(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dialect, db := e.connect(t)
		m := rung.New(dialect, migrations)
		ctx := context.Background()

		if err := m.Up(ctx, db, 0); err != nil {
			t.Fatalf("Up() error = %v", err)
		}
		for i := 0; i < 3; i++ {
			if err := m.Down(ctx, db); err != nil {
				t.Fatalf("Down() %d error = %v", i+1, err)
			}
		}

		err := m.Down(ctx, db)
		if !errors.Is(err, rung.ErrNothingToRollback) {
			t.Errorf("Down() on an empty ledger = %v, want ErrNothingToRollback", err)
		}
	})
}

// Reads must not have side effects: a status query that created the ledger
// would be modifying the schema it claims only to inspect.
func TestStatusesDoesNotCreateTheLedger(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dialect, db := e.connect(t)
		m := rung.New(dialect, migrations)
		ctx := context.Background()

		statuses, err := m.Statuses(ctx, db)
		if err != nil {
			t.Fatalf("Statuses() error = %v", err)
		}
		for _, status := range statuses {
			if status.Applied {
				t.Errorf("version %d reported as applied against an empty database", status.Version)
			}
		}

		var exists bool
		if err := db.QueryRowContext(ctx, dialect.LedgerExistsQuery()).Scan(&exists); err != nil {
			t.Fatalf("checking for the ledger: %v", err)
		}
		if exists {
			t.Error("Statuses() created the ledger table")
		}
	})
}

// A migration that fails must leave no ledger row claiming it succeeded, so
// that the next run re-attempts it rather than skipping it.
func TestAFailedMigrationRecordsNothing(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dialect, db := e.connect(t)
		m := rung.New(dialect, broken)
		ctx := context.Background()

		err := m.Up(ctx, db, 0)
		if err == nil {
			t.Fatal("Up() error = nil, want the invalid SQL to fail")
		}
		if !strings.Contains(err.Error(), "applying migration 1") {
			t.Errorf("Up() error = %q, want it to name the migration", err)
		}

		var rows int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+rung.LedgerTable).Scan(&rows); err != nil {
			t.Fatalf("counting ledger rows: %v", err)
		}
		if rows != 0 {
			t.Errorf("ledger holds %d rows after a failed migration, want 0", rows)
		}
	})
}

// Rolling back a version this build does not carry would apply the wrong down
// file and leave a schema matching neither version.
func TestDownRefusesAVersionItDoesNotCarry(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dialect, db := e.connect(t)
		ctx := context.Background()

		full := rung.New(dialect, migrations)
		if err := full.Up(ctx, db, 0); err != nil {
			t.Fatalf("Up() error = %v", err)
		}

		// A build carrying only the first two migrations, as an older release
		// would be.
		older := fstest.MapFS{}
		for name, contents := range migrations {
			if !strings.Contains(name, "000003") {
				older[name] = contents
			}
		}

		err := rung.New(dialect, older).Down(ctx, db)
		if err == nil {
			t.Fatal("Down() error = nil, want a refusal")
		}
		if !strings.Contains(err.Error(), "does not carry it") {
			t.Errorf("Down() error = %q, want it to explain the mismatch", err)
		}
	})
}

func TestResetDropsEverything(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dialect, db := e.connect(t)
		m := rung.New(dialect, migrations)
		ctx := context.Background()

		if err := m.Up(ctx, db, 0); err != nil {
			t.Fatalf("Up() error = %v", err)
		}

		// Three tables plus the ledger.
		dropped, err := reset.DropAll(ctx, db, dialect)
		if err != nil {
			t.Fatalf("DropAll() error = %v", err)
		}
		if dropped != 4 {
			t.Errorf("DropAll() dropped %d tables, want 4", dropped)
		}

		var exists bool
		if err := db.QueryRowContext(ctx, dialect.LedgerExistsQuery()).Scan(&exists); err != nil {
			t.Fatalf("checking for the ledger: %v", err)
		}
		if exists {
			t.Error("DropAll() left the ledger behind")
		}
	})
}

func TestExpectedMatchesTheFileSet(t *testing.T) {
	eachEngine(t, func(t *testing.T, e engine) {
		dialect, _ := e.connect(t)

		expected, err := rung.New(dialect, migrations).Expected()
		if err != nil {
			t.Fatalf("Expected() error = %v", err)
		}
		if expected != 3 {
			t.Errorf("Expected() = %d, want 3", expected)
		}
	})
}
