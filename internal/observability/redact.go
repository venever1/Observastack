package observability

import (
	"strings"
	"unicode/utf8"
)

// MaskEmail redacts the local part of an email address so a log entry can
// identify its subject without recording the full address.
//
// Only the first character of the local part and the domain are kept, so
// "alice.private@example.com" becomes "a***@example.com". Values with no usable
// local part collapse to "***".
//
// This is for identifiers only. Credentials must never be logged at all, not
// even partially masked.
func MaskEmail(email string) string {
	at := strings.IndexByte(email, '@')
	if at <= 0 {
		return "***"
	}

	// Decode the first character as a rune rather than slicing a single byte:
	// for a multi-byte character a byte slice yields invalid UTF-8, which
	// would mangle the resulting log line.
	first, size := utf8.DecodeRuneInString(email[:at])
	if first == utf8.RuneError || size == 0 {
		return "***"
	}

	return string(first) + "***" + email[at:]
}
