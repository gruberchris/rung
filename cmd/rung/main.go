// Command rung applies versioned SQL migrations from a directory.
//
// It is the standalone form of the library: useful for ad-hoc migration of a
// database from a workstation, for a CI job that has the migration files but no
// service image, and for trying the tool out. A service is better served by
// embedding its own migrations and building its own binary with the clicmd
// package, so that the schema and the code expecting it ship together.
//
// Usage:
//
//	rung --driver postgres --database-uri "$DSN" --migrations-path ./migrations up --force
//
// The migrations path is expected to hold one directory per dialect --
// postgres/ and mysql/ -- so that a single tree serves several databases. Pass
// --dir . for a flat directory holding one dialect's files.
package main

import (
	"os"
	"runtime/debug"

	"github.com/gruberchris/rung/clicmd"

	_ "github.com/gruberchris/rung/dialect/mysql"    // registers "mysql" and "mariadb"
	_ "github.com/gruberchris/rung/dialect/postgres" // registers "postgres", "postgresql" and "pgx"
	_ "github.com/gruberchris/rung/dialect/sqlite"   // registers "sqlite" and "sqlite3"
)

// Stamped at link time by the release build; see .goreleaser.yaml.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// init recovers the version for builds the release pipeline did not produce.
//
// GoReleaser supplies the values above through -ldflags. A `go install
// module@version` build gets no ldflags, but the toolchain records the module
// version in the binary, so read it back rather than reporting "dev" for a
// binary that plainly has a version.
func init() {
	if version != "dev" {
		return // -ldflags supplied a real version.
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	// "(devel)" is what a build from a working tree reports, which "dev"
	// already says more clearly.
	if info.Main.Version == "" || info.Main.Version == "(devel)" {
		return
	}
	version = info.Main.Version
}

func main() {
	cmd := clicmd.New(clicmd.Options{
		Use:       "rung",
		Short:     "Apply versioned SQL migrations to PostgreSQL, MySQL or MariaDB",
		Version:   version + " (commit " + commit + ", built " + date + ")",
		EnvPrefix: "RUNG",
	})

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
