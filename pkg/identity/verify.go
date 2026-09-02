package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/theflywheel/crest/pkg/clock"
)

// ErrInvalidToken is every reason a token was not accepted, wrapped.
//
// One error rather than a taxonomy, and the detail goes to the log rather than
// to the caller. "expired" and "wrong signature" are different facts to an
// operator and the same fact to whoever is holding a token they should not
// have; telling them which one they got is free reconnaissance.
var ErrInvalidToken = errors.New("identity: token rejected")

// allowedAlgs is the signature allowlist, and it is passed to the parser
// rather than read from the token.
//
// This is the alg-confusion defence, and it is structural rather than a check
// somebody has to remember: go-jose v4 will not even parse a token whose alg
// is outside this list, so `none` and a symmetric algorithm verified against a
// public key are both unrepresentable rather than caught.
var allowedAlgs = []jose.SignatureAlgorithm{jose.RS256, jose.ES256, jose.PS256}

// Verifier checks access tokens against an issuer's published keys.
type Verifier struct {
	cfg  Config
	clk  clock.Clock
	http *http.Client

	mu        sync.RWMutex
	keys      *jose.JSONWebKeySet
	fetchedAt time.Time
}

// NewVerifier builds a verifier. It does not fetch keys: a service must be
// able to start when its identity provider is briefly down, and a token
// arriving before the first successful fetch is rejected rather than accepted.
//
// It takes no clock, and that is deliberate rather than an omission.
//
// CREST services outside production can be handed their time, because the
// confirmation window is seven days and a harness that waits one is not a
// harness (pkg/service.chooseClock). A token's lifetime is not that kind of
// time. It is the identity provider's judgement, made in real time, about how
// long somebody's session should last — and checking it against a clock that
// something else can move means a clock set to last March makes every expired
// token valid again. Whoever can drive the clock can then log in as anybody
// who ever held a token.
//
// So the verifier reads the wall clock, always, in every environment. The one
// visible consequence is that a scenario advancing CREST twenty days into the
// future still holds tokens that have not expired, which is exactly what would
// happen against a real provider.
func NewVerifier(cfg Config) *Verifier {
	return &Verifier{
		cfg:  cfg,
		clk:  clock.System{},
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// claims is the subset of a token this system reads. Everything else about the
// principal — their name, their contact details — belongs in the registry
// under consent, not in an access token being used as a profile.
type claims struct {
	Issuer    string   `json:"iss"`
	Subject   string   `json:"sub"`
	Audience  audience `json:"aud"`
	ExpiresAt int64    `json:"exp"`
	NotBefore int64    `json:"nbf"`
}

// audience is `aud`, which JWT allows to be either a string or an array of
// them. Both are seen in the wild and a decoder that handles one produces a
// confusing failure against the other.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a audience) has(want string) bool {
	for _, v := range a {
		if v == want {
			return true
		}
	}
	return false
}

// Verify checks a bearer token and returns the caller it establishes.
//
// The returned Caller has no PartyID: binding a subject to a Party is the
// registry's answer, not the token's, and a token that could assert a CREST
// party id would be an identity provider deciding who somebody is inside a
// system it does not run.
func (v *Verifier) Verify(ctx context.Context, token string) (Caller, error) {
	sig, err := jose.ParseSigned(token, allowedAlgs)
	if err != nil {
		return Caller{}, fmt.Errorf("%w: unparseable: %w", ErrInvalidToken, err)
	}
	if len(sig.Signatures) != 1 {
		// A token with no signature is unsigned; one with several invites the
		// reader to accept whichever verifies, which is a different token to
		// the one the issuer meant.
		return Caller{}, fmt.Errorf("%w: expected exactly one signature, got %d",
			ErrInvalidToken, len(sig.Signatures))
	}
	kid := sig.Signatures[0].Header.KeyID

	payload, err := v.verifyWith(ctx, sig, kid)
	if err != nil {
		return Caller{}, err
	}

	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Caller{}, fmt.Errorf("%w: claims are not JSON: %w", ErrInvalidToken, err)
	}
	if c.Issuer != v.cfg.Issuer {
		return Caller{}, fmt.Errorf("%w: issued by %q, not %q", ErrInvalidToken, c.Issuer, v.cfg.Issuer)
	}
	if v.cfg.Audience != "" && !c.Audience.has(v.cfg.Audience) {
		// Refusing a token minted for somebody else is what stops a token this
		// deployment was handed for one purpose being replayed into another.
		return Caller{}, fmt.Errorf("%w: audience %v does not include %q",
			ErrInvalidToken, []string(c.Audience), v.cfg.Audience)
	}
	if c.Subject == "" {
		return Caller{}, fmt.Errorf("%w: no subject", ErrInvalidToken)
	}

	now := v.clk.Now()
	if c.ExpiresAt == 0 {
		// A token that never expires is a credential somebody keeps. Refused
		// rather than defaulted to some lifetime of our choosing.
		return Caller{}, fmt.Errorf("%w: no expiry", ErrInvalidToken)
	}
	exp := time.Unix(c.ExpiresAt, 0)
	if now.After(exp.Add(v.cfg.Leeway)) {
		return Caller{}, fmt.Errorf("%w: expired at %s", ErrInvalidToken, exp.UTC().Format(time.RFC3339))
	}
	if c.NotBefore != 0 {
		nbf := time.Unix(c.NotBefore, 0)
		if now.Add(v.cfg.Leeway).Before(nbf) {
			return Caller{}, fmt.Errorf("%w: not valid until %s", ErrInvalidToken, nbf.UTC().Format(time.RFC3339))
		}
	}

	return Caller{
		Subject:   Pairwise(v.cfg.Salt, c.Issuer, c.Subject),
		Issuer:    c.Issuer,
		ExpiresAt: exp,
	}, nil
}

