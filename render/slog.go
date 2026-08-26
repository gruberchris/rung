package render

import (
	"log/slog"

	"github.com/gruberchris/rung"
)

// Slog reports progress as structured log records.
//
// It suits a program whose output is parsed rather than read -- a server
// applying migrations at startup in a development environment, or a job whose
// logs are shipped to an aggregator. For a command somebody runs and watches,
// prefer [Console].
type Slog struct {
	// Logger receives the records. A nil Logger means slog.Default().
	Logger *slog.Logger
}

func (s Slog) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// Applying implements [rung.Reporter].
func (s Slog) Applying(m rung.Migration) {
	s.logger().Info("applying migration", "version", m.Version, "name", m.Name)
}

// Applied implements [rung.Reporter].
func (s Slog) Applied(m rung.Migration) {
	s.logger().Info("migration applied", "version", m.Version, "name", m.Name)
}

// Skipped implements [rung.Reporter]. It logs at debug level, because a
// previously applied migration is the ordinary case on every run after the
// first.
func (s Slog) Skipped(m rung.Migration) {
	s.logger().Debug("migration already applied", "version", m.Version, "name", m.Name)
}

// RollingBack implements [rung.Reporter].
func (s Slog) RollingBack(m rung.Migration) {
	s.logger().Info("rolling back migration", "version", m.Version, "name", m.Name)
}

// RolledBack implements [rung.Reporter].
func (s Slog) RolledBack(m rung.Migration) {
	s.logger().Info("migration rolled back", "version", m.Version, "name", m.Name)
}

// StoppedAtTarget implements [rung.Reporter].
func (s Slog) StoppedAtTarget(target, next int) {
	s.logger().Info("stopping at the requested target", "target", target, "next_version", next)
}
