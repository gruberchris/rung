package clicmd

import (
	"net/url"
	"strings"
)

// redacted replaces a password wherever one is shown.
const redacted = "xxxxx"

// maskDSN removes the password from a DSN so it can be printed.
//
// Two shapes are handled, because the two supported drivers do not agree on
// one: a URL such as postgres://user:secret@host/db, and MySQL's
// user:secret@tcp(host:3306)/db, which is not a URL at all. Anything else is
// returned unchanged rather than guessed at -- printing a DSN with the password
// still in it would be worse than printing nothing useful.
func maskDSN(dsn string) string {
	if dsn == "" {
		return ""
	}

	if parsed, err := url.Parse(dsn); err == nil && parsed.Scheme != "" && parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), redacted)
		}
		return parsed.String()
	}

	// MySQL DSN: everything before the last @ is the credential.
	at := strings.LastIndex(dsn, "@")
	if at <= 0 {
		return dsn
	}
	colon := strings.Index(dsn[:at], ":")
	if colon < 0 {
		return dsn
	}
	return dsn[:colon+1] + redacted + dsn[at:]
}
