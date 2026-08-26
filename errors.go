package rung

import "errors"

// ErrNothingToRollback reports an exhausted ledger.
//
// It is an error rather than a silent success so that a caller rolling back
// repeatedly -- "undo two more", "undo everything" -- has a way to know it has
// finished. Treating an empty ledger as success gives such a loop no
// termination condition.
var ErrNothingToRollback = errors.New("no migrations to roll back")
