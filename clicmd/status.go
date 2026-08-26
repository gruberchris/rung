package clicmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gruberchris/rung/render"
)

func (a *app) statusCommand() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show which migrations have been applied",
		Long: `Lists every migration this build carries and whether it has been applied.

This is a read-only command. It never creates the ledger table, so running it
against an unmigrated database reports everything as pending rather than
modifying the schema it is inspecting.`,
	}

	cmd.Flags().StringVar(&format, "format", "table", "Output format: table or json")

	cmd.RunE = a.run(func(c *cobra.Command, _ []string) error {
		statuses, err := a.migrator.Statuses(c.Context(), a.db)
		if err != nil {
			return err
		}

		switch format {
		case "json":
			// Only JSON goes to the writer in this mode, so the output can be
			// piped straight into a parser.
			return render.JSON(a.console.Writer(), statuses)
		case "table":
			a.console.Info("📊", "Migration Status (%s):", a.dialect.Name())
			a.console.Blank()
			if len(statuses) == 0 {
				a.console.Printf("(no migrations found in %s)\n", a.migrationsSource())
				return nil
			}
			return render.Table(a.console.Writer(), statuses)
		default:
			return fmt.Errorf("unsupported --format %q: expected \"table\" or \"json\"", format)
		}
	})

	return cmd
}
