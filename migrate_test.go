package rung

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// testDialect is a Dialect that touches no database, for exercising the parts
// of a Migrator that only read files.
type testDialect struct{ dir string }

func (d testDialect) Name() string          { return "test" }
func (d testDialect) MigrationsDir() string { return d.dir }
func (testDialect) OpenForMigrations(string) (*sql.DB, error) {
	return nil, errors.New("testDialect does not open databases")
}
func (testDialect) Rebind(query string) string { return query }
func (testDialect) LedgerDDL() string          { return "" }
func (testDialect) LedgerExistsQuery() string  { return "" }

func migratorFor(files fstest.MapFS, dir string, opts ...Option) *Migrator {
	return New(testDialect{dir: dir}, files, opts...)
}

func sqlFile(body string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(body)} }

func TestLoadOrdersByVersion(t *testing.T) {
	files := fstest.MapFS{
		"postgres/000003_third.up.sql":    sqlFile("SELECT 3"),
		"postgres/000003_third.down.sql":  sqlFile("SELECT 3"),
		"postgres/000001_first.up.sql":    sqlFile("SELECT 1"),
		"postgres/000001_first.down.sql":  sqlFile("SELECT 1"),
		"postgres/000002_second.up.sql":   sqlFile("SELECT 2"),
		"postgres/000002_second.down.sql": sqlFile("SELECT 2"),
	}

	migrations, err := migratorFor(files, "postgres").Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := []struct {
		version int
		name    string
	}{{1, "first"}, {2, "second"}, {3, "third"}}

	if len(migrations) != len(want) {
		t.Fatalf("Load() returned %d migrations, want %d", len(migrations), len(want))
	}
	for i, expected := range want {
		if migrations[i].Version != expected.version || migrations[i].Name != expected.name {
			t.Errorf("migration %d = %d/%q, want %d/%q",
				i, migrations[i].Version, migrations[i].Name, expected.version, expected.name)
		}
	}
}

// A migration with only one half cannot be reversed, so it is skipped rather
// than half-loaded.
func TestLoadSkipsAHalfPair(t *testing.T) {
	files := fstest.MapFS{
		"postgres/000001_first.up.sql":   sqlFile("SELECT 1"),
		"postgres/000001_first.down.sql": sqlFile("SELECT 1"),
		"postgres/000002_orphan.up.sql":  sqlFile("SELECT 2"),
	}

	migrations, err := migratorFor(files, "postgres").Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("Load() returned %d migrations, want 1", len(migrations))
	}
	if migrations[0].Version != 1 {
		t.Errorf("kept version %d, want 1", migrations[0].Version)
	}
}

func TestLoadIgnoresUnrelatedFiles(t *testing.T) {
	files := fstest.MapFS{
		"postgres/README.md":             sqlFile("# migrations"),
		"postgres/.gitkeep":              sqlFile(""),
		"postgres/notes.txt":             sqlFile("not a migration"),
		"postgres/no_version.up.sql":     sqlFile("SELECT 1"),
		"postgres/000001_first.up.sql":   sqlFile("SELECT 1"),
		"postgres/000001_first.down.sql": sqlFile("SELECT 1"),
	}

	migrations, err := migratorFor(files, "postgres").Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("Load() returned %d migrations, want 1", len(migrations))
	}
}

// Each dialect reads only its own directory. Reading the wrong one yields zero
// migrations and silently does nothing, which is the failure this guards.
func TestLoadReadsOnlyItsOwnDirectory(t *testing.T) {
	files := fstest.MapFS{
		"postgres/000001_first.up.sql":   sqlFile("SELECT 1"),
		"postgres/000001_first.down.sql": sqlFile("SELECT 1"),
		"mysql/000001_first.up.sql":      sqlFile("SELECT 1"),
		"mysql/000001_first.down.sql":    sqlFile("SELECT 1"),
		"mysql/000002_second.up.sql":     sqlFile("SELECT 2"),
		"mysql/000002_second.down.sql":   sqlFile("SELECT 2"),
	}

	postgres, err := migratorFor(files, "postgres").Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(postgres) != 1 {
		t.Errorf("postgres loaded %d migrations, want 1", len(postgres))
	}

	mysql, err := migratorFor(files, "mysql").Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(mysql) != 2 {
		t.Errorf("mysql loaded %d migrations, want 2", len(mysql))
	}
}

