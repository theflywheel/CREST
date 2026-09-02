package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// TokenVerifier is what the middleware needs from a verifier. Verifier and
// MultiVerifier both satisfy it.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (Caller, error)
}

// MultiVerifier accepts tokens from more than one issuer — the migration
// shape #155 needs, where eSignet authenticates the doors while the
// externally-shared PoC still logs in through the dev issuer. Each issuer
// keeps its own Verifier with its own JWKS; nothing about any single issuer's
// checking is relaxed.
//
// Routing reads the token's `iss` claim before verification. That is safe
// because it only CHOOSES which verifier runs: the chosen verifier still
// enforces signature, issuer equality, audience, and lifetime in full. A
// token lying about its issuer selects a verifier that will reject it.
type MultiVerifier struct {
	byIssuer map[string]*Verifier
	issuers  []string
}

// NewMultiVerifier builds one Verifier per provider in the config.
func NewMultiVerifier(cfg Config) *MultiVerifier {
	m := &MultiVerifier{byIssuer: map[string]*Verifier{}}
	add := func(c Config) {
		m.byIssuer[c.Issuer] = NewVerifier(c)
		m.issuers = append(m.issuers, c.Issuer)
	}
	add(cfg)
	for _, p := range cfg.Extra {
		c := cfg // extras share audience, salt and cache posture
		c.Issuer, c.JWKSURL, c.Extra = p.Issuer, p.JWKSURL, nil
		add(c)
	}
	return m
}

// Verify routes on the unverified iss claim and hands the token to that
// issuer's verifier.
func (m *MultiVerifier) Verify(ctx context.Context, token string) (Caller, error) {
	iss, err := unverifiedIssuer(token)
	if err != nil {
		return Caller{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	v, ok := m.byIssuer[iss]
	if !ok {
		return Caller{}, fmt.Errorf("%w: issued by %q, which is not a configured provider", ErrInvalidToken, iss)
	}
	return v.Verify(ctx, token)
}

// unverifiedIssuer reads iss from the payload without checking anything —
// see the routing note on MultiVerifier for why that is sound here.
func unverifiedIssuer(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("not a compact JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("payload is not base64url: %w", err)
	}
	var c struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return "", fmt.Errorf("payload is not JSON: %w", err)
	}
	if c.Issuer == "" {
		return "", fmt.Errorf("no issuer claim")
	}
	return c.Issuer, nil
}
