package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/jaimegago/joe/internal/config"
)

// ErrOIDCUnavailable is returned when OIDC discovery has not (yet) succeeded —
// e.g. the IdP is unreachable. Handlers surface this as 503 so existing
// sessions keep working while only new logins fail (design §4: IdP unreachable).
var ErrOIDCUnavailable = errors.New("auth: OIDC provider unavailable")

// VerifiedToken is the result of verifying an ID token: the decoded claims plus
// the token's nonce (echoed back by the IdP), which the callback compares to
// the nonce it issued during login.
type VerifiedToken struct {
	Claims Claims
	Nonce  string
}

// Provider is the IdP-facing seam: it builds the authorization URL, completes
// the PKCE token exchange, and verifies the returned ID token against the
// issuer's JWKS. It is an interface so the OIDC flow is testable without a live
// issuer — the acceptance tests inject a fake that returns chosen claims.
//
// Signature verification and JWKS handling are NOT hand-rolled: the real
// implementation delegates to github.com/coreos/go-oidc/v3 (verification) and
// golang.org/x/oauth2 (auth-code + PKCE), per design §2.1.
type Provider interface {
	// AuthCodeURL returns the issuer's authorization URL for an auth-code+PKCE
	// flow carrying the given state, OIDC nonce, and PKCE code verifier (the
	// S256 challenge is derived from the verifier).
	AuthCodeURL(ctx context.Context, state, nonce, codeVerifier string) (string, error)
	// Exchange swaps the callback authorization code (with the PKCE verifier)
	// for the raw ID token string.
	Exchange(ctx context.Context, code, codeVerifier string) (rawIDToken string, err error)
	// Verify validates the raw ID token against the issuer's JWKS (signature,
	// issuer, audience, expiry) and returns the decoded claims and nonce.
	Verify(ctx context.Context, rawIDToken string) (*VerifiedToken, error)
}

// oidcProvider is the production Provider. Discovery (oidc.NewProvider) is a
// network call, so it is performed lazily on first use and cached: startup must
// not hard-depend on the IdP being reachable (design §4). Concurrent first-use
// is serialised by mu.
type oidcProvider struct {
	cfg config.OIDCConfig

	mu        sync.Mutex
	ready     bool
	oauth2Cfg *oauth2.Config
	verifier  *oidc.IDTokenVerifier
}

// NewOIDCProvider builds the production Provider from config. It does NOT
// perform discovery here; the first request that needs the issuer triggers it.
func NewOIDCProvider(cfg config.OIDCConfig) Provider {
	return &oidcProvider{cfg: cfg}
}

// ensure performs (once) the OIDC discovery and builds the oauth2 config and
// verifier. Safe for concurrent callers.
func (p *oidcProvider) ensure(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready {
		return nil
	}
	provider, err := oidc.NewProvider(ctx, p.cfg.Issuer)
	if err != nil {
		return fmt.Errorf("%w: discovery failed for issuer %q: %v", ErrOIDCUnavailable, p.cfg.Issuer, err)
	}
	p.oauth2Cfg = &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		RedirectURL:  p.cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email"},
	}
	p.verifier = provider.Verifier(&oidc.Config{ClientID: p.cfg.ClientID})
	p.ready = true
	return nil
}

func (p *oidcProvider) AuthCodeURL(ctx context.Context, state, nonce, codeVerifier string) (string, error) {
	if err := p.ensure(ctx); err != nil {
		return "", err
	}
	return p.oauth2Cfg.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(codeVerifier),
	), nil
}

func (p *oidcProvider) Exchange(ctx context.Context, code, codeVerifier string) (string, error) {
	if err := p.ensure(ctx); err != nil {
		return "", err
	}
	tok, err := p.oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return "", fmt.Errorf("auth: token exchange failed: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", errors.New("auth: token response has no id_token")
	}
	return raw, nil
}

func (p *oidcProvider) Verify(ctx context.Context, rawIDToken string) (*VerifiedToken, error) {
	if err := p.ensure(ctx); err != nil {
		return nil, err
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("auth: id token verification failed: %w", err)
	}
	var c Claims
	if err := idToken.Claims(&c); err != nil {
		return nil, fmt.Errorf("auth: decode id token claims: %w", err)
	}
	return &VerifiedToken{Claims: c, Nonce: idToken.Nonce}, nil
}
