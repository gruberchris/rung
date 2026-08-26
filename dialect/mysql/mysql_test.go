package mysql

import (
	"strings"
	"testing"
	"time"

	"github.com/gruberchris/rung"
)

func TestDialectIsRegistered(t *testing.T) {
	for _, name := range []string{"mysql", "mariadb", "MySQL", " MariaDB "} {
		t.Run(name, func(t *testing.T) {
			d, err := rung.For(name)
			if err != nil {
				t.Fatalf("rung.For(%q) error = %v", name, err)
			}
			if d.Name() != "mysql" {
				t.Errorf("rung.For(%q).Name() = %q, want mysql", name, d.Name())
			}
		})
	}
}

// MariaDB runs the MySQL file set; a separate directory would mean maintaining
// two identical copies.
func TestMigrationsDirIsSharedWithMariaDB(t *testing.T) {
	if got := (Dialect{}).MigrationsDir(); got != "mysql" {
		t.Errorf("MigrationsDir() = %q, want mysql", got)
	}
}

// Without parseTime the driver returns DATETIME as []byte and every scan into a
// time.Time fails; without a UTC location it reads timestamps in the server's
// local zone. Neither is left to a hand-written DSN.
func TestMigrationConfigForcesParseTimeAndUTC(t *testing.T) {
	config, err := migrationConfig("user:secret@tcp(127.0.0.1:3306)/example")
	if err != nil {
		t.Fatalf("migrationConfig() error = %v", err)
	}

	if !config.ParseTime {
		t.Error("ParseTime = false, want true")
	}
	if config.Loc != time.UTC {
		t.Errorf("Loc = %v, want UTC", config.Loc)
	}
	if !config.MultiStatements {
		t.Error("MultiStatements = false, want true")
	}
}

// A DSN asking for the opposite is corrected rather than obeyed: these are
// requirements, not preferences. A local time zone here would silently shift
// every timestamp by the deployment's UTC offset.
func TestMigrationConfigOverridesContradictorySettings(t *testing.T) {
	config, err := migrationConfig(
		"user:secret@tcp(127.0.0.1:3306)/example?parseTime=false&multiStatements=false&loc=Local")
	if err != nil {
		t.Fatalf("migrationConfig() error = %v", err)
	}

	if !config.ParseTime {
		t.Error("ParseTime = false, want it forced on")
	}
	if !config.MultiStatements {
		t.Error("MultiStatements = false, want it forced on")
	}
	if config.Loc != time.UTC {
		t.Errorf("Loc = %v, want it forced to UTC", config.Loc)
	}

	// The correction has to survive into the DSN the driver is actually opened
	// with, not just the parsed config.
	formatted := config.FormatDSN()
	if strings.Contains(formatted, "loc=Local") {
		t.Errorf("FormatDSN() = %q, want no local time zone", formatted)
	}
	if !strings.Contains(formatted, "multiStatements=true") {
		t.Errorf("FormatDSN() = %q, want multiStatements=true", formatted)
	}
}

func TestMigrationConfigReportsAMalformedDSN(t *testing.T) {
	if _, err := migrationConfig("this is not a DSN"); err == nil {
		t.Fatal("migrationConfig() error = nil, want an error")
	}
}

// Without TABLE_SCHEMA = DATABASE() the query finds a _migrations table
// belonging to some other database on the same server.
func TestLedgerExistsQueryIsScopedToTheConnectedDatabase(t *testing.T) {
	query := (Dialect{}).LedgerExistsQuery()
	if !strings.Contains(query, "DATABASE()") {
		t.Errorf("LedgerExistsQuery() = %q, want it scoped with DATABASE()", query)
	}
}

// MySQL's TIMESTAMP converts to and from the session time zone and tops out in
// 2038, neither of which is wanted for a record of what has been applied.
func TestLedgerDDLUsesDatetimeNotTimestamp(t *testing.T) {
	ddl := (Dialect{}).LedgerDDL()
	if !strings.Contains(ddl, "DATETIME(6)") {
		t.Errorf("LedgerDDL() = %q, want DATETIME(6)", ddl)
	}
	if strings.Contains(ddl, "TIMESTAMP") {
		t.Errorf("LedgerDDL() = %q, want no TIMESTAMP column", ddl)
	}
}

func TestRebindLeavesQuestionMarks(t *testing.T) {
	query := "INSERT INTO t (a, b) VALUES (?, ?)"
	if got := (Dialect{}).Rebind(query); got != query {
		t.Errorf("Rebind(%q) = %q, want it unchanged", query, got)
	}
}

func TestQuoteIdentifierEscapesBackticks(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"users", "`users`"},
		{"we`ird", "`we``ird`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Dialect{}).QuoteIdentifier(tt.name); got != tt.want {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// MySQL has no CASCADE, so the drop has to suspend foreign key checks itself.
func TestDropTablesStatementSuspendsForeignKeyChecks(t *testing.T) {
	statement := (Dialect{}).DropTablesStatement([]string{"`a`", "`b`"})

	if !strings.HasPrefix(statement, "SET FOREIGN_KEY_CHECKS = 0;") {
		t.Errorf("DropTablesStatement() = %q, want it to disable foreign key checks first", statement)
	}
	if !strings.HasSuffix(statement, "SET FOREIGN_KEY_CHECKS = 1") {
		t.Errorf("DropTablesStatement() = %q, want it to restore foreign key checks", statement)
	}
	if !strings.Contains(statement, "DROP TABLE IF EXISTS `a`, `b`") {
		t.Errorf("DropTablesStatement() = %q, want both tables dropped", statement)
	}
}
