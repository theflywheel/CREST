package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/theflywheel/crest/pkg/clock"
)

// issuer is a test OpenID provider: a key, a JWKS endpoint, and a way to mint
// whatever a scenario needs to be wrong about.
type issuer struct {
	key    *ecdsa.PrivateKey
	kid    string
	srv    *httptest.Server
	name   string
	served int
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	i := &issuer{key: key, kid: "test-key-1"}
	i.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i.served++
		// i.key rather than the local: a test that rotates the issuer's key
		// must actually change what the JWKS serves.
		set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: i.key.Public(), KeyID: i.kid, Algorithm: string(jose.ES256), Use: "sig",
		}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(i.srv.Close)
	i.name = "https://issuer.test"
	return i
}

func (i *issuer) mint(t *testing.T, claims map[string]any) string {
	t.Helper()
	return i.mintWith(t, i.key, i.kid, claims)
}

func (i *issuer) mintWith(t *testing.T, key *ecdsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	token, err := sig.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (i *issuer) config() Config {
	return Config{
		Issuer:     i.name,
		JWKSURL:    i.srv.URL,
		Audience:   "crest",
		Salt:       []byte("a-test-salt"),
		Leeway:     time.Minute,
		CacheFor:   15 * time.Minute,
		MinRefresh: time.Minute,
	}
}

// newTestVerifier drives the verifier from a fake clock.
//
// Only tests may do this. NewVerifier reads the wall clock in every
// environment, on purpose — see its comment for what a movable clock would do
// to token expiry — and the fake here is how the expiry rules get exercised at
// all rather than a way for a deployment to acquire one.
func newTestVerifier(cfg Config, clk clock.Clock) *Verifier {
	v := NewVerifier(cfg)
	v.clk = clk
	return v
}

func goodClaims(i *issuer, now time.Time) map[string]any {
	return map[string]any{
		"iss": i.name, "sub": "worker-1", "aud": "crest",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
}

// eSignet publishes self-signed x5c certificates whose serial numbers Go's
// x509 refuses ("negative serial number"), and go-jose fails the whole key on
// an unparseable chain. The chain is dropped before parsing — the configured
// JWKS URL is the trust anchor, not the certificate — so a provider with a
// broken x5c must still verify (this rejected every deployed eSignet login on
// 2026-09-02).
func TestAJWKSWithAnUnparseableCertChainStillServesItsKeys(t *testing.T) {
	i := newIssuer(t)
	// Re-serve the same key with a garbage x5c bolted on.
	good := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: i.key.Public(), KeyID: i.kid, Algorithm: string(jose.ES256), Use: "sig",
	}}}
	raw, _ := json.Marshal(good)
	var m map[string][]map[string]any
	_ = json.Unmarshal(raw, &m)
	m["keys"][0]["x5c"] = []string{"bm90IGEgY2VydGlmaWNhdGU="} // "not a certificate"
	poisoned, _ := json.Marshal(m)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(poisoned)
	}))
	t.Cleanup(srv.Close)

	now := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	cfg := i.config()
	cfg.JWKSURL = srv.URL
	v := newTestVerifier(cfg, clock.NewFake(now))
	if _, err := v.Verify(context.Background(), i.mint(t, goodClaims(i, now))); err != nil {
		t.Fatalf("a key with a broken cert chain must still verify: %v", err)
	}
}

func TestAValidTokenEstablishesACaller(t *testing.T) {
	i := newIssuer(t)
	now := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now)
	v := newTestVerifier(i.config(), clk)

	c, err := v.Verify(context.Background(), i.mint(t, goodClaims(i, now)))
	if err != nil {
		t.Fatalf("a valid token was rejected: %v", err)
	}
	if c.Issuer != i.name {
		t.Errorf("issuer is %q", c.Issuer)
	}
	// The provider's own subject must not survive into the Caller. That is the
	// whole of the #52/#63 mitigation: what CREST holds is its own value, not
	// eSignet's.
	if c.Subject == "worker-1" {
		t.Fatal("the provider's raw subject reached the caller unsalted")
	}
	if c.Subject != Pairwise([]byte("a-test-salt"), i.name, "worker-1") {
		t.Errorf("the subject is not this deployment's pairwise value")
	}
}

