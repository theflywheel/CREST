package esignet

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// The client id must match the Python spike's derivation for the same key —
// the key IS the client identity (finding C9), and a Go re-registration that
// derived a different id would register a second client instead of being the
// same one.
func TestClientIDMatchesTheSpikesDerivation(t *testing.T) {
	key := testKey(t)
	digest := sha256.Sum256([]byte(key.N.String()))
	want := fmt.Sprintf("crest-rp-core-%x", digest[:4])
	if got := ClientIDFor("core", key); got != want {
		t.Fatalf("client id %q, want %q", got, want)
	}
}

func TestPKCEChallengeIsS256OfTheVerifier(t *testing.T) {
	p, err := NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); p.Challenge != want {
		t.Fatalf("challenge %q is not S256(verifier)", p.Challenge)
	}
	if p.State == "" || p.Verifier == p.State {
		t.Fatal("state must be its own random value")
	}
}

func TestAuthorizeURLCarriesTheContract(t *testing.T) {
	c := New("https://esignet.example", "https://ui.example", "crest", testKey(t))
	p, _ := NewPKCE()
	u, err := url.Parse(c.AuthorizeURL("https://door.example/cb", p))
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "ui.example" || u.Path != "/authorize" {
		t.Fatalf("authorize URL must target the UI host (C12), got %s", u)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"client_id": c.ClientID, "redirect_uri": "https://door.example/cb",
		"response_type": "code", "state": p.State,
		"code_challenge": p.Challenge, "code_challenge_method": "S256",
	} {
		if q.Get(k) != want {
			t.Fatalf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
}

func TestSignedStateRoundTripsAndRefusesTampering(t *testing.T) {
	salt := []byte("test-salt")
	payload := []byte(`{"state":"abc","door":"https://d"}`)
	signed := SignState(salt, payload)

	got, ok := VerifyState(salt, signed)
	if !ok || string(got) != string(payload) {
		t.Fatal("a signed state must verify and round-trip")
	}
	if _, ok := VerifyState([]byte("other-salt"), signed); ok {
		t.Fatal("a state signed under one salt must not verify under another")
	}
	parts := strings.SplitN(signed, ".", 2)
	forged := base64.RawURLEncoding.EncodeToString([]byte(`{"state":"evil"}`)) + "." + parts[1]
	if _, ok := VerifyState(salt, forged); ok {
		t.Fatal("a forged payload must not verify")
	}
	if _, ok := VerifyState(salt, "garbage"); ok {
		t.Fatal("garbage must not verify")
	}
}

func TestParseKeyReadsPKCS8AndPKCS1(t *testing.T) {
	key := testKey(t)
	for _, pemStr := range []string{pkcs8PEM(t, key), pkcs1PEM(key)} {
		parsed, err := ParseKey([]byte(pemStr))
		if err != nil {
			t.Fatal(err)
		}
		if parsed.N.Cmp(key.N) != 0 {
			t.Fatal("parsed key is not the key that was written")
		}
	}
	if _, err := ParseKey([]byte("not a key")); err == nil {
		t.Fatal("junk must not parse")
	}
}

func pkcs8PEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func pkcs1PEM(key *rsa.PrivateKey) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}
