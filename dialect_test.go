package rung

import (
	"slices"
	"strings"
	"testing"
)

func TestRegisterAndFor(t *testing.T) {
	d := testDialect{dir: "example"}
	Register(d, "Example", "  example-alias  ")

	for _, name := range []string{"example", "EXAMPLE", " example ", "example-alias"} {
		t.Run(name, func(t *testing.T) {
			got, err := For(name)
			if err != nil {
				t.Fatalf("For(%q) error = %v", name, err)
			}
			if got.MigrationsDir() != "example" {
				t.Errorf("For(%q) returned the wrong dialect", name)
			}
		})
	}
}

func TestForRejectsAnUnregisteredDriver(t *testing.T) {
	Register(testDialect{dir: "listed"}, "listed-driver")

	_, err := For("sqlite")
	if err == nil {
		t.Fatal("For() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "sqlite") {
		t.Errorf("error = %q, want it to name the driver asked for", err)
	}
	// The message has to say what *is* available, or the reader has no next
	// step beyond guessing.
	if !strings.Contains(err.Error(), "listed-driver") {
		t.Errorf("error = %q, want it to list the registered drivers", err)
	}
}

func TestNamesIncludesAliasesSorted(t *testing.T) {
	Register(testDialect{dir: "sorted"}, "zeta-driver", "alpha-driver")

	names := Names()
	if !slices.IsSorted(names) {
		t.Errorf("Names() = %v, want sorted", names)
	}
	for _, want := range []string{"alpha-driver", "zeta-driver"} {
		if !slices.Contains(names, want) {
			t.Errorf("Names() = %v, want it to contain %q", names, want)
		}
	}
}

// Registering the same name twice is a programming error caught at startup
// rather than a silent last-one-wins.
func TestRegisterPanicsOnDuplicates(t *testing.T) {
	Register(testDialect{dir: "first"}, "duplicate-driver")

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("Register() did not panic on a duplicate name")
		}
		if message, ok := recovered.(string); ok && !strings.Contains(message, "duplicate-driver") {
			t.Errorf("panic = %q, want it to name the duplicate", message)
		}
	}()

	Register(testDialect{dir: "second"}, "duplicate-driver")
}

func TestRegisterPanicsOnBadArguments(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		names   []string
	}{
		{"a nil dialect", nil, []string{"nil-dialect"}},
		{"no names", testDialect{}, nil},
		{"an empty name", testDialect{}, []string{"   "}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Register() did not panic")
				}
			}()
			Register(tt.dialect, tt.names...)
		})
	}
}

func TestRebindDollar(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"no placeholders", "SELECT 1", "SELECT 1"},
		{"one", "DELETE FROM t WHERE version = ?", "DELETE FROM t WHERE version = $1"},
		{
			"several",
			"INSERT INTO t (a, b, c) VALUES (?, ?, ?)",
			"INSERT INTO t (a, b, c) VALUES ($1, $2, $3)",
		},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RebindDollar(tt.query); got != tt.want {
				t.Errorf("RebindDollar(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

// Ordinals run past nine, where the placeholder stops being a single character.
func TestRebindDollarNumbersBeyondNine(t *testing.T) {
	query := strings.Repeat("?,", 11)
	got := RebindDollar(query)

	if !strings.Contains(got, "$10,") || !strings.Contains(got, "$11,") {
		t.Errorf("RebindDollar(%q) = %q, want two-digit ordinals", query, got)
	}
}

func TestRebindQuestionIsIdentity(t *testing.T) {
	query := "INSERT INTO t (a, b) VALUES (?, ?)"
	if got := RebindQuestion(query); got != query {
		t.Errorf("RebindQuestion(%q) = %q, want it unchanged", query, got)
	}
}