func TestATokenIsRefusedWhenAnythingAboutItIsWrong(t *testing.T) {
	i := newIssuer(t)
	now := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)

	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		claims map[string]any
		key    *ecdsa.PrivateKey
		kid    string
	}{
		{name: "another issuer", claims: map[string]any{
			"iss": "https://somewhere.else", "sub": "worker-1", "aud": "crest",
			"exp": now.Add(time.Hour).Unix()}},
		{name: "another audience", claims: map[string]any{
			"iss": i.name, "sub": "worker-1", "aud": "another-system",
			"exp": now.Add(time.Hour).Unix()}},
		{name: "expired", claims: map[string]any{
			"iss": i.name, "sub": "worker-1", "aud": "crest",
			"exp": now.Add(-2 * time.Hour).Unix()}},
		{name: "not valid yet", claims: map[string]any{
			"iss": i.name, "sub": "worker-1", "aud": "crest",
			"nbf": now.Add(time.Hour).Unix(), "exp": now.Add(2 * time.Hour).Unix()}},
		{name: "no expiry at all", claims: map[string]any{
			"iss": i.name, "sub": "worker-1", "aud": "crest"}},
		{name: "no subject", claims: map[string]any{
			"iss": i.name, "aud": "crest", "exp": now.Add(time.Hour).Unix()}},
		// Signed by a key the issuer never published. The kid still names a
		// key that exists, which is what makes this the interesting case:
		// trusting the kid to say who signed it would accept this.
		{name: "signed by a key nobody published", claims: goodClaims(i, now),
			key: other, kid: "test-key-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := clock.NewFake(now)
			v := newTestVerifier(i.config(), clk)
			key, kid := i.key, i.kid
			if tc.key != nil {
				key, kid = tc.key, tc.kid
			}
			_, err := v.Verify(context.Background(), i.mintWith(t, key, kid, tc.claims))
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("accepted a token that was %s: %v", tc.name, err)
			}
		})
	}
}

func TestAnUnsignedTokenIsNotEvenParsed(t *testing.T) {
	i := newIssuer(t)
	v := NewVerifier(i.config())

	// alg=none, the oldest JWT mistake there is. It does not reach a signature
	// check because the parser will not produce it — which is the point of
	// passing the allowlist to ParseSigned rather than reading the header.
	header := `{"alg":"none","typ":"JWT"}`
	payload := `{"iss":"https://issuer.test","sub":"nobody","exp":9999999999}`
	token := b64(header) + "." + b64(payload) + "."

	if _, err := v.Verify(context.Background(), token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("an unsigned token produced %v", err)
	}
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func TestAKeySetIsCachedAndRefreshedOnAnUnknownKey(t *testing.T) {
	i := newIssuer(t)
	now := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now)
	v := newTestVerifier(i.config(), clk)
	ctx := context.Background()

	if _, err := v.Verify(ctx, i.mint(t, goodClaims(i, now))); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(ctx, i.mint(t, goodClaims(i, now))); err != nil {
		t.Fatal(err)
	}
	if i.served != 1 {
		t.Fatalf("the key set was fetched %d times for two tokens; it is not cached", i.served)
	}

	// The issuer rotates. Nothing restarts, and a token signed by the new key
	// has to start working — an identity provider rotating its keys must not
	// take a deployment down until somebody notices.
	rotated, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	i.key, i.kid = rotated, "test-key-2"
	clk.Advance(2 * time.Minute) // past MinRefresh

	if _, err := v.Verify(ctx, i.mint(t, goodClaims(i, now))); err != nil {
		t.Fatalf("a token signed by a rotated key was rejected: %v", err)
	}
	if i.served != 2 {
		t.Fatalf("the key set was fetched %d times; the unknown key id should have provoked exactly one refetch", i.served)
	}
}

func TestARefetchIsRateLimited(t *testing.T) {
	i := newIssuer(t)
	now := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now)
	v := newTestVerifier(i.config(), clk)
	ctx := context.Background()

	if _, err := v.Verify(ctx, i.mint(t, goodClaims(i, now))); err != nil {
		t.Fatal(err)
	}
	// A stream of tokens naming keys that do not exist is what a forgery
	// attempt looks like. Each one must not become a request to the identity
	// provider, or this service becomes the instrument of an outage there.
	for n := 0; n < 20; n++ {
		_, _ = v.Verify(ctx, i.mintWith(t, i.key, "no-such-key", goodClaims(i, now)))
	}
	if i.served > 2 {
		t.Fatalf("20 forged tokens caused %d key fetches", i.served)
	}
}

func TestPairwiseSeparatesDeploymentsAndProviders(t *testing.T) {
	const sub = "worker-1"
	a := Pairwise([]byte("salt-a"), "https://issuer.test", sub)
	b := Pairwise([]byte("salt-b"), "https://issuer.test", sub)
	if a == b {
		t.Fatal("two deployments produce the same subject; they could correlate their workers")
	}
	if Pairwise([]byte("salt-a"), "https://other.test", sub) == a {
		t.Fatal("two providers issuing the same sub collide")
	}

	// Length-prefixed rather than concatenated. Without it, iss="ab" sub="c"
	// and iss="a" sub="bc" are the same input, which is one person being two
	// and two being one depending on which way round it happens.
	if Pairwise([]byte("s"), "ab", "c") == Pairwise([]byte("s"), "a", "bc") {
		t.Fatal("the issuer and subject run together")
	}
}

