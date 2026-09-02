package identity

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestMultiVerifierRoutesOnIssuerAndRefusesStrangers(t *testing.T) {
	m := NewMultiVerifier(Config{
		Issuer: "https://a.example", JWKSURL: "https://a.example/jwks",
		Salt:  []byte("s"),
		Extra: []Provider{{Issuer: "https://b.example", JWKSURL: "https://b.example/jwks"}},
	})
	if len(m.byIssuer) != 2 {
		t.Fatalf("want 2 providers, got %d", len(m.byIssuer))
	}
	// A token from an unconfigured issuer is refused by the router itself.
	tok := makeUnsigned(t, `{"iss":"https://evil.example","sub":"x"}`)
	if _, err := m.Verify(context.Background(), tok); err == nil ||
		!strings.Contains(err.Error(), "not a configured provider") {
		t.Fatalf("stranger issuer must be refused by name, got %v", err)
	}
	// Garbage is refused before any network I/O.
	if _, err := m.Verify(context.Background(), "not.a.jwt"); err == nil {
		t.Fatal("garbage must be refused")
	}
	if _, err := m.Verify(context.Background(), "garbage"); err == nil {
		t.Fatal("a non-compact token must be refused")
	}
}

// An extra provider's own audience must reach its verifier, and one without
// an audience must inherit the primary's — eSignet targets access tokens at
// the relying-party client id, and validating them against the dev issuer's
// audience rejected every deployed login (2026-09-02).
func TestExtraProviderCarriesItsOwnAudience(t *testing.T) {
	m := NewMultiVerifier(Config{
		Issuer: "https://a.example", JWKSURL: "https://a.example/jwks",
		Audience: "crest", Salt: []byte("s"),
		Extra: []Provider{
			{Issuer: "https://b.example", JWKSURL: "https://b.example/jwks", Audience: "crest-rp-core-abcd1234"},
			{Issuer: "https://c.example", JWKSURL: "https://c.example/jwks"},
		},
	})
	if got := m.byIssuer["https://b.example"].cfg.Audience; got != "crest-rp-core-abcd1234" {
		t.Fatalf("extra provider audience = %q, want its own", got)
	}
	if got := m.byIssuer["https://c.example"].cfg.Audience; got != "crest" {
		t.Fatalf("audience-less extra provider = %q, want the primary's", got)
	}
}

func makeUnsigned(t *testing.T, payload string) string {
	t.Helper()
	b64 := func(s string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(s))
	}
	return b64(`{"alg":"RS256"}`) + "." + b64(payload) + ".sig"
}
