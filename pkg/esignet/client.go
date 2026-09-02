// Package esignet is CREST's relying-party client for eSignet (#155, §4.1).
//
// The browser does the authenticating: the login route sends the person to
// eSignet's UI with a PKCE challenge, eSignet walks them through
// authentication and consent, and comes back to CREST's callback with a code.
// This package covers the two server-side legs — registering the client and
// exchanging the code for tokens with a private_key_jwt assertion.
//
// Everything an implementation choice depends on was discovered by the P0
// spikes (tools/spikes/esignet_oidc.py, docs/p0-findings.md) and is kept
// faithful here:
//   - C9: eSignet cannot rotate a registered client's public key, so the key
//     decides the client id — lose the key and you get a new client, never a
//     client you cannot authenticate as.
//   - eSignet answers 200 with an `errors` array; a bare status proves nothing.
//   - The client-mgmt endpoints sit behind Spring CSRF: a cookie plus an
//     X-XSRF-TOKEN header, both minted by /csrf/token.
package esignet

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/theflywheel/crest/pkg/clock"
)

// Client is a registered eSignet relying-party client.
type Client struct {
	// BaseURL is the eSignet API ("https://host" — paths below add /v1/esignet).
	BaseURL string
	// UIBaseURL is eSignet's OIDC UI, the hostname a browser is sent to. The
	// API service alone serves none of the URLs its discovery advertises
	// (finding C12); the UI is the public face.
	UIBaseURL string
	// RelyingPartyID partitions pairwise subjects (finding E6: per relying
	// party, not per client) — every CREST door under one id sees one sub.
	RelyingPartyID string

	Key      *rsa.PrivateKey
	ClientID string

	HTTP *http.Client
}

// New derives the client id from the key (C9) and prepares an HTTP client
// with a cookie jar, which the CSRF dance needs.
func New(baseURL, uiBaseURL, relyingPartyID string, key *rsa.PrivateKey) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		BaseURL:        strings.TrimRight(baseURL, "/"),
		UIBaseURL:      strings.TrimRight(uiBaseURL, "/"),
		RelyingPartyID: relyingPartyID,
		Key:            key,
		ClientID:       ClientIDFor("core", key),
		HTTP:           &http.Client{Timeout: 20 * time.Second, Jar: jar},
	}
}

// ParseKey reads a PKCS#8 or PKCS#1 RSA private key from PEM.
func ParseKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("esignet: no PEM block in key material")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("esignet: key is %T, want RSA", k)
		}
		return rk, nil
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// ClientIDFor matches the spike's derivation exactly, so a client registered
// by tools/spikes/esignet_oidc.py and one registered here are the same client
// when they hold the same key: sha256 of the decimal modulus, first 8 hex.
func ClientIDFor(label string, key *rsa.PrivateKey) string {
	digest := sha256.Sum256([]byte(key.N.String()))
	return fmt.Sprintf("crest-rp-%s-%x", label, digest[:4])
}

