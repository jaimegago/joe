package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jaimegago/joe/internal/rbac"
)

// ErrEmailNotVerified is returned when an ID token's email_verified claim is
// absent or not true. Keying a principal on an unverified email would be an
// impersonation vector, so this is a hard rejection, not a warning
// (design §2.2: "Reject tokens where email_verified is absent or false").
var ErrEmailNotVerified = errors.New("auth: id token email is not verified")

// ErrNoEmail is returned when a verified token carries no email claim.
var ErrNoEmail = errors.New("auth: id token has no email claim")

// Claims is the subset of OIDC ID-token claims Joe consumes. The principal is
// keyed on the verified email (design §2.2); sub is retained for diagnostics
// only (Joe does not key on the opaque subject in v1).
type Claims struct {
	Email         string   `json:"email"`
	EmailVerified flexBool `json:"email_verified"`
	Subject       string   `json:"sub"`
}

// PrincipalFromClaims enforces email_verified == true and mints the
// user:<email> principal. It is the single point where verified OIDC identity
// becomes an rbac.Principal entering the context path. The order matters: the
// email_verified gate runs BEFORE any principal is constructed, so an
// unverified token can never yield a principal.
func PrincipalFromClaims(c Claims) (rbac.Principal, error) {
	if !bool(c.EmailVerified) {
		return "", ErrEmailNotVerified
	}
	if c.Email == "" {
		return "", ErrNoEmail
	}
	return rbac.UserPrincipal(c.Email)
}

// flexBool decodes a JSON boolean that some IdPs (notably Azure AD) historically
// encode as the string "true"/"false" rather than a native bool. Anything that
// is not an explicit true (bool true or string "true") decodes to false, which
// — combined with the email_verified gate above — fails closed: an absent,
// malformed, or string-"false" claim is treated as unverified.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	// Native boolean.
	var native bool
	if err := json.Unmarshal(data, &native); err == nil {
		*b = flexBool(native)
		return nil
	}
	// String-encoded boolean.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		parsed, perr := strconv.ParseBool(s)
		if perr != nil {
			// Unrecognised string ⇒ fail closed (unverified).
			*b = false
			return nil
		}
		*b = flexBool(parsed)
		return nil
	}
	return fmt.Errorf("auth: email_verified claim is neither bool nor string: %s", string(data))
}