func TestAudienceReadsBothShapes(t *testing.T) {
	var one, many audience
	if err := json.Unmarshal([]byte(`"crest"`), &one); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`["other","crest"]`), &many); err != nil {
		t.Fatal(err)
	}
	if !one.has("crest") || !many.has("crest") {
		t.Fatalf("aud was not read: %v %v", one, many)
	}
	if many.has("missing") {
		t.Fatal("an audience that is not there was found")
	}
}

func TestBearerToken(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   string
		ok     bool
	}{
		{"Bearer abc", "abc", true},
		{"bearer abc", "abc", true}, // schemes are case-insensitive
		{"Bearer  abc ", "abc", true},
		{"abc", "", false},
		{"Bearer ", "", false},
		{"", "", false},
		{"Basic abc", "", false},
	} {
		got, ok := BearerToken(tc.header)
		if got != tc.want || ok != tc.ok {
			t.Errorf("BearerToken(%q) = %q,%v want %q,%v", tc.header, got, ok, tc.want, tc.ok)
		}
	}
}

func alwaysPermits(context.Context, string, string, string) (bool, error) { return true, nil }
func neverPermits(context.Context, string, string, string) (bool, error)  { return false, nil }

func TestActorRefusesAPartyItCannotProve(t *testing.T) {
	ctx := context.Background()
	worker := Caller{Subject: "s", PartyID: "did:crest:party:A"}

	// The failure #89 is about, and it had no name before this: the request
	// says one party and the caller is another.
	if _, err := Actor(ctx, worker, "did:crest:party:B", "", true, neverPermits); !errors.Is(err, ErrImpersonation) {
		t.Fatalf("naming another party produced %v", err)
	}
	// Naming your own party is fine, and so is naming none.
	for _, claimed := range []string{"did:crest:party:A", ""} {
		got, err := Actor(ctx, worker, claimed, "", true, neverPermits)
		if err != nil || got != "did:crest:party:A" {
			t.Fatalf("claimed=%q gave %q, %v", claimed, got, err)
		}
	}
}

func TestActorRefusesAnUnauthenticatedRequestWhereItMatters(t *testing.T) {
	ctx := context.Background()
	if _, err := Actor(ctx, Caller{}, "did:crest:party:A", "", true, alwaysPermits); !errors.Is(err, ErrNoCaller) {
		t.Fatalf("an unauthenticated request was not refused: %v", err)
	}
	// With no identity provider configured, the claimed party is returned and
	// the service has already said so loudly at start-up. pkg/service refuses
	// to start a production deployment in that state.
	got, err := Actor(ctx, Caller{}, "did:crest:party:A", "", false, alwaysPermits)
	if err != nil || got != "did:crest:party:A" {
		t.Fatalf("unenforced gave %q, %v", got, err)
	}
}

func TestActingForSomebodyElseNeedsTheAuthorization(t *testing.T) {
	ctx := context.Background()
	supervisor := Caller{Subject: "s", PartyID: "did:crest:party:SUP",
		requestedFor: "did:crest:party:WRK"}

	if _, err := Actor(ctx, supervisor, "", "ctx-1", true, neverPermits); !errors.Is(err, ErrNotPermitted) {
		t.Fatalf("assistance without the authorization produced %v", err)
	}

	// And with it, the assisted path works and reports itself as assisted.
	// This half matters as much as the refusal: a worker with no phone
	// confirming through a supervisor is one of the four T=7 exits, and a
	// tightening that closed it would take that exit away from exactly the
	// workers who cannot use any of the others.
	got, err := Actor(ctx, supervisor, "", "ctx-1", true, alwaysPermits)
	if err != nil {
		t.Fatalf("a permitted assistance was refused: %v", err)
	}
	if got != "did:crest:party:WRK" {
		t.Fatalf("the assisted action acted on %q", got)
	}
	if !supervisor.Assisting() {
		t.Fatal("the action does not report itself as assisted; the record would read as the worker's own")
	}
}

func TestAnUnboundCallerCannotAssist(t *testing.T) {
	ctx := context.Background()
	// Authenticated, unknown here, naming somebody to act for. Assistance with
	// nobody's name on it is not assistance.
	stranger := Caller{Subject: "s", requestedFor: "did:crest:party:WRK"}
	if _, err := Actor(ctx, stranger, "", "ctx-1", true, alwaysPermits); !errors.Is(err, ErrUnbound) {
		t.Fatalf("an unbound caller assisted: %v", err)
	}
}

func TestDenialSeparatesTheThreeRefusals(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{ErrNoCaller, 401},
		{ErrUnbound, 403},
		{ErrNotPermitted, 403},
		{ErrImpersonation, 403},
	} {
		status, code, _, ok := Denial(tc.err)
		if !ok || status != tc.want || code == "" {
			t.Errorf("Denial(%v) = %d,%q,%v", tc.err, status, code, ok)
		}
	}
	if _, _, _, ok := Denial(errors.New("a database fell over")); ok {
		t.Error("an unrelated error was reported as a denial; an outage would read as a refusal")
	}
}
