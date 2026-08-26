package rung

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

// LedgerTable is the name of the table recording which migrations have been
// applied. It is fixed rather than configurable: it is a schema contract with
// every database this package has already migrated.
const LedgerTable = "_migrations"

// Migration is one migration, as a pair of files.
type Migration struct {
	Version  int
	Name     string
	UpFile   string
	DownFile string
}

// String renders a migration as its zero-padded version and name, the form used
// in file names.
func (m Migration) String() string {
	return fmt.Sprintf("%06d_%s", m.Version, m.Name)
}

// Status reports whether a known migration has been applied.
type Status struct {
	Version   int
	Name      string
	Applied   bool
	AppliedAt time.Time
}

// Migrator applies one dialect's migrations from one file set.
//
// A Migrator holds no database handle: the handle is passed to each call, so
// one Migrator can serve a short-lived command and a long-running server alike.
// It is safe for concurrent use.
type Migrator struct {
	dialect  Dialect
	fsys     fs.FS
	dir      string
	reporter Reporter
}

// Option configures a Migrator.
type Option func(*Migrator)

// WithDir overrides the directory migrations are read from, which defaults to
// the dialect's [Dialect.MigrationsDir].
//
// Use it for a file set that does not follow the convention -- a legacy layout
// naming the directory "postgresql", or "." for a flat directory holding a
// single dialect's files.
func WithDir(dir string) Option {
	return func(m *Migrator) { m.dir = dir }
}

// WithReporter directs progress events to r. Without it a Migrator is silent.
func WithReporter(r Reporter) Option {
	return func(m *Migrator) {
		if r != nil {
			m.reporter = r
		}
	}
}

// New returns a Migrator reading d's migrations out of fsys.
//
// Both d and fsys must be non-nil. The directory read is d.MigrationsDir()
// unless [WithDir] says otherwise.
func New(d Dialect, fsys fs.FS, opts ...Option) *Migrator {
	m := &Migrator{
		dialect:  d,
		fsys:     fsys,
		dir:      d.MigrationsDir(),
		reporter: nopReporter{},
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Dialect returns the dialect this Migrator was built for.
func (m *Migrator) Dialect() Dialect { return m.dialect }

// Dir returns the directory migrations are read from.
func (m *Migrator) Dir() string { return m.dir }

// Up applies every migration not already recorded in the ledger, in version
// order.
//
// A target above zero bounds the run: migrations up to and including that
// version are applied and the rest are left pending. Because migrations are
// ordered, the first version past the target ends the run rather than being
// skipped -- applying a later migration while leaving an earlier one pending
// would produce a schema that no sequence of migrations describes.
//
// Up is idempotent. Applying an up-to-date database is a no-op.
func (m *Migrator) Up(ctx context.Context, db *sql.DB, target int) error {
	if err := m.ensureLedger(ctx, db); err != nil {
		return err
	}

	migrations, err := m.Load()
	if err != nil {
		return err
	}

	applied, err := m.appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	insert := m.dialect.Rebind(
		"INSERT INTO " + LedgerTable + " (version, name, applied_at) VALUES (?, ?, ?)")

	for _, migration := range migrations {
		if target > 0 && migration.Version > target {
			m.reporter.StoppedAtTarget(target, migration.Version)
			break
		}

		if _, ok := applied[migration.Version]; ok {
			m.reporter.Skipped(migration)
			continue
		}

		m.reporter.Applying(migration)

		if err := m.exec(ctx, db, migration.UpFile, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, insert, migration.Version, migration.Name, time.Now().UTC())
			return err
		}); err != nil {
			return fmt.Errorf("applying migration %d (%s): %w", migration.Version, migration.Name, err)
		}

		m.reporter.Applied(migration)
	}

	return nil
}

// Down rolls back the highest version recorded in the ledger.
//
// It reports [ErrNothingToRollback] when the ledger is empty, so that a caller
// rolling back repeatedly can stop.
func (m *Migrator) Down(ctx context.Context, db *sql.DB) error {
	if err := m.ensureLedger(ctx, db); err != nil {
		return err
	}

	migrations, err := m.Load()
	if err != nil {
		return err
	}

	applied, err := m.appliedVersions(ctx, db)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		return ErrNothingToRollback
	}

	newest := 0
	for version := range applied {
		if version > newest {
			newest = version
		}
	}

	index := slices.IndexFunc(migrations, func(candidate Migration) bool {
		return candidate.Version == newest
	})
	if index < 0 {
		// The ledger names a migration this file set does not carry, which
		// means the database was migrated by a newer build. Rolling back with
		// the wrong file would leave a schema matching neither version.
		return fmt.Errorf(
			"the database has migration %d applied, but this build does not carry it; "+
				"roll back with the build that applied it", newest)
	}
	migration := migrations[index]

	remove := m.dialect.Rebind("DELETE FROM " + LedgerTable + " WHERE version = ?")

	m.reporter.RollingBack(migration)

	if err := m.exec(ctx, db, migration.DownFile, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, remove, migration.Version)
		return err
	}); err != nil {
		return fmt.Errorf("rolling back migration %d (%s): %w", migration.Version, migration.Name, err)
	}

	m.reporter.RolledBack(migration)

	return nil
}

