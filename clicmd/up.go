package clicmd

import (
	"github.com/spf13/cobra"

	"github.com/gruberchris/rung/render"
)

func (a *app) upCommand() *cobra.Command {
	var (
		dryRun bool
		target int
	)

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Apply every pending migration",
		Long: `Applies pending migrations in version order and records each one in the ledger.

A target bounds the run: migrations up to and including that version are applied
and the rest are left pending. Because migrations are ordered, the first version
past the target ends the run rather than being skipped.`,
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be applied without applying it")
	cmd.Flags().IntVar(&target, "target", 0, "Apply migrations up to and including this version (0 applies all)")

	cmd.RunE = a.run(func(c *cobra.Command, _ []string) error {
		return a.up(c, target, dryRun)
	})

	return cmd
}

func (a *app) up(cmd *cobra.Command, target int, dryRun bool) error {
	if dryRun {
		a.console.Warn("🔍", "DRY RUN MODE - No changes will be applied")
		a.console.Blank()
	}

	a.console.Info("📊", "Checking migration status...")

	statuses, err := a.migrator.Statuses(cmd.Context(), a.db)
	if err != nil {
		return err
	}
	if err := render.Table(a.console.Writer(), statuses); err != nil {
		return err
	}
	a.console.Blank()

	pending := 0
	for _, status := range statuses {
		if !status.Applied && (target == 0 || status.Version <= target) {
			pending++
		}
	}
	if pending == 0 {
		a.console.Success("✅", "The database is up to date; nothing to apply.")
		return nil
	}

	if !a.force && !dryRun {
		confirmed, err := a.confirm("Do you want to proceed with running pending migrations?")
		if err != nil {
			return err
		}
		if !confirmed {
			a.console.Warn("❌", "Migration cancelled")
			return nil
		}
		a.console.Blank()
	}

	if dryRun {
		a.console.Info("✅", "Dry run completed - no changes made")
		return nil
	}

	if target > 0 {
		a.console.Info("⚡", "Running migrations up to version %d...", target)
	} else {
		a.console.Info("⚡", "Running migrations...")
	}

	if err := a.migrator.Up(cmd.Context(), a.db, target); err != nil {
		a.console.Error("❌", "Migration failed: %v", err)
		return reported(err)
	}

	a.console.Success("✅", "All migrations completed successfully!")
	return nil
}
