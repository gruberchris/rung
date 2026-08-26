package rung

// Reporter receives progress events as migrations are applied or rolled back.
//
// A Migrator narrates through this rather than through a *slog.Logger, so the
// caller decides how progress appears: prose on a terminal, structured records
// in a service log, or nothing at all. A library that logs has already chosen
// its caller's output format.
//
// Implementations must be safe to call with a zero-valued Migration and must
// not retain the value. A Migrator never calls a Reporter concurrently.
type Reporter interface {
	// Applying is called immediately before a migration's up file runs.
	Applying(m Migration)
	// Applied is called after a migration and its ledger row have committed.
	Applied(m Migration)
	// Skipped is called for a migration already recorded in the ledger.
	Skipped(m Migration)
	// RollingBack is called immediately before a migration's down file runs.
	RollingBack(m Migration)
	// RolledBack is called after a rollback and its ledger deletion commit.
	RolledBack(m Migration)
	// StoppedAtTarget is called when Up halts at a version bound, reporting the
	// requested target and the version that was not applied.
	StoppedAtTarget(target, next int)
}

// nopReporter discards every event. It is the default so that a Migrator built
// without a Reporter is silent rather than nil-panicking.
type nopReporter struct{}

func (nopReporter) Applying(Migration)       {}
func (nopReporter) Applied(Migration)        {}
func (nopReporter) Skipped(Migration)        {}
func (nopReporter) RollingBack(Migration)    {}
func (nopReporter) RolledBack(Migration)     {}
func (nopReporter) StoppedAtTarget(int, int) {}
