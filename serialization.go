package mailkube

import (
	"net/url"
	"time"
)

// itemPath returns the path of one item under a collection, with the identifier escaped.
//
// Escaping is not cosmetic: an identifier carrying an encoded "?" or "/" would otherwise
// re-target the request at a different route. url.PathEscape escapes both, and url.URL keeps
// the escaped form through resolution, so the identifier stays one path segment.
func itemPath(base, identifier string) string {
	return base + "/" + url.PathEscape(identifier)
}

// renderTime renders an instant for the wire as RFC 3339, or "" for the zero time.
//
// One home for the "how does an instant become a string" decision, shared by every verb that
// takes one. It does not validate: an instant in the past or outside the scheduling horizon is
// rejected by the server, which is the authority on what a value means.
func renderTime(instant time.Time) string {
	if instant.IsZero() {
		return ""
	}
	return instant.Format(time.RFC3339)
}
