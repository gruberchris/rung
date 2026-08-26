// Package reset drops every table in a database, including the migration
// ledger.
//
// It is a separate package because it is destructive and because most programs
// have no business importing it. It exists to support a "start from nothing"
// command in a development or test environment.
package reset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/gruberchris/rung"
)

// Resetter is the part of a dialect needed to drop a schema. A [rung.Dialect]
// that does not implement it cannot be reset by this package.
//
// It is a separate, optional interface rather than part of [rung.Dialect] so
// that implementing a dialect does not oblige anyone to implement a destructive
// operation.
type Resetter interface {
	// ListTablesQuery selects the names of every table in the connected
	// database or current schema, as a single column.
	ListTablesQuery() string
	// QuoteIdentifier quotes a table name for interpolation.
	QuoteIdentifier(name string) string
	// DropTablesStatement drops every already-quoted table named.
	DropTablesStatement(quoted []string) string
}

// ErrUnsupported reports a dialect that does not implement [Resetter].
var ErrUnsupported = errors.New("dialect does not support reset")

// DropAll drops every table in the database, the migration ledger included, and
// returns how many were dropped.
//
// This destroys all data. There is no confirmation and nothing is recoverable;
// asking is the caller's responsibility.
func DropAll(ctx context.Context, db *sql.DB, dialect rung.Dialect) (int, error) {
	resetter, ok := dialect.(Resetter)
	if !ok {
		return 0, fmt.Errorf("%s: %w", dialect.Name(), ErrUnsupported)
	}

	tables, err := listTables(ctx, db, resetter)
	if err != nil {
		return 0, err
	}
	if len(tables) == 0 {
		return 0, nil
	}

	if _, err := db.ExecContext(ctx, resetter.DropTablesStatement(tables)); err != nil {
		return 0, fmt.Errorf("dropping tables: %w", err)
	}
	return len(tables), nil
}

func listTables(ctx context.Context, db *sql.DB, resetter Resetter) ([]string, error) {
	rows, err := db.QueryContext(ctx, resetter.ListTablesQuery())
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning a table name: %w", err)
		}
		tables = append(tables, resetter.QuoteIdentifier(name))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	return tables, nil
}
