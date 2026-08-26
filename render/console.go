// Package render turns migration progress and status into output.
//
// It exists so that the engine in the parent package reports events without
// deciding how they look. A [Console] writes prose for a person, [Slog] writes
// structured records for a service log, and [Nop] discards everything.
package render

import (
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"

	"github.com/gruberchris/rung"
)

// Console reports progress as prose, with optional colour and emoji.
//
// The zero value is usable and writes decorated output to os.Stdout. Colour is
// additionally suppressed by the fatih/color package whenever the destination
// is not a terminal, which is what makes the same output readable in a terminal
// and in a CI log.
type Console struct {
	// Out receives all output. Defaults to os.Stdout.
	Out io.Writer
	// NoColor suppresses ANSI colour even on a terminal.
	NoColor bool
	// NoEmoji suppresses the leading icons.
	NoEmoji bool
	// Verbose additionally reports migrations skipped as already applied.
	Verbose bool
}

// NewConsole returns a Console writing to out, with colour and emoji enabled.
// A nil out means os.Stdout.
func NewConsole(out io.Writer) *Console { return &Console{Out: out} }

// NewPlain returns a Console writing to out with colour and emoji disabled.
// The wording is identical to [NewConsole]. A nil out means os.Stdout.
func NewPlain(out io.Writer) *Console {
	return &Console{Out: out, NoColor: true, NoEmoji: true}
}

var (
	successAttrs = []color.Attribute{color.FgGreen, color.Bold}
	errorAttrs   = []color.Attribute{color.FgRed, color.Bold}
	warnAttrs    = []color.Attribute{color.FgYellow, color.Bold}
	infoAttrs    = []color.Attribute{color.FgCyan}
	detailAttrs  = []color.Attribute{color.Faint}
)

// Writer returns the destination all output is written to, resolving a nil Out
// to os.Stdout. It is exported so that callers can direct other output, such as
// a status table, to the same place.
func (c *Console) Writer() io.Writer {
	if c.Out != nil {
		return c.Out
	}
	return os.Stdout
}

func (c *Console) writer() io.Writer { return c.Writer() }

func (c *Console) paint(attrs []color.Attribute) *color.Color {
	painter := color.New(attrs...)
	if c.NoColor {
		painter.DisableColor()
	}
	return painter
}

// line writes one decorated line: an optional icon, then the formatted text.
func (c *Console) line(attrs []color.Attribute, icon, format string, a ...any) {
	text := fmt.Sprintf(format, a...)
	if icon != "" && !c.NoEmoji {
		text = icon + " " + text
	}
	_, _ = c.paint(attrs).Fprintln(c.writer(), text)
}

// Info writes an informational line, such as a step beginning.
func (c *Console) Info(icon, format string, a ...any) { c.line(infoAttrs, icon, format, a...) }

// Success writes a line reporting that something completed.
func (c *Console) Success(icon, format string, a ...any) { c.line(successAttrs, icon, format, a...) }

// Warn writes a line drawing attention to a consequence.
func (c *Console) Warn(icon, format string, a ...any) { c.line(warnAttrs, icon, format, a...) }

// Error writes a line reporting a failure.
func (c *Console) Error(icon, format string, a ...any) { c.line(errorAttrs, icon, format, a...) }

// Printf writes undecorated text exactly as given.
func (c *Console) Printf(format string, a ...any) {
	_, _ = fmt.Fprintf(c.writer(), format, a...)
}

// Blank writes an empty line.
func (c *Console) Blank() { _, _ = fmt.Fprintln(c.writer()) }

// detail writes an indented per-migration line.
func (c *Console) detail(attrs []color.Attribute, verb string, m rung.Migration) {
	_, _ = c.paint(attrs).Fprintf(c.writer(), "  %-13s %s\n", verb, m)
}

// Applying implements [rung.Reporter].
func (c *Console) Applying(m rung.Migration) { c.detail(infoAttrs, "applying", m) }

// Applied implements [rung.Reporter].
func (c *Console) Applied(m rung.Migration) { c.detail(successAttrs, "applied", m) }

// Skipped implements [rung.Reporter]. It writes nothing unless Verbose is set,
// because reporting every previously applied migration on every run buries the
// ones that did something.
func (c *Console) Skipped(m rung.Migration) {
	if c.Verbose {
		c.detail(detailAttrs, "skipped", m)
	}
}

// RollingBack implements [rung.Reporter].
func (c *Console) RollingBack(m rung.Migration) { c.detail(warnAttrs, "rolling back", m) }

// RolledBack implements [rung.Reporter].
func (c *Console) RolledBack(m rung.Migration) { c.detail(successAttrs, "rolled back", m) }

// StoppedAtTarget implements [rung.Reporter].
func (c *Console) StoppedAtTarget(target, next int) {
	c.paintf(warnAttrs, "  stopping at target %d; version %d and later remain pending\n", target, next)
}

func (c *Console) paintf(attrs []color.Attribute, format string, a ...any) {
	_, _ = c.paint(attrs).Fprintf(c.writer(), format, a...)
}
