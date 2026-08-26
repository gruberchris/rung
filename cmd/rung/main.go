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

	"github.com/gruberchris/rung/clicmd"

	_ "github.com/gruberchris/rung/dialect/mysql"    // registers "mysql" and "mariadb"
	_ "github.com/gruberchris/rung/dialect/postgres" // registers "postgres", "postgresql" and "pgx"
)

// Stamped at link time by the release build; see .goreleaser.yaml.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

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
