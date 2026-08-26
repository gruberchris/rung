package render

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gruberchris/rung"
)

var appliedAt = time.Date(2026, 1, 2, 1, 46, 55, 0, time.UTC)

func sampleStatuses() []rung.Status {
	return []rung.Status{
		{Version: 1, Name: "initial_schema", Applied: true, AppliedAt: appliedAt},
		{Version: 3, Name: "expand_clients_add_revoked_tokens"},
	}
}

// The table format is a compatibility contract: it appears in deploy logs that
// people read and in scripts that grep them. This asserts it byte for byte,
// trailing space on a pending row included.
func TestTableFormatIsExact(t *testing.T) {
	var out bytes.Buffer
	if err := Table(&out, sampleStatuses()); err != nil {
		t.Fatalf("Table() error = %v", err)
	}

	want := "Migration Status:\n" +
		"================\n" +
		"Version 1: initial_schema [Applied] 2026-01-02 01:46:55\n" +
		"Version 3: expand_clients_add_revoked_tokens [Pending] \n"

	if got := out.String(); got != want {
		t.Errorf("Table() wrote:\n%q\nwant:\n%q", got, want)
	}
}

func TestTableWithNoStatusesWritesOnlyTheHeader(t *testing.T) {
	var out bytes.Buffer
	if err := Table(&out, nil); err != nil {
		t.Fatalf("Table() error = %v", err)
	}

	want := "Migration Status:\n================\n"
	if got := out.String(); got != want {
		t.Errorf("Table() wrote %q, want %q", got, want)
	}
}

func TestJSONShape(t *testing.T) {
	var out bytes.Buffer
	if err := JSON(&out, sampleStatuses()); err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("JSON() produced invalid JSON: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("JSON() produced %d rows, want 2", len(rows))
	}

	if rows[0]["status"] != "applied" {
		t.Errorf("row 0 status = %v, want applied", rows[0]["status"])
	}
	if rows[0]["applied_at"] != "2026-01-02T01:46:55Z" {
		t.Errorf("row 0 applied_at = %v, want 2026-01-02T01:46:55Z", rows[0]["applied_at"])
	}
	if rows[1]["status"] != "pending" {
		t.Errorf("row 1 status = %v, want pending", rows[1]["status"])
	}
	// A pending row carries no timestamp at all, rather than a zero one.
	if _, present := rows[1]["applied_at"]; present {
		t.Errorf("row 1 carries applied_at = %v, want it omitted", rows[1]["applied_at"])
	}
}

// An empty result must encode as [] so that a consumer can index it without a
// nil check.
func TestJSONEncodesEmptyAsAnArray(t *testing.T) {
	var out bytes.Buffer
	if err := JSON(&out, nil); err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "[]" {
		t.Errorf("JSON() = %q, want %q", got, "[]")
	}
}

func TestConsoleReportsProgress(t *testing.T) {
	var out bytes.Buffer
	console := &Console{Out: &out, NoColor: true, NoEmoji: true}

	migration := rung.Migration{Version: 3, Name: "agent_principals"}
	console.Applying(migration)
	console.Applied(migration)
	console.RollingBack(migration)
	console.RolledBack(migration)
	console.StoppedAtTarget(2, 3)

	got := out.String()
	for _, want := range []string{
		"applying      000003_agent_principals",
		"applied       000003_agent_principals",
		"rolling back  000003_agent_principals",
		"rolled back   000003_agent_principals",
		"stopping at target 2; version 3 and later remain pending",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Console output missing %q; got:\n%s", want, got)
		}
	}
}

// Reporting every previously applied migration on every run buries the ones
// that did something, so it is opt-in.
func TestConsoleSkippedIsQuietUnlessVerbose(t *testing.T) {
	migration := rung.Migration{Version: 1, Name: "initial_schema"}

	var quiet bytes.Buffer
	(&Console{Out: &quiet, NoColor: true, NoEmoji: true}).Skipped(migration)
	if quiet.Len() != 0 {
		t.Errorf("Skipped() wrote %q, want nothing", quiet.String())
	}

	var verbose bytes.Buffer
	(&Console{Out: &verbose, NoColor: true, NoEmoji: true, Verbose: true}).Skipped(migration)
	if !strings.Contains(verbose.String(), "skipped") {
		t.Errorf("verbose Skipped() wrote %q, want it to report the migration", verbose.String())
	}
}

func TestConsoleEmojiCanBeDisabled(t *testing.T) {
	var withEmoji, withoutEmoji bytes.Buffer

	(&Console{Out: &withEmoji, NoColor: true}).Success("✅", "done")
	(&Console{Out: &withoutEmoji, NoColor: true, NoEmoji: true}).Success("✅", "done")

	if !strings.Contains(withEmoji.String(), "✅") {
		t.Errorf("Success() = %q, want the icon", withEmoji.String())
	}
	if strings.Contains(withoutEmoji.String(), "✅") {
		t.Errorf("Success() with NoEmoji = %q, want no icon", withoutEmoji.String())
	}
	// The wording is identical either way; only the decoration differs.
	if !strings.Contains(withoutEmoji.String(), "done") {
		t.Errorf("Success() with NoEmoji = %q, want the message", withoutEmoji.String())
	}
}

// A Console with no Out must not panic; it falls back to standard output.
func TestConsoleZeroValueIsUsable(t *testing.T) {
	console := &Console{}
	if console.Writer() == nil {
		t.Error("Writer() = nil, want a default")
	}
}

func TestSlogReportsStructuredRecords(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))

	reporter := Slog{Logger: logger}
	migration := rung.Migration{Version: 5, Name: "nexwiki_integration"}
	reporter.Applying(migration)
	reporter.Skipped(migration)

	got := out.String()
	if !strings.Contains(got, "version=5") {
		t.Errorf("Slog output = %q, want a version attribute", got)
	}
	if !strings.Contains(got, "name=nexwiki_integration") {
		t.Errorf("Slog output = %q, want a name attribute", got)
	}
}

// Every reporter must satisfy the interface the engine depends on.
func TestReportersImplementTheInterface(t *testing.T) {
	var (
		_ rung.Reporter = (*Console)(nil)
		_ rung.Reporter = Slog{}
		_ rung.Reporter = Nop{}
	)
}

func TestNopWritesNothing(t *testing.T) {
	// Nothing to assert beyond it not panicking: a Nop that did anything would
	// be a Nop that could not be relied on in a test.
	reporter := Nop{}
	migration := rung.Migration{Version: 1, Name: "initial_schema"}
	reporter.Applying(migration)
	reporter.Applied(migration)
	reporter.Skipped(migration)
	reporter.RollingBack(migration)
	reporter.RolledBack(migration)
	reporter.StoppedAtTarget(1, 2)
}
