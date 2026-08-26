package clicmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/gruberchris/rung/render"
)

func TestMaskDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			"a PostgreSQL URL",
			"postgresql://owner:hunter2@db.example.com:5432/example?sslmode=disable",
			"postgresql://owner:xxxxx@db.example.com:5432/example?sslmode=disable",
		},
		{
			"a URL with no password",
			"postgresql://owner@db.example.com:5432/example",
			"postgresql://owner@db.example.com:5432/example",
		},
		{
			"a MySQL DSN",
			"owner:hunter2@tcp(db.example.com:3306)/example",
			"owner:xxxxx@tcp(db.example.com:3306)/example",
		},
		{
			"a MySQL DSN with no password",
			"owner@tcp(db.example.com:3306)/example",
			"owner@tcp(db.example.com:3306)/example",
		},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskDSN(tt.dsn)
			if got != tt.want {
				t.Errorf("maskDSN(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
			if strings.Contains(got, "hunter2") {
				t.Errorf("maskDSN(%q) = %q, which still contains the password", tt.dsn, got)
			}
		})
	}
}

// The failure this guards: without a terminal and without --force, a prompt
// reads EOF, is taken as "no", and the deploy reports success over an
// un-migrated database. Refusing loudly is the only safe answer.
//
// Both non-terminal shapes are covered on purpose. /dev/null is the one that
// matters: it is a *character device*, so a check for os.ModeCharDevice passes
// and only a real terminal test rejects it. `migrate up < /dev/null` in a
// deploy script is exactly how this reaches production.
func TestConfirmRefusesWithoutATerminal(t *testing.T) {
	tests := map[string]func(t *testing.T) *os.File{
		"a pipe": func(t *testing.T) *os.File {
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatalf("os.Pipe() error = %v", err)
			}
			t.Cleanup(func() {
				_ = read.Close()
				_ = write.Close()
			})
			return read
		},
		"/dev/null, which is a character device": func(t *testing.T) *os.File {
			devNull, err := os.Open(os.DevNull)
			if err != nil {
				t.Fatalf("opening %s: %v", os.DevNull, err)
			}
			t.Cleanup(func() { _ = devNull.Close() })
			return devNull
		},
	}

	for name, open := range tests {
		t.Run(name, func(t *testing.T) {
			a := &app{
				opts:    Options{Stdin: open(t)},
				console: render.NewPlain(&bytes.Buffer{}),
			}

			confirmed, err := a.confirm("Proceed?")
			if err == nil {
				t.Fatal("confirm() error = nil, want a refusal")
			}
			if confirmed {
				t.Error("confirm() = true, want false")
			}
			if !strings.Contains(err.Error(), "--force") {
				t.Errorf("confirm() error = %q, want it to name --force", err)
			}
		})
	}
}

// Closed input is silence, not a decision, so it is an error rather than a
// quiet "no" even when the reader is not a file.
func TestConfirmTreatsClosedInputAsAFailure(t *testing.T) {
	a := &app{
		opts:    Options{Stdin: strings.NewReader("")},
		console: render.NewPlain(&bytes.Buffer{}),
	}

	confirmed, err := a.confirm("Proceed?")
	if err == nil {
		t.Fatal("confirm() error = nil, want a refusal")
	}
	if confirmed {
		t.Error("confirm() = true, want false")
	}
}

func TestConfirmSkipsThePromptWhenForced(t *testing.T) {
	var out bytes.Buffer
	a := &app{
		force:   true,
		opts:    Options{Stdin: strings.NewReader("")},
		console: render.NewPlain(&out),
	}

	confirmed, err := a.confirm("Proceed?")
	if err != nil {
		t.Fatalf("confirm() error = %v", err)
	}
	if !confirmed {
		t.Error("confirm() = false, want true")
	}
	if out.Len() != 0 {
		t.Errorf("confirm() wrote %q, want no prompt when forced", out.String())
	}
}

func TestConfirmReadsTheAnswer(t *testing.T) {
	tests := []struct {
		answer string
		want   bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"n\n", false},
		{"\n", false},
		{"maybe\n", false},
	}

	for _, tt := range tests {
		t.Run(strings.TrimSpace(tt.answer), func(t *testing.T) {
			a := &app{
				opts:    Options{Stdin: strings.NewReader(tt.answer)},
				console: render.NewPlain(&bytes.Buffer{}),
			}

			confirmed, err := a.confirm("Proceed?")
			if err != nil {
				t.Fatalf("confirm() error = %v", err)
			}
			if confirmed != tt.want {
				t.Errorf("confirm(%q) = %v, want %v", tt.answer, confirmed, tt.want)
			}
		})
	}
}

