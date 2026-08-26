package rung

import (
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Dialect is everything that differs between the supported databases.
//
// The rule this interface exists to enforce: nothing else may branch on which
// database is in use. Migration files stay in each dialect's own directory, and
// what genuinely diverges -- connection handling, placeholder syntax, the
// ledger's DDL -- lives here, selected once from a configured driver name.
//
// Implementations must be safe for concurrent use and are expected to be
// stateless value types.
type Dialect interface {
	// Name is the canonical driver name, such as "postgres" or "mysql".
	Name() string

	// MigrationsDir is the directory within the file set holding this
	// dialect's migrations. It is a single path element, not a path.
	MigrationsDir() string

	// OpenForMigrations returns a database handle able to execute a file
	// containing several statements.
	//
	// This is deliberately separate from however an application opens its own
	// pool. Both supported drivers refuse multi-statement execution by
	// default, in different ways and for different reasons, and that default
	// is what lets a driver reject an injected statement. Relaxing it is
	// appropriate for a migration tool and never for serving traffic.
	OpenForMigrations(dsn string) (*sql.DB, error)

	// Rebind converts a query written with ? placeholders into this dialect's
	// syntax. Queries are written once, with ?, and translated here.
	Rebind(query string) string

	// LedgerDDL creates the _migrations table if it does not already exist.
	LedgerDDL() string

	// LedgerExistsQuery reports whether the _migrations table exists, as a
	// single boolean column, scoped to the connected database.
	//
	// The read-only paths use this because they must not create the table they
	// claim only to inspect: a database with no ledger has simply had nothing
	// applied.
	LedgerExistsQuery() string
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Dialect)
)

// Register makes a Dialect available under one or more driver names.
//
// It is intended to be called from a dialect package's init function, so that
// importing that package is what makes its names resolvable:
//
//	func init() { rung.Register(Dialect{}, "postgres", "postgresql", "pgx") }
//
// Register panics if d is nil, if no names are given, or if a name is already
// registered, all of which are programming errors detectable at startup.
func Register(d Dialect, names ...string) {
	if d == nil {
		panic("rung: Register called with a nil Dialect")
	}
	if len(names) == 0 {
		panic("rung: Register called with no names")
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	for _, name := range names {
		key := normalizeDriver(name)
		if key == "" {
			panic("rung: Register called with an empty driver name")
		}
		if _, exists := registry[key]; exists {
			panic("rung: driver " + strconv.Quote(key) + " is already registered")
		}
		registry[key] = d
	}
}

// For returns the Dialect registered under a driver name.
//
// Matching ignores case and surrounding space, and dialects register generous
// aliases: "postgresql" is what a deployment is likely to call it, "pgx" is the
// driver, and "mariadb" is what somebody running MariaDB will write even though
// the MySQL dialect serves it.
func For(name string) (Dialect, error) {
	registryMu.RLock()
	d, ok := registry[normalizeDriver(name)]
	registryMu.RUnlock()

	if !ok {
		registered := Names()
		if len(registered) == 0 {
			return nil, fmt.Errorf(
				"rung: unsupported database driver %q: no dialects are registered "+
					`(import one, for example _ "github.com/gruberchris/rung/dialect/postgres")`,
				name)
		}
		return nil, fmt.Errorf(
			"rung: unsupported database driver %q: registered drivers are %s",
			name, strings.Join(registered, ", "))
	}
	return d, nil
}

// Names lists every registered driver name, including aliases, sorted. It is
// intended for error messages and help text.
func Names() []string {
	registryMu.RLock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	registryMu.RUnlock()

	slices.Sort(names)
	return names
}

func normalizeDriver(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// RebindDollar converts ? placeholders into PostgreSQL's numbered $1, $2, …
// form. It is exported so that third-party dialects can reuse it.
//
// It does not parse SQL: a literal question mark inside a string literal is
// rewritten too. Queries with such literals should be written in the target
// dialect's own syntax rather than passed through Rebind.
func RebindDollar(query string) string {
	count := strings.Count(query, "?")
	if count == 0 {
		return query
	}

	var b strings.Builder
	// Each placeholder grows by at least one byte ("?" -> "$1"), and more once
	// the ordinal reaches two digits.
	b.Grow(len(query) + count*2)

	ordinal := 0
	for i := 0; i < len(query); i++ {
		if query[i] != '?' {
			b.WriteByte(query[i])
			continue
		}
		ordinal++
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(ordinal))
	}
	return b.String()
}

// RebindQuestion returns the query unchanged, for dialects that already use ?
// placeholders. It exists so that every Dialect implementation states its
// placeholder syntax explicitly rather than by omission.
func RebindQuestion(query string) string { return query }
