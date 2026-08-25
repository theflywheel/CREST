package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/theflywheel/crest/pkg/identity"
)

// Caller is who a request is from (#89).
//
// A token, and optionally a party it is acting for. The two together are the
// assisted case — a supervisor confirming for a worker with no phone — and the
// reason it is expressible here at all is that it has to be exercised: an
// assisted path nobody tests is one that gets closed off by the next person
// tightening authentication, and closing it takes the confirmation route away
// from the workers who most need it.
type Caller struct {
	Token      string
	OnBehalfOf string
}

func (c Caller) header() http.Header {
	if c.Token == "" && c.OnBehalfOf == "" {
		return nil
	}
	h := http.Header{}
	if c.Token != "" {
		h.Set("Authorization", "Bearer "+c.Token)
	}
	if c.OnBehalfOf != "" {
		h.Set(identity.HeaderOnBehalfOf, c.OnBehalfOf)
	}
	return h
}

// OIDC is the identity provider the stack authenticates against.
//
// In the local stack that is tools/mocks/oidc, which mints real ES256 tokens
// against a real JWKS. There is no bypass to reach for here because there is no
// bypass in pkg/identity: the suite proves the production verification path or
// it proves nothing.
type OIDC struct {
	Base     string
	Issuer   string
	Audience string
	Salt     []byte
	http     *http.Client
}

// NewOIDC builds a client for the provider, reading the same environment the
// services read so the salt and issuer cannot disagree with theirs. A
// disagreement would show up as every token being valid and nobody being
// anybody, which is a confusing way to spend a morning.
func NewOIDC() *OIDC {
	return &OIDC{
		Base:     env("OIDC_URL", "http://localhost:59103"),
		Issuer:   env("CREST_OIDC_ISSUER", "http://mock-oidc:8080"),
		Audience: env("CREST_OIDC_AUDIENCE", "crest"),
		Salt: []byte(env("CREST_SUBJECT_SALT",
			"local-development-subject-salt-not-for-any-real-deployment")),
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

// Subject is the value a Party must be bound to for a token with this provider
// subject to resolve to them.
//
// The harness computes it exactly as the services do, through pkg/identity, so
// that a change to the salting can never leave the suite testing the old rule
// against the new code.
func (o *OIDC) Subject(providerSub string) string {
	return identity.Pairwise(o.Salt, o.Issuer, providerSub)
}

// Token mints an access token for a provider subject.
//
// The lifetime is long on purpose. Scenarios advance CREST's clock by weeks,
// and the identity provider's clock is not CREST's — a real one would not take
// instructions from a test harness — so a token has to outlive the whole run
// rather than the seven days a window does.
func (o *OIDC) Token(ctx context.Context, providerSub string) (string, error) {
	return o.token(ctx, map[string]any{
		"sub": providerSub, "aud": o.Audience, "expiresIn": "8760h",
	})
}

// TokenWith mints a token from explicit claims, for the scenarios about tokens
// that are wrong in a particular way rather than about the work.
func (o *OIDC) TokenWith(ctx context.Context, claims map[string]any) (string, error) {
	return o.token(ctx, claims)
}

func (o *OIDC) token(ctx context.Context, body map[string]any) (string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.Base+"/token", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mock oidc %s/token -> %d: %s", o.Base, resp.StatusCode, out)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(out, &tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("mock oidc returned no access token")
	}
	return tok.AccessToken, nil
}

// WaitReady polls the provider until it serves a key set.
func (o *OIDC) WaitReady(ctx context.Context, within time.Duration) error {
	deadline := time.Now().Add(within)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			o.Base+"/.well-known/jwks.json", nil)
		if err != nil {
			return err
		}
		resp, err := o.http.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the identity provider at %s never served a key set", o.Base)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