func TestResolveConnectionPrecedence(t *testing.T) {
	t.Setenv("EXAMPLE_DATABASE_DRIVER", "from-env")
	t.Setenv("EXAMPLE_DATABASE_URI", "dsn-from-env")

	t.Run("a flag beats the environment", func(t *testing.T) {
		a := &app{
			opts:        Options{EnvPrefix: "EXAMPLE"},
			driver:      "from-flag",
			databaseURI: "dsn-from-flag",
		}

		driver, dsn, err := a.resolveConnection()
		if err != nil {
			t.Fatalf("resolveConnection() error = %v", err)
		}
		if driver != "from-flag" || dsn != "dsn-from-flag" {
			t.Errorf("resolveConnection() = %q/%q, want the flag values", driver, dsn)
		}
	})

	t.Run("the environment beats the config", func(t *testing.T) {
		a := &app{opts: Options{
			EnvPrefix: "EXAMPLE",
			Config:    func() (string, string, error) { return "from-config", "dsn-from-config", nil },
		}}

		driver, dsn, err := a.resolveConnection()
		if err != nil {
			t.Fatalf("resolveConnection() error = %v", err)
		}
		if driver != "from-env" || dsn != "dsn-from-env" {
			t.Errorf("resolveConnection() = %q/%q, want the environment values", driver, dsn)
		}
	})
}

func TestResolveConnectionFallsBackToConfig(t *testing.T) {
	a := &app{opts: Options{
		EnvPrefix: "UNSET_PREFIX",
		Config:    func() (string, string, error) { return "from-config", "dsn-from-config", nil },
	}}

	driver, dsn, err := a.resolveConnection()
	if err != nil {
		t.Fatalf("resolveConnection() error = %v", err)
	}
	if driver != "from-config" || dsn != "dsn-from-config" {
		t.Errorf("resolveConnection() = %q/%q, want the config values", driver, dsn)
	}
}

func TestResolveConnectionReportsWhatIsMissing(t *testing.T) {
	tests := []struct {
		name   string
		app    *app
		expect string
	}{
		{
			"no driver",
			&app{opts: Options{EnvPrefix: "UNSET_PREFIX"}, databaseURI: "dsn"},
			"UNSET_PREFIX_DATABASE_DRIVER",
		},
		{
			"no DSN",
			&app{opts: Options{EnvPrefix: "UNSET_PREFIX"}, driver: "postgres"},
			"UNSET_PREFIX_DATABASE_URI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.app.resolveConnection()
			if err == nil {
				t.Fatal("resolveConnection() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.expect) {
				t.Errorf("error = %q, want it to name %q", err, tt.expect)
			}
		})
	}
}

// A build embedding no migrations has to say so, rather than reporting an empty
// migration set as an up-to-date database.
func TestResolveFSRequiresAPathWhenNothingIsEmbedded(t *testing.T) {
	a := &app{opts: Options{}}

	_, err := a.resolveFS()
	if err == nil {
		t.Fatal("resolveFS() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "--migrations-path") {
		t.Errorf("error = %q, want it to name --migrations-path", err)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	cmd := New(Options{})

	if cmd.Use != "rung" {
		t.Errorf("Use = %q, want rung", cmd.Use)
	}

	for _, name := range []string{"up", "down", "status", "init"} {
		t.Run(name, func(t *testing.T) {
			found := false
			for _, sub := range cmd.Commands() {
				if sub.Name() == name {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("New() did not register the %q command", name)
			}
		})
	}
}

func TestNewUsesTheGivenNameInHelp(t *testing.T) {
	cmd := New(Options{Use: "migrate", Short: "example tool", EnvPrefix: "EXAMPLE"})

	if !strings.Contains(cmd.Long, "EXAMPLE_DATABASE_URI") {
		t.Errorf("Long = %q, want it to document the environment variables", cmd.Long)
	}
	if !strings.Contains(cmd.Long, "migrate up --force") {
		t.Errorf("Long = %q, want examples using the command's own name", cmd.Long)
	}
}
