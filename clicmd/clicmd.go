// Package clicmd builds a complete cobra command tree for applying migrations.
//
// A service embeds its own migration files and gets a migrate binary from a few
// lines:
//
//	func main() {
//		cmd := clicmd.New(clicmd.Options{
//			Use:       "migrate",
//			Short:     "example database migration tool",
//			EnvPrefix: "EXAMPLE",
//			FS:        migrations.FS(),
//		})
//		if err := cmd.Execute(); err != nil {
//			os.Exit(1)
//		}
//	}
//
// The resulting binary needs DDL privileges. The service it ships beside should
// not have them: it connects as an account that can read and write rows but
// cannot create, alter or drop anything.
//
// # Confirmation
//
// Destructive commands ask before acting. In CI there is no terminal, so they
// refuse rather than block -- a deploy script must pass --force explicitly. The
// alternative, treating an unanswerable prompt as "no", reports success while
// silently skipping the migrations.
package clicmd

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gruberchris/rung"
	"github.com/gruberchris/rung/render"
)

// Options configures the command tree.
type Options struct {
	// Use is the command name, conventionally "migrate". Defaults to "rung".
	Use string
	// Short is the one-line description shown in help.
	Short string
	// Long is the full description shown in the command's own help.
	Long string
	// Version is reported by --version. Empty omits the flag.
	Version string

	// EnvPrefix names the environment variables the connection is read from:
	// ${EnvPrefix}_DATABASE_URI and ${EnvPrefix}_DATABASE_DRIVER.
	// Defaults to "RUNG".
	EnvPrefix string

	// FS holds the migration files, usually embedded. It may be nil, in which
	// case --migrations-path is required.
	FS fs.FS
	// Dir overrides the directory read from FS, which defaults to the
	// dialect's own. Use "." for a flat, single-dialect layout.
	Dir string

	// Console receives all output. A nil Console writes decorated output to
	// standard output, and is also used as the migration reporter.
	Console *render.Console

	// Config supplies the driver and DSN when neither a flag nor an
	// environment variable did. It is optional, and exists so that a service
	// can fall back to its own configuration file.
	Config func() (driver, dsn string, err error)

	// Stdin is read when confirming. A nil Stdin means os.Stdin.
	Stdin FileReader
}

// FileReader is the part of *os.File that confirmation needs. Anything that is
// not an *os.File is assumed to be a non-interactive test double and is read
// without a terminal check.
type FileReader interface {
	Read(p []byte) (int, error)
}

// app holds the state one invocation resolves.
type app struct {
	opts    Options
	console *render.Console

	databaseURI    string
	driver         string
	migrationsPath string
	dir            string
	force          bool
	verbose        bool
	noColor        bool
	noEmoji        bool

	dialect     rung.Dialect
	db          *sql.DB
	migrator    *rung.Migrator
	resolvedDSN string
}

// New returns the root command. The caller runs it with Execute.
func New(opts Options) *cobra.Command {
	if opts.Use == "" {
		opts.Use = "rung"
	}
	if opts.EnvPrefix == "" {
		opts.EnvPrefix = "RUNG"
	}
	if opts.Short == "" {
		opts.Short = "Apply versioned SQL migrations"
	}

	a := &app{opts: opts, console: opts.Console}
	if a.console == nil {
		a.console = render.NewConsole(nil)
	}

	root := &cobra.Command{
		Use:     opts.Use,
		Short:   opts.Short,
		Long:    opts.longDescription(),
		Version: opts.Version,
		// Usage on a runtime failure is noise; the error is the message. Errors
		// are printed by this package so that they carry the same decoration as
		// everything else.
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	flags := root.PersistentFlags()
	flags.StringVar(&a.databaseURI, "database-uri", "",
		"Database connection DSN (overrides the environment and any config)")
	flags.StringVar(&a.driver, "driver", "",
		"Database driver: "+strings.Join(rung.Names(), ", "))
	flags.StringVar(&a.migrationsPath, "migrations-path", "",
		"Read migrations from this directory instead of the embedded set")
	flags.StringVar(&a.dir, "dir", opts.Dir,
		`Directory within the migrations path for this dialect (default: the dialect's name; "." for a flat layout)`)
	flags.BoolVarP(&a.force, "force", "f", false,
		"Do not ask for confirmation (required in CI, where there is no terminal)")
	flags.BoolVarP(&a.verbose, "verbose", "v", false, "Report migrations that were already applied")
	flags.BoolVar(&a.noColor, "no-color", false, "Disable coloured output")
	flags.BoolVar(&a.noEmoji, "no-emoji", false, "Disable emoji in output")

	root.AddCommand(
		a.upCommand(),
		a.downCommand(),
		a.statusCommand(),
		a.initCommand(),
	)

	return root
}

func (o Options) longDescription() string {
	if o.Long != "" {
		return o.Long
	}

	prefix := o.EnvPrefix
	if prefix == "" {
		prefix = "RUNG"
	}

	return fmt.Sprintf(`%s

The driver and connection are read from the flags, then %s_DATABASE_DRIVER and
%s_DATABASE_URI, then any configured fallback, in that order.

This tool needs DDL privileges. The service it ships beside should not have
them: it is expected to connect as an account holding only SELECT, INSERT,
UPDATE and DELETE.

Examples:
  %s up                  # Apply every pending migration
  %s up --force          # Skip confirmation (required in CI: there is no terminal)
  %s up --target 5       # Apply migrations up to and including version 5
  %s up --dry-run        # Report what would be applied
  %s status              # Show applied and pending migrations
  %s status --format json
  %s down                # Roll back the most recent migration
  %s down --steps 2      # Roll back the last two`,
		o.Short, prefix, prefix,
		o.Use, o.Use, o.Use, o.Use, o.Use, o.Use, o.Use, o.Use)
}

// run wires a command body to a connection whose lifetime is the command.
func (a *app) run(body func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := a.connect(cmd); err != nil {
			a.console.Error("❌", "%v", err)
			return err
		}
		defer a.disconnect()

		err := body(cmd, args)
		if err != nil && !isReported(err) {
			a.console.Error("❌", "%v", err)
		}
		return err
	}
}

