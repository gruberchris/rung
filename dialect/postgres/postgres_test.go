package postgres

import (
	"strings"
	"testing"

	"github.com/gruberchris/rung"
)

func TestDialectIsRegistered(t *testing.T) {
	for _, name := range []string{"postgres", "postgresql", "pgx", "PostgreSQL"} {
		t.Run(name, func(t *testing.T) {
			d, err := rung.For(name)
			if err != nil {
				t.Fatalf("rung.For(%q) error = %v", name, err)
			}
			if d.Name() != "postgres" {
				t.Errorf("rung.For(%q).Name() = %q, want postgres", name, d.Name())
			}
		})
	}
}

func TestMigrationsDir(t *testing.T) {
	if got := (Dialect{}).MigrationsDir(); got != "postgres" {
		t.Errorf("MigrationsDir() = %q, want postgres", got)
	}
}

func TestOpenForMigrationsReportsAMalformedDSN(t *testing.T) {
	if _, err := (Dialect{}).OpenForMigrations("://nonsense"); err == nil {
		t.Fatal("OpenForMigrations() error = nil, want an error")
	}
}

func TestRebindNumbersPlaceholders(t *testing.T) {
	got := (Dialect{}).Rebind("INSERT INTO t (a, b, c) VALUES (?, ?, ?)")
	want := "INSERT INTO t (a, b, c) VALUES ($1, $2, $3)"
	if got != want {
		t.Errorf("Rebind() = %q, want %q", got, want)
	}
}

// The read-only path must not create the table it claims only to inspect, so it
// needs an existence check rather than a CREATE.
func TestLedgerExistsQueryOnlyReads(t *testing.T) {
	query := (Dialect{}).LedgerExistsQuery()

	if !strings.Contains(query, "current_schema()") {
		t.Errorf("LedgerExistsQuery() = %q, want it scoped to the current schema", query)
	}
	if strings.Contains(strings.ToUpper(query), "CREATE") {
		t.Errorf("LedgerExistsQuery() = %q, want it to create nothing", query)
	}
}

func TestLedgerDDLIsIdempotent(t *testing.T) {
	ddl := (Dialect{}).LedgerDDL()
	if !strings.Contains(ddl, "IF NOT EXISTS") {
		t.Errorf("LedgerDDL() = %q, want IF NOT EXISTS so it can run on every command", ddl)
	}
}

// Both dialects record the same four columns; a ledger written by one must be
// readable by the other after a database migration.
func TestLedgerDDLCarriesTheAgreedColumns(t *testing.T) {
	ddl := (Dialect{}).LedgerDDL()
	for _, column := range []string{"version", "name", "applied_at"} {
		if !strings.Contains(ddl, column) {
			t.Errorf("LedgerDDL() = %q, want a %q column", ddl, column)
		}
	}
	if !strings.Contains(ddl, "UNIQUE") {
		t.Errorf("LedgerDDL() = %q, want version to be unique", ddl)
	}
}

func TestQuoteIdentifierEscapesQuotes(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"users", `"users"`},
		{`we"ird`, `"we""ird"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Dialect{}).QuoteIdentifier(tt.name); got != tt.want {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// CASCADE means dependency ordering does not have to be computed.
func TestDropTablesStatementCascades(t *testing.T) {
	statement := (Dialect{}).DropTablesStatement([]string{`"a"`, `"b"`})

	if !strings.HasSuffix(statement, "CASCADE") {
		t.Errorf("DropTablesStatement() = %q, want it to CASCADE", statement)
	}
	if !strings.Contains(statement, `"a", "b"`) {
		t.Errorf("DropTablesStatement() = %q, want both tables named", statement)
	}
}