// Statuses lists every migration in the file set with its applied state, in
// version order. It does not create the ledger.
func (m *Migrator) Statuses(ctx context.Context, db *sql.DB) ([]Status, error) {
	migrations, err := m.Load()
	if err != nil {
		return nil, err
	}

	applied, err := m.readLedger(ctx, db)
	if err != nil {
		return nil, err
	}

	statuses := make([]Status, 0, len(migrations))
	for _, migration := range migrations {
		status := Status{Version: migration.Version, Name: migration.Name}
		if at, ok := applied[migration.Version]; ok {
			status.Applied = true
			status.AppliedAt = at
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// Pending returns the versions in the file set that have not been applied, in
// ascending order. It does not create the ledger, which is what makes it safe
// for a server to call at startup to report drift.
func (m *Migrator) Pending(ctx context.Context, db *sql.DB) ([]int, error) {
	statuses, err := m.Statuses(ctx, db)
	if err != nil {
		return nil, err
	}

	var pending []int
	for _, status := range statuses {
		if !status.Applied {
			pending = append(pending, status.Version)
		}
	}
	return pending, nil
}

// Expected returns the highest version in the file set: the schema this build
// was written against. It is zero when the file set carries no migrations.
func (m *Migrator) Expected() (int, error) {
	migrations, err := m.Load()
	if err != nil {
		return 0, err
	}
	if len(migrations) == 0 {
		return 0, nil
	}
	return migrations[len(migrations)-1].Version, nil
}

// Load reads the file set and returns its migrations in version order.
//
// Files are named NNNNNN_name.up.sql and NNNNNN_name.down.sql. Anything that
// does not parse as that is ignored, so a README or a .gitkeep alongside the
// migrations is harmless.
//
// A version with only one of its two halves is skipped rather than reported:
// applying an up with no matching down would create a state this package cannot
// reverse.
func (m *Migrator) Load() ([]Migration, error) {
	entries, err := fs.ReadDir(m.fsys, m.dir)
	if err != nil {
		return nil, fmt.Errorf("reading migrations directory %q: %w", m.dir, err)
	}

	byVersion := make(map[int]*Migration, len(entries)/2+1)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		version, name, direction, ok := parseFilename(entry.Name())
		if !ok {
			continue
		}

		migration := byVersion[version]
		if migration == nil {
			migration = &Migration{Version: version, Name: name}
			byVersion[version] = migration
		}

		full := path.Join(m.dir, entry.Name())
		switch direction {
		case "up":
			migration.UpFile = full
		case "down":
			migration.DownFile = full
		}
	}

	migrations := make([]Migration, 0, len(byVersion))
	for _, migration := range byVersion {
		if migration.UpFile != "" && migration.DownFile != "" {
			migrations = append(migrations, *migration)
		}
	}

	slices.SortFunc(migrations, func(a, b Migration) int {
		return cmp.Compare(a.Version, b.Version)
	})

	return migrations, nil
}

// parseFilename splits NNNNNN_name.up.sql into its parts. It reports false for
// any name that is not a migration file.
func parseFilename(filename string) (version int, name, direction string, ok bool) {
	rest, isSQL := strings.CutSuffix(filename, ".sql")
	if !isSQL {
		return 0, "", "", false
	}

	digits, rest, found := strings.Cut(rest, "_")
	if !found || digits == "" {
		return 0, "", "", false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, "", "", false
		}
	}
	// Parsed by hand rather than with strconv.Atoi so that a name such as
	// "+1_foo.up.sql", which Atoi accepts, is not treated as a migration.
	for i := 0; i < len(digits); i++ {
		version = version*10 + int(digits[i]-'0')
	}

	// The direction is the last dot-separated element; everything before it is
	// the name, which may itself contain dots.
	name, direction, found = cutLast(rest, ".")
	if !found || name == "" {
		return 0, "", "", false
	}
	if direction != "up" && direction != "down" {
		return 0, "", "", false
	}

	return version, name, direction, true
}

// cutLast is [strings.Cut] anchored at the final occurrence of sep.
func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

// exec runs a migration file and its ledger update in one transaction, so that
// a file failing halfway leaves neither a partially-built schema nor a ledger
// row claiming it succeeded.
func (m *Migrator) exec(ctx context.Context, db *sql.DB, name string, record func(*sql.Tx) error) error {
	body, err := fs.ReadFile(m.fsys, name)
	if err != nil {
		return fmt.Errorf("reading migration file %q: %w", name, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("executing migration SQL: %w", err)
	}

	if err := record(tx); err != nil {
		return fmt.Errorf("recording migration in the ledger: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migration: %w", err)
	}
	return nil
}

func (m *Migrator) ensureLedger(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, m.dialect.LedgerDDL()); err != nil {
		return fmt.Errorf("creating the %s table: %w", LedgerTable, err)
	}
	return nil
}

// readLedger reports applied versions without creating anything.
//
// Both `status` and a server's startup drift check call this, and a read that
// created a table would be modifying the schema it claims only to inspect. A
// database with no ledger has simply had nothing applied.
func (m *Migrator) readLedger(ctx context.Context, db *sql.DB) (map[int]time.Time, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, m.dialect.LedgerExistsQuery()).Scan(&exists); err != nil {
		return nil, fmt.Errorf("checking for the %s table: %w", LedgerTable, err)
	}
	if !exists {
		return map[int]time.Time{}, nil
	}
	return m.appliedVersions(ctx, db)
}

func (m *Migrator) appliedVersions(ctx context.Context, db *sql.DB) (map[int]time.Time, error) {
	rows, err := db.QueryContext(ctx, "SELECT version, applied_at FROM "+LedgerTable)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", LedgerTable, err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[int]time.Time)
	for rows.Next() {
		var (
			version int
			at      time.Time
		)
		if err := rows.Scan(&version, &at); err != nil {
			return nil, fmt.Errorf("scanning a %s row: %w", LedgerTable, err)
		}
		applied[version] = at
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", LedgerTable, err)
	}
	return applied, nil
}
