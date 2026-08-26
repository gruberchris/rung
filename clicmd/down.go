package clicmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/gruberchris/rung"
	"github.com/gruberchris/rung/render"
)

func (a *app) downCommand() *cobra.Command {
	var (
		steps       int
		rollbackAll bool
	)

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Roll back applied migrations",
		Long: `Rolls back the most recently applied migration, or several with --steps.

This runs each migration's down file, which typically drops schema objects and
the data in them. It cannot be undone.

Asking for more steps than there are applied migrations is not an error: the
rollback stops at the end of the ledger and reports what it did.`,
	}

	cmd.Flags().IntVar(&steps, "steps", 1, "Number of migrations to roll back")
	cmd.Flags().BoolVar(&rollbackAll, "all", false, "Roll back every applied migration")

	cmd.RunE = a.run(func(c *cobra.Command, _ []string) error {
		return a.down(c, steps, rollbackAll)
	})

	return cmd
}

func (a *app) down(cmd *cobra.Command, steps int, rollbackAll bool) error {
	if steps < 1 {
		return errors.New("--steps must be at least 1")
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

	switch {
	case rollbackAll:
		a.console.Warn("⚠️", "WARNING: This will rollback ALL migrations. This is VERY DESTRUCTIVE!")
	case steps > 1:
		a.console.Warn("⚠️", "WARNING: This will rollback the last %d migrations.", steps)
	default:
		a.console.Warn("⚠️", "WARNING: This will rollback the last applied migration.")
	}
	a.console.Blank()

	if !a.force {
		confirmed, err := a.confirm("Are you absolutely sure you want to proceed?")
		if err != nil {
			return err
		}
		if !confirmed {
			a.console.Warn("❌", "Rollback cancelled")
			return nil
		}
		a.console.Blank()
	}

	a.console.Info("⚡", "Rolling back migrations...")

	// An unbounded rollback is expressed as "more steps than there can be",
	// so both paths share one loop that stops on an exhausted ledger.
	limit := steps
	if rollbackAll {
		limit = len(statuses)
	}

	rolledBack := 0
	for i := 0; i < limit; i++ {
		if err := a.migrator.Down(cmd.Context(), a.db); err != nil {
			if errors.Is(err, rung.ErrNothingToRollback) {
				break
			}
			a.console.Error("❌", "Rollback failed after %d migration(s): %v", rolledBack, err)
			return reported(err)
		}
		rolledBack++
	}

	switch {
	case rolledBack == 0:
		a.console.Info("ℹ️", "No migrations to rollback")
	case !rollbackAll && rolledBack < steps:
		a.console.Warn("⚠️", "Only %d of %d requested migration(s) were available to roll back", rolledBack, steps)
		a.console.Success("✅", "Successfully rolled back %d migration(s)!", rolledBack)
	default:
		a.console.Success("✅", "Successfully rolled back %d migration(s)!", rolledBack)
	}
	return nil
}
