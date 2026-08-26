package clicmd

import (
	"github.com/spf13/cobra"

	"github.com/gruberchris/rung/reset"
)

func (a *app) initCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Drop every table and re-apply all migrations (DESTRUCTIVE)",
		Long: `Drops every table in the database, the migration ledger included, then applies
every migration from the beginning.

This permanently deletes all data. It is meant for setting up a development
environment or resetting a test database, and asks twice before doing anything.

Do not run this against a database you care about.`,
	}

	cmd.RunE = a.run(func(c *cobra.Command, args []string) error {
		a.console.Error("⚠️", "EXTREME DANGER ZONE")
		a.console.Blank()
		a.console.Error("", "This will PERMANENTLY DELETE all data in the database:")
		a.console.Printf("  Driver:     %s\n", a.dialect.Name())
		a.console.Printf("  Target:     %s\n", maskDSN(a.resolvedDSN))
		a.console.Printf("  Migrations: %s\n", a.migrationsSource())
		a.console.Blank()
		a.console.Error("", "ALL tables will be DROPPED.")
		a.console.Error("", "ALL data will be LOST.")
		a.console.Error("", "This operation CANNOT be undone.")
		a.console.Blank()

		if !a.force {
			confirmed, err := a.confirm("Are you ABSOLUTELY SURE you want to drop every table and reset the database?")
			if err != nil {
				return err
			}
			if !confirmed {
				a.console.Warn("❌", "Operation cancelled")
				return nil
			}

			a.console.Warn("⚠️", "Last chance to abort.")
			confirmed, err = a.confirm("Confirm once more that every table should be dropped")
			if err != nil {
				return err
			}
			if !confirmed {
				a.console.Warn("❌", "Operation cancelled")
				return nil
			}
			a.console.Blank()
		}

		a.console.Info("🗑", "Dropping all tables...")
		dropped, err := reset.DropAll(c.Context(), a.db, a.dialect)
		if err != nil {
			a.console.Error("❌", "Failed to reset the database: %v", err)
			return reported(err)
		}
		a.console.Success("✅", "Database emptied (%d table(s) dropped)", dropped)
		a.console.Blank()

		a.console.Info("⚡", "Running all migrations from scratch...")
		// The confirmation above covered this too; asking again would stall a
		// reset that has already destroyed everything it was going to.
		force := a.force
		a.force = true
		defer func() { a.force = force }()

		if err := a.up(c, 0, false); err != nil {
			return err
		}

		a.console.Blank()
		a.console.Success("✅", "Database initialized successfully!")
		return nil
	})

	return cmd
}
