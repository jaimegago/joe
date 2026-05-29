package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// newTestRepo builds a real SQL auth Repository over an in-memory SQLite store
// with migration 014 applied. Using the real repo (not a mock) exercises the
// actual SQL and the session/flow lifetime semantics end-to-end.
func newTestRepo(t *testing.T) (*SQLRepository, *store.Store) {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewRepository(s.DB(), s.Driver()), s
}

// fakeProvider is a test double for the OIDC Provider. It records the nonce
// handed to AuthCodeURL and echoes it back from Verify — simulating an IdP that
// reflects the nonce into the ID token — so a full login→callback flow passes
// the nonce check without a live issuer.
type fakeProvider struct {
	lastNonce   string
	claims      Claims
	authURLErr  error
	exchangeErr error
	verifyErr   error
}

func (f *fakeProvider) AuthCodeURL(_ context.Context, state, nonce, _ string) (string, error) {
	f.lastNonce = nonce
	if f.authURLErr != nil {
		return "", f.authURLErr
	}
	return "https://idp.example/authorize?state=" + state, nil
}

func (f *fakeProvider) Exchange(_ context.Context, _ /*code*/, _ /*verifier*/ string) (string, error) {
	if f.exchangeErr != nil {
		return "", f.exchangeErr
	}
	return "raw-id-token", nil
}

func (f *fakeProvider) Verify(_ context.Context, _ string) (*VerifiedToken, error) {
	if f.verifyErr != nil {
		return nil, f.verifyErr
	}
	return &VerifiedToken{Claims: f.claims, Nonce: f.lastNonce}, nil
}

// cookieByName returns the first Set-Cookie with the given name, or nil.
func cookieByName(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