// verifyWith finds the signing key and checks the signature, refreshing the
// key set once if the kid is unknown.
//
// The refresh is what makes issuer key rotation survivable without a restart.
// It is rate-limited because an unknown kid is also exactly what a forged token
// looks like, and an unlimited refresh turns each one into a request to the
// identity provider — a way to make this service the instrument of an outage
// somewhere else.
func (v *Verifier) verifyWith(ctx context.Context, sig *jose.JSONWebSignature, kid string) ([]byte, error) {
	keys, err := v.keySet(ctx, false)
	if err != nil {
		return nil, err
	}
	if payload, ok := tryKeys(sig, keys, kid); ok {
		return payload, nil
	}

	v.mu.RLock()
	stale := v.clk.Now().Sub(v.fetchedAt) >= v.cfg.MinRefresh
	v.mu.RUnlock()
	if !stale {
		return nil, fmt.Errorf("%w: no known key signed it", ErrInvalidToken)
	}
	keys, err = v.keySet(ctx, true)
	if err != nil {
		return nil, err
	}
	if payload, ok := tryKeys(sig, keys, kid); ok {
		return payload, nil
	}
	return nil, fmt.Errorf("%w: no known key signed it", ErrInvalidToken)
}

// tryKeys verifies against the named key, or against every key when the token
// names none.
//
// Trying every key is not a weakening: the signature still has to verify, and
// an issuer entitled to sign with any of its published keys is entitled to
// omit the kid. What would be a weakening is trusting the kid to pick a key of
// a different type than the alg — which ParseSigned's allowlist already makes
// impossible.
func tryKeys(sig *jose.JSONWebSignature, keys *jose.JSONWebKeySet, kid string) ([]byte, bool) {
	candidates := keys.Keys
	if kid != "" {
		candidates = keys.Key(kid)
	}
	for _, k := range candidates {
		// A key published for encryption is not a key to check a signature
		// with, whatever the token says.
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		if payload, err := sig.Verify(k); err == nil {
			return payload, true
		}
	}
	return nil, false
}

// keySet returns the cached JWKS, fetching if it is empty or if forced.
func (v *Verifier) keySet(ctx context.Context, force bool) (*jose.JSONWebKeySet, error) {
	v.mu.RLock()
	cached, at := v.keys, v.fetchedAt
	v.mu.RUnlock()
	if cached != nil && !force && v.clk.Now().Sub(at) < v.cfg.CacheFor {
		return cached, nil
	}

	fetched, err := v.fetch(ctx)
	if err != nil {
		// A provider that is briefly unreachable must not invalidate tokens
		// that were already verifiable. Serving a stale key set is the honest
		// failure here: the keys have not changed, the network has.
		if cached != nil {
			return cached, nil
		}
		return nil, fmt.Errorf("identity: no signing keys available: %w", err)
	}
	v.mu.Lock()
	v.keys, v.fetchedAt = fetched, v.clk.Now()
	v.mu.Unlock()
	return fetched, nil
}

func (v *Verifier) fetch(ctx context.Context) (*jose.JSONWebKeySet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.cfg.JWKSURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", v.cfg.JWKSURL, resp.StatusCode)
	}
	// Bounded: a key set is kilobytes, and an unbounded read of a URL from
	// configuration is a way for a misconfigured host to exhaust this process.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	// The certificate chain is dropped before parsing, deliberately: eSignet
	// publishes self-signed x5c certificates whose serial numbers Go's x509
	// refuses outright ("negative serial number"), and go-jose fails the whole
	// key on an unparseable chain. Verification never uses the chain — the JWKS
	// URL itself is the trust anchor here, configured per provider — so the
	// bare key material is what matters.
	var chainless struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &chainless); err != nil {
		return nil, fmt.Errorf("%s did not return a JWKS: %w", v.cfg.JWKSURL, err)
	}
	var set jose.JSONWebKeySet
	for _, k := range chainless.Keys {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(k, &fields); err != nil {
			return nil, fmt.Errorf("%s did not return a JWKS: %w", v.cfg.JWKSURL, err)
		}
		delete(fields, "x5c")
		bare, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		var key jose.JSONWebKey
		if err := json.Unmarshal(bare, &key); err != nil {
			return nil, fmt.Errorf("%s did not return a JWKS: %w", v.cfg.JWKSURL, err)
		}
		set.Keys = append(set.Keys, key)
	}
	if len(set.Keys) == 0 {
		return nil, fmt.Errorf("%s returned no keys", v.cfg.JWKSURL)
	}
	return &set, nil
}

// BearerToken pulls the token out of an Authorization header.
func BearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}
