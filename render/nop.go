package render

import "github.com/gruberchris/rung"

// Nop discards every event. It is useful in tests and in programs that report
// progress some other way.
type Nop struct{}

// Applying implements [rung.Reporter].
func (Nop) Applying(rung.Migration) {}

// Applied implements [rung.Reporter].
func (Nop) Applied(rung.Migration) {}

// Skipped implements [rung.Reporter].
func (Nop) Skipped(rung.Migration) {}

// RollingBack implements [rung.Reporter].
func (Nop) RollingBack(rung.Migration) {}

// RolledBack implements [rung.Reporter].
func (Nop) RolledBack(rung.Migration) {}

// StoppedAtTarget implements [rung.Reporter].
func (Nop) StoppedAtTarget(int, int) {}