func b64u(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func b64uBig(i *big.Int) string { return b64u(i.Bytes()) }

func (c *Client) jwk() map[string]any {
	return map[string]any{
		"kty": "RSA",
		"e":   b64uBig(big.NewInt(int64(c.Key.E))),
		"n":   b64uBig(c.Key.N),
		"use": "sig", "alg": "RS256", "kid": c.ClientID,
	}
}

// The wall clock, deliberately: like token verification (pkg/identity), the
// login handshake runs on the identity provider's real time, never a
// driveable clock.
func nowISO() string { return clock.System{}.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

type envelope struct {
	Response json.RawMessage `json:"response"`
	Errors   []struct {
		ErrorCode string `json:"errorCode"`
		Message   string `json:"errorMessage"`
	} `json:"errors"`
}

func (c *Client) csrf(ctx context.Context) (header, token string, err error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/v1/esignet/csrf/token", nil)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = res.Body.Close() }()
	var body struct {
		HeaderName string `json:"headerName"`
		Token      string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", "", fmt.Errorf("esignet: csrf token: %w", err)
	}
	return body.HeaderName, body.Token, nil
}

// Register registers (or re-registers, idempotently) this client with eSignet,
// with the given redirect URIs. duplicate_client_id means an earlier run
// already registered this key and is success.
func (c *Client) Register(ctx context.Context, redirectURIs []string) error {
	hdr, tok, err := c.csrf(ctx)
	if err != nil {
		return fmt.Errorf("esignet: fetching csrf: %w", err)
	}
	body := map[string]any{
		"requestTime": nowISO(),
		"request": map[string]any{
			"clientId":          c.ClientID,
			"clientName":        c.ClientID,
			"clientNameLangMap": map[string]string{"eng": c.ClientID},
			"publicKey":         c.jwk(),
			"relyingPartyId":    c.RelyingPartyID,
			"userClaims":        []string{"name", "phone_number"},
			"authContextRefs":   []string{"mosip:idp:acr:generated-code", "mosip:idp:acr:static-code"},
			"logoUri":           "https://crest.example/logo.png",
			"redirectUris":      redirectURIs,
			"grantTypes":        []string{"authorization_code"},
			"clientAuthMethods": []string{"private_key_jwt"},
		},
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST",
		c.BaseURL+"/v1/esignet/client-mgmt/oauth-client", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if tok != "" {
		req.Header.Set(hdr, tok)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	text, _ := io.ReadAll(res.Body)
	var env envelope
	if err := json.Unmarshal(text, &env); err != nil {
		return fmt.Errorf("esignet: client registration answered %d: %.300s", res.StatusCode, text)
	}
	for _, e := range env.Errors {
		if e.ErrorCode == "duplicate_client_id" {
			return nil // already registered by an earlier run
		}
	}
	if len(env.Errors) > 0 {
		return fmt.Errorf("esignet: client registration failed: %v", env.Errors)
	}
	return nil
}

// PKCE is one login attempt's verifier/challenge pair plus the state that
// ties the callback to it.
type PKCE struct {
	State     string
	Verifier  string
	Challenge string
}

// NewPKCE mints the random material for one authorization request.
func NewPKCE() (PKCE, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return PKCE{}, err
	}
	verifier := b64u(buf[:])
	sum := sha256.Sum256([]byte(verifier))
	var st [16]byte
	if _, err := rand.Read(st[:]); err != nil {
		return PKCE{}, err
	}
	return PKCE{State: b64u(st[:]), Verifier: verifier, Challenge: b64u(sum[:])}, nil
}

// AuthorizeURL is where the browser goes: eSignet's UI, which runs the
// oauth-details/authenticate/consent legs itself and redirects back to
// redirectURI with ?code&state.
func (c *Client) AuthorizeURL(redirectURI string, p PKCE) string {
	q := url.Values{
		"client_id":             {c.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {"openid profile"},
		"state":                 {p.State},
		"code_challenge":        {p.Challenge},
		"code_challenge_method": {"S256"},
		"acr_values":            {"mosip:idp:acr:generated-code mosip:idp:acr:static-code"},
		"display":               {"page"},
		"prompt":                {"consent"},
		"max_age":               {"21"},
		"claims_locales":        {"en"},
	}
	return c.UIBaseURL + "/authorize?" + q.Encode()
}

// Tokens is what the exchange returns; AccessToken is what CREST's services
// verify as the Bearer, IDToken carries the pairwise sub the same way.
type Tokens struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// Exchange redeems the authorization code: private_key_jwt client assertion
// (RS256 under the registration key, kid = client id) plus the PKCE verifier.
func (c *Client) Exchange(ctx context.Context, code, redirectURI, verifier string) (Tokens, error) {
	endpoint := c.BaseURL + "/v1/esignet/oauth/v2/token"

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: c.Key},
		(&jose.SignerOptions{}).WithHeader("kid", c.ClientID),
	)
	if err != nil {
		return Tokens{}, fmt.Errorf("esignet: assertion signer: %w", err)
	}
	var jti [16]byte
	if _, err := rand.Read(jti[:]); err != nil {
		return Tokens{}, err
	}
	now := clock.System{}.Now()
	assertion, err := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer:   c.ClientID,
		Subject:  c.ClientID,
		Audience: jwt.Audience{endpoint},
		ID:       b64u(jti[:]),
		Expiry:   jwt.NewNumericDate(now.Add(5 * time.Minute)),
		IssuedAt: jwt.NewNumericDate(now),
	}).Serialize()
	if err != nil {
		return Tokens{}, fmt.Errorf("esignet: signing assertion: %w", err)
	}

	form := url.Values{
		"grant_type":            {"authorization_code"},
		"code":                  {code},
		"redirect_uri":          {redirectURI},
		"client_id":             {c.ClientID},
		"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"},
		"client_assertion":      {assertion},
		"code_verifier":         {verifier},
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Tokens{}, err
	}
	defer func() { _ = res.Body.Close() }()
	text, _ := io.ReadAll(res.Body)
	var t Tokens
	if err := json.Unmarshal(text, &t); err != nil || t.AccessToken == "" {
		return Tokens{}, fmt.Errorf("esignet: token exchange answered %d: %.300s", res.StatusCode, text)
	}
	return t, nil
}

// SignState HMACs a login attempt's cookie payload under a purpose-bound key,
// so the callback can trust that the state/verifier pair it reads is the one
// this service wrote. The derivation keeps the subject salt single-purpose.
func SignState(salt []byte, payload []byte) string {
	mac := hmac.New(sha256.New, deriveKey(salt))
	mac.Write(payload)
	return b64u(payload) + "." + b64u(mac.Sum(nil))
}

// VerifyState is SignState's inverse; a forged or truncated cookie yields false.
func VerifyState(salt []byte, signed string) ([]byte, bool) {
	parts := strings.SplitN(signed, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, deriveKey(salt))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, false
	}
	return payload, true
}

func deriveKey(salt []byte) []byte {
	mac := hmac.New(sha256.New, salt)
	mac.Write([]byte("crest-auth-state-v1"))
	return mac.Sum(nil)
}
