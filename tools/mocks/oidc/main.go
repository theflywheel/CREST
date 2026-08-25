// Command oidc is a mock OpenID Provider for local development and the harness.
//
// It exists so the e2e suite can prove that CREST refuses an unproven caller
// (#89) without booting eSignet, which lives in the `substrate` compose profile
// and brings Redis, a mock identity system and three open findings with it.
//
// What it deliberately is not is a bypass. There is no "trust this header in
// development" path anywhere in pkg/identity, because a bypass is a production
// hole that happens to be switched off, and the switch is one environment
// variable away from being on. This mints real ES256 tokens against a real
// JWKS, and the services verify them with exactly the code they run in
// production — signature, issuer, audience, expiry and all. The only thing that
// differs between here and a deployment is which URL the keys came from.
//
// The signing key is generated at start-up and never leaves this process, so
// there is no key material in the repository to leak. The cost is that
// restarting this container invalidates the key sets the services cached; the
// local compose sets a short cache and a short refresh interval so they
// recover in seconds rather than minutes.
//
// Never deployed anywhere real.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"os"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/theflywheel/crest/pkg/identity"
)

type provider struct {
	issuer string
	key    *ecdsa.PrivateKey
	kid    string
	signer jose.Signer
}

func main() {
	addr := env("ADDR", ":8080")
	issuer := env("OIDC_ISSUER", "http://mock-oidc:8080")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("generating a signing key: %v", err)
	}
	kid := randomID()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid),
	)
	if err != nil {
		log.Fatalf("building a signer: %v", err)
	}
	p := &provider{issuer: issuer, key: key, kid: kid, signer: signer}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/jwks.json", p.jwks)
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("POST /token", p.token)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// Dev-only: the browser app binds by self-proof, which needs THIS
	// deployment's salted subject for a provider sub — and the browser must
	// not know the salt. The mock issuer shares the stack's salt in dev, so it
	// can answer. A real deployment has no such endpoint anywhere: the binding
	// happens server-side in the login callback.
	salt := []byte(env("CREST_SUBJECT_SALT", ""))
	mux.HandleFunc("GET /dev/pairwise", func(w http.ResponseWriter, r *http.Request) {
		if len(salt) == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no CREST_SUBJECT_SALT configured"})
			return
		}
		sub := r.URL.Query().Get("sub")
		if sub == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sub is required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"subject": identity.Pairwise(salt, p.issuer, sub)})
	})

	log.Printf("mock oidc listening on %s as %s (kid %s)", addr, issuer, kid)
	srv := &http.Server{Addr: addr, Handler: devCORS(mux), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func (p *provider) jwks(w http.ResponseWriter, _ *http.Request) {
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       p.key.Public(),
		KeyID:     p.kid,
		Algorithm: string(jose.ES256),
		Use:       "sig",
	}}}
	writeJSON(w, http.StatusOK, set)
}

func (p *provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                p.issuer,
		"jwks_uri":                              p.issuer + "/.well-known/jwks.json",
		"token_endpoint":                        p.issuer + "/token",
		"id_token_signing_alg_values_supported": []string{"ES256"},
	})
}

// token mints an access token for whoever asks.
//
// No password, no authorization code, no consent screen: this is a test
// fixture standing in for a national identity system, and reproducing its
// login flow would be reproducing the part that is not under test. What is
// under test is what CREST does with the token afterwards.
func (p *provider) token(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Subject   string `json:"sub"`
		Audience  string `json:"aud"`
		ExpiresIn string `json:"expiresIn"`
		// NotBefore lets a test mint a token that is not valid yet, which is
		// one of the ways a token can be wrong and therefore one of the ways
		// the verifier has to be right.
		NotBefore string `json:"notBefore"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if body.Subject == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sub is required"})
		return
	}
	ttl := 15 * time.Minute
	if body.ExpiresIn != "" {
		d, err := time.ParseDuration(body.ExpiresIn)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expiresIn is not a duration"})
			return
		}
		ttl = d
	}

	// Wall-clock, not the driveable clock the services run on. A token's
	// lifetime is the identity provider's judgement and a real one does not
	// take instructions from a test harness — so a suite that advances CREST's
	// clock by twenty days must still hold a token that has not expired, which
	// is why the harness mints tokens with long lifetimes rather than moving
	// this clock too.
	now := time.Now()
	claims := map[string]any{
		"iss": p.issuer,
		"sub": body.Subject,
		"iat": now.Unix(),
		"exp": now.Add(ttl).Unix(),
	}
	if body.Audience != "" {
		claims["aud"] = body.Audience
	}
	if body.NotBefore != "" {
		d, err := time.ParseDuration(body.NotBefore)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "notBefore is not a duration"})
			return
		}
		claims["nbf"] = now.Add(d).Unix()
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	signed, err := p.signer.Sign(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": compact,
		"token_type":   "Bearer",
		"expires_in":   int(ttl.Seconds()),
	})
}

func randomID() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		log.Fatalf("no randomness: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(n.Bytes())
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// devCORS reflects any origin. This is a MOCK identity provider that exists
// only in dev stacks; a real issuer sets its own policy.
func devCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
