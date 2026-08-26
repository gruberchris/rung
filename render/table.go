package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gruberchris/rung"
)

// tableTimeLayout is how an applied timestamp appears in the status table.
const tableTimeLayout = "2006-01-02 15:04:05"

// jsonTimeLayout is how an applied timestamp appears in JSON output.
const jsonTimeLayout = "2006-01-02T15:04:05Z"

// Table writes the migration status table.
//
// The format is a compatibility contract, not a presentation choice: it appears
// in deploy logs that people read and in scripts that grep them, so it is kept
// exactly as it is.
func Table(w io.Writer, statuses []rung.Status) error {
	if _, err := fmt.Fprintln(w, "Migration Status:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "================"); err != nil {
		return err
	}

	for _, status := range statuses {
		state := "Pending"
		appliedAt := ""
		if status.Applied {
			state = "Applied"
			appliedAt = status.AppliedAt.Format(tableTimeLayout)
		}
		if _, err := fmt.Fprintf(w, "Version %d: %s [%s] %s\n",
			status.Version, status.Name, state, appliedAt); err != nil {
			return err
		}
	}
	return nil
}

// statusJSON is the wire shape of a status row.
type statusJSON struct {
	Version   int    `json:"version"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	AppliedAt string `json:"applied_at,omitempty"`
}

// JSON writes the migration status as an indented JSON array, for scripts.
//
// An empty status list encodes as [] rather than null, so a consumer can index
// the result without a nil check.
func JSON(w io.Writer, statuses []rung.Status) error {
	rows := make([]statusJSON, 0, len(statuses))
	for _, status := range statuses {
		row := statusJSON{
			Version: status.Version,
			Name:    status.Name,
			Status:  "pending",
		}
		if status.Applied {
			row.Status = "applied"
			row.AppliedAt = status.AppliedAt.UTC().Format(jsonTimeLayout)
		}
		rows = append(rows, row)
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(rows)
}