func TestLoadReportsAMissingDirectory(t *testing.T) {
	_, err := migratorFor(fstest.MapFS{}, "postgres").Load()
	if err == nil {
		t.Fatal("Load() error = nil, want an error naming the missing directory")
	}
	if !strings.Contains(err.Error(), "postgres") {
		t.Errorf("Load() error = %q, want it to name the directory", err)
	}
}

// WithDir serves a layout that does not follow the dialect naming convention.
func TestWithDirOverridesTheDialectDirectory(t *testing.T) {
	files := fstest.MapFS{
		"postgresql/000001_first.up.sql":   sqlFile("SELECT 1"),
		"postgresql/000001_first.down.sql": sqlFile("SELECT 1"),
	}

	m := migratorFor(files, "postgres", WithDir("postgresql"))
	if m.Dir() != "postgresql" {
		t.Errorf("Dir() = %q, want %q", m.Dir(), "postgresql")
	}

	migrations, err := m.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("Load() returned %d migrations, want 1", len(migrations))
	}
}

// A flat directory is expressed as ".", which is what a single-dialect project
// migrating from another tool will have.
func TestWithDirSupportsAFlatLayout(t *testing.T) {
	files := fstest.MapFS{
		"000001_first.up.sql":   sqlFile("SELECT 1"),
		"000001_first.down.sql": sqlFile("SELECT 1"),
	}

	migrations, err := migratorFor(files, "postgres", WithDir(".")).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("Load() returned %d migrations, want 1", len(migrations))
	}
}

func TestExpectedIsTheHighestVersionCarried(t *testing.T) {
	files := fstest.MapFS{
		"postgres/000001_first.up.sql":    sqlFile("SELECT 1"),
		"postgres/000001_first.down.sql":  sqlFile("SELECT 1"),
		"postgres/000007_latest.up.sql":   sqlFile("SELECT 7"),
		"postgres/000007_latest.down.sql": sqlFile("SELECT 7"),
	}

	expected, err := migratorFor(files, "postgres").Expected()
	if err != nil {
		t.Fatalf("Expected() error = %v", err)
	}
	if expected != 7 {
		t.Errorf("Expected() = %d, want 7", expected)
	}
}

func TestExpectedIsZeroWhenNothingIsCarried(t *testing.T) {
	files := fstest.MapFS{"postgres/README.md": sqlFile("nothing here")}

	expected, err := migratorFor(files, "postgres").Expected()
	if err != nil {
		t.Fatalf("Expected() error = %v", err)
	}
	if expected != 0 {
		t.Errorf("Expected() = %d, want 0", expected)
	}
}

func TestParseFilename(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		wantOK        bool
		wantVersion   int
		wantName      string
		wantDirection string
	}{
		{"an up file", "000001_initial_schema.up.sql", true, 1, "initial_schema", "up"},
		{"a down file", "000012_resource_servers.down.sql", true, 12, "resource_servers", "down"},
		{"a name containing dots", "000002_add.v2.column.up.sql", true, 2, "add.v2.column", "up"},
		{"an unpadded version", "7_seven.up.sql", true, 7, "seven", "up"},
		{"not SQL", "000001_initial.up.txt", false, 0, "", ""},
		{"no version", "initial_schema.up.sql", false, 0, "", ""},
		{"no underscore", "000001.up.sql", false, 0, "", ""},
		{"an empty name", "000001_.up.sql", false, 0, "", ""},
		{"no direction", "000001_initial.sql", false, 0, "", ""},
		{"an unknown direction", "000001_initial.sideways.sql", false, 0, "", ""},
		{"a signed version", "+1_initial.up.sql", false, 0, "", ""},
		{"plain text", "README.md", false, 0, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, name, direction, ok := parseFilename(tt.filename)
			if ok != tt.wantOK {
				t.Fatalf("parseFilename(%q) ok = %v, want %v", tt.filename, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if version != tt.wantVersion || name != tt.wantName || direction != tt.wantDirection {
				t.Errorf("parseFilename(%q) = %d/%q/%q, want %d/%q/%q",
					tt.filename, version, name, direction,
					tt.wantVersion, tt.wantName, tt.wantDirection)
			}
		})
	}
}

func TestMigrationString(t *testing.T) {
	m := Migration{Version: 3, Name: "agent_principals"}
	if got, want := m.String(), "000003_agent_principals"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
