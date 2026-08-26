package clicmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// confirm asks before a destructive or irreversible step.
//
// --force skips the question, which CI needs: there is no terminal there. When
// there is no terminal and no --force, this refuses with an error rather than
// blocking or guessing.
//
// Treating an unanswerable prompt as "no" is what turns a forgotten --force
// into a green pipeline over an un-migrated database: the tool prints
// "cancelled", exits successfully, and the deploy carries on against a schema
// that was never updated. Silence is therefore a failure, and only a person
// present at a terminal can decline.
//
// The terminal test is term.IsTerminal rather than a check for a character
// device, because /dev/null is a character device: `migrate up < /dev/null`
// would otherwise pass the check, read EOF, and be taken as a refusal.
func (a *app) confirm(question string) (bool, error) {
	if a.force {
		return true, nil
	}

	in := a.stdin()
	if file, ok := in.(*os.File); ok && !term.IsTerminal(int(file.Fd())) {
		return false, notATerminal(question)
	}

	a.console.Printf("%s [y/N]: ", question)

	var answer string
	if _, err := fmt.Fscanln(in, &answer); err != nil {
		if errors.Is(err, io.EOF) {
			// Nobody answered and nobody can.
			return false, notATerminal(question)
		}
		// A bare newline is a refusal by somebody who is present.
		return false, nil
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func notATerminal(question string) error {
	return fmt.Errorf(
		"%s: stdin is not a terminal, so there is nobody to ask; pass --force to proceed anyway",
		question)
}

func (a *app) stdin() FileReader {
	if a.opts.Stdin != nil {
		return a.opts.Stdin
	}
	return os.Stdin
}