// connect resolves configuration, opens the database and builds the migrator.
func (a *app) connect(cmd *cobra.Command) error {
	a.console.NoColor = a.console.NoColor || a.noColor
	a.console.NoEmoji = a.console.NoEmoji || a.noEmoji
	a.console.Verbose = a.console.Verbose || a.verbose

	driver, dsn, err := a.resolveConnection()
	if err != nil {
		return err
	}
	a.resolvedDSN = dsn

	a.dialect, err = rung.For(driver)
	if err != nil {
		return err
	}

	fsys, err := a.resolveFS()
	if err != nil {
		return err
	}

	// A multi-statement connection, which serving traffic never gets.
	a.db, err = a.dialect.OpenForMigrations(dsn)
	if err != nil {
		return fmt.Errorf("opening the database: %w", err)
	}
	if err := a.db.PingContext(cmd.Context()); err != nil {
		_ = a.db.Close()
		a.db = nil
		return fmt.Errorf("connecting to %s as the migration account: %w", maskDSN(dsn), err)
	}

	options := []rung.Option{rung.WithReporter(a.console)}
	if a.dir != "" {
		options = append(options, rung.WithDir(a.dir))
	}
	a.migrator = rung.New(a.dialect, fsys, options...)

	if a.verbose {
		a.console.Info("", "Connected: driver %s, migrations %s, target %s",
			a.dialect.Name(), a.migrationsSource(), maskDSN(dsn))
	}
	return nil
}

func (a *app) disconnect() {
	if a.db != nil {
		_ = a.db.Close()
		a.db = nil
	}
}

// resolveConnection applies the precedence: flag, environment, configuration.
func (a *app) resolveConnection() (driver, dsn string, err error) {
	driver, dsn = a.driver, a.databaseURI

	if driver == "" {
		driver = os.Getenv(a.opts.EnvPrefix + "_DATABASE_DRIVER")
	}
	if dsn == "" {
		dsn = os.Getenv(a.opts.EnvPrefix + "_DATABASE_URI")
	}

	if (driver == "" || dsn == "") && a.opts.Config != nil {
		configuredDriver, configuredDSN, configErr := a.opts.Config()
		if configErr != nil {
			return "", "", fmt.Errorf("loading configuration: %w", configErr)
		}
		if driver == "" {
			driver = configuredDriver
		}
		if dsn == "" {
			dsn = configuredDSN
		}
	}

	if driver == "" {
		return "", "", fmt.Errorf(
			"no database driver: pass --driver or set %s_DATABASE_DRIVER (registered drivers are %s)",
			a.opts.EnvPrefix, strings.Join(rung.Names(), ", "))
	}
	if dsn == "" {
		return "", "", fmt.Errorf(
			"no database DSN: pass --database-uri or set %s_DATABASE_URI", a.opts.EnvPrefix)
	}
	return driver, dsn, nil
}

// resolveFS prefers an explicit --migrations-path over the embedded set.
func (a *app) resolveFS() (fs.FS, error) {
	if a.migrationsPath != "" {
		// Rooted above the dialect directory, so that the migrator finds
		// postgres/ or mysql/ beneath it exactly as it does when embedded.
		return os.DirFS(a.migrationsPath), nil
	}
	if a.opts.FS != nil {
		return a.opts.FS, nil
	}
	return nil, errors.New("no migrations: this build embeds none, so --migrations-path is required")
}

func (a *app) migrationsSource() string {
	if a.migrationsPath != "" {
		return a.migrationsPath
	}
	return "embedded"
}

// reportedError marks an error whose message has already been shown, so that it
// is not printed a second time on the way out.
type reportedError struct{ err error }

func (e reportedError) Error() string { return e.err.Error() }
func (e reportedError) Unwrap() error { return e.err }

func reported(err error) error { return reportedError{err: err} }

func isReported(err error) bool {
	var target reportedError
	return errors.As(err, &target)
}
