package adapters

import "net/url"

// redactedURIPlaceholder is returned whenever a connection URI cannot be parsed
// into a form we can prove is credential-free. We never echo the raw input on a
// parse failure, because a malformed-but-credential-bearing string is exactly
// the dangerous case.
const redactedURIPlaceholder = "[redacted-uri]"

// RedactURI returns rawURI with any userinfo (the "user:password@" portion)
// stripped, leaving scheme, host, port, and path intact, so a datastore
// connection URI can be safely formatted into error and status messages.
//
// If the input does not parse as a URI with an identifiable host, RedactURI
// returns a credential-free placeholder rather than the raw input — it never
// falls back to echoing a string that might still embed credentials.
func RedactURI(rawURI string) string {
	u, err := url.Parse(rawURI)
	if err != nil || u.Host == "" {
		// We could not safely isolate a host, so we cannot prove any
		// remaining substring is credential-free. Refuse to echo it.
		return redactedURIPlaceholder
	}
	u.User = nil
	return u.String()
}
