package serviceauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type replayStoreStub struct {
	claims int
	seen   map[string]bool
}

func (s *replayStoreStub) ClaimServiceNonce(_ context.Context, serviceID, nonce string, _ time.Time) (bool, error) {
	s.claims++
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	key := serviceID + ":" + nonce
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
	return true, nil
}

func TestServiceIdentityBindsBodyTargetAndCapabilities(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]Peer{"payments": {PublicKey: base64.StdEncoding.EncodeToString(public), Allow: []string{"GET /internal/units/"}}})
	for _, tc := range []struct {
		name, method, path string
		tamper             bool
		want               int
	}{
		{"authorized read", "GET", "/internal/units/unit", false, 204},
		{"payment cannot issue credential", "POST", "/internal/credentials/issue", false, 403},
		{"tampered query", "GET", "/internal/units/unit", true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, _ := NewVerifier(string(raw))
			r := httptest.NewRequest(tc.method, "http://core:8080"+tc.path, nil)
			if err := Sign(r, "payments", base64.StdEncoding.EncodeToString(private.Seed())); err != nil {
				t.Fatal(err)
			}
			if tc.tamper {
				r.URL.RawQuery = "other=party"
			}
			w := httptest.NewRecorder()
			v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d want %d", w.Code, tc.want)
			}
		})
	}
	v, _ := NewVerifier(string(raw))
	r := httptest.NewRequest("GET", "http://core:8080/internal/units/u", nil)
	if err := Sign(r, "payments", base64.StdEncoding.EncodeToString(private.Seed())); err != nil {
		t.Fatal(err)
	}
	h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	h.ServeHTTP(httptest.NewRecorder(), r)
	replay := httptest.NewRecorder()
	h.ServeHTTP(replay, r)
	if replay.Code != 409 {
		t.Fatal("replay was accepted")
	}
}

func TestSignedBodyCannotBeChanged(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	raw, _ := json.Marshal(map[string]Peer{"core": {PublicKey: base64.StdEncoding.EncodeToString(public), Allow: []string{"POST /internal/claims/"}}})
	v, _ := NewVerifier(string(raw))
	r, _ := http.NewRequest("POST", "http://core:8080/internal/claims/c/transition", strings.NewReader(`{"to":"ACCEPTED"}`))
	if err := Sign(r, "core", base64.StdEncoding.EncodeToString(private.Seed())); err != nil {
		t.Fatal(err)
	}
	changed, _ := http.NewRequest("POST", r.URL.String(), strings.NewReader(`{"to":"DISPUTED"}`))
	changed.Header = r.Header
	w := httptest.NewRecorder()
	v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, changed)
	if w.Code != 401 {
		t.Fatal("changed body was authorized")
	}
}

func TestInvalidSignatureDoesNotCallReplayStore(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	raw, _ := json.Marshal(map[string]Peer{"core": {PublicKey: base64.StdEncoding.EncodeToString(public), Allow: []string{"POST /internal/claims/"}}})
	store := &replayStoreStub{}
	v, _ := NewVerifier(string(raw))
	v.WithReplayStore(store)
	r, err := http.NewRequest("POST", "http://core:8080/internal/claims/c/transition", strings.NewReader(`{"to":"ACCEPTED"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(r, "core", base64.StdEncoding.EncodeToString(private.Seed())); err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-CREST-Service-Signature", "invalid")
	w := httptest.NewRecorder()
	v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature status = %d, want 401", w.Code)
	}
	if store.claims != 0 {
		t.Fatalf("invalid signature caused %d replay-store calls", store.claims)
	}
}

func TestReplayStoreProtectsAcrossVerifierInstances(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	raw, _ := json.Marshal(map[string]Peer{"core": {PublicKey: base64.StdEncoding.EncodeToString(public), Allow: []string{"GET /internal/units/"}}})
	shared := &replayStoreStub{}
	v1, _ := NewVerifier(string(raw))
	v2, _ := NewVerifier(string(raw))
	v1.WithReplayStore(shared)
	v2.WithReplayStore(shared)
	request := httptest.NewRequest("GET", "http://core:8080/internal/units/u", nil)
	if err := Sign(request, "core", base64.StdEncoding.EncodeToString(private.Seed())); err != nil {
		t.Fatal(err)
	}
	for i, verifier := range []*Verifier{v1, v2} {
		w := httptest.NewRecorder()
		verifier.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, request)
		want := http.StatusNoContent
		if i == 1 {
			want = http.StatusConflict
		}
		if w.Code != want {
			t.Fatalf("verifier %d status = %d, want %d", i, w.Code, want)
		}
	}
}

func TestPreviousServiceKeyIsAcceptedWithinGrace(t *testing.T) {
	currentPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	previousPublic, previousPrivate, _ := ed25519.GenerateKey(rand.Reader)
	expires := time.Now().Add(time.Minute)
	raw, _ := json.Marshal(map[string]Peer{"core": {
		PublicKey:    base64.StdEncoding.EncodeToString(currentPublic),
		Allow:        []string{"GET /internal/units/"},
		PreviousKeys: []PreviousKey{{PublicKey: base64.StdEncoding.EncodeToString(previousPublic), NotAfter: expires}},
	}})
	v, err := NewVerifier(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://core:8080/internal/units/u", nil)
	if err := Sign(r, "core", base64.StdEncoding.EncodeToString(previousPrivate.Seed())); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("grace-period previous key status = %d, want 204", w.Code)
	}
}

func TestExpiredPreviousServiceKeyIsRejected(t *testing.T) {
	currentPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	previousPublic, previousPrivate, _ := ed25519.GenerateKey(rand.Reader)
	raw, _ := json.Marshal(map[string]Peer{"core": {
		PublicKey:    base64.StdEncoding.EncodeToString(currentPublic),
		Allow:        []string{"GET /internal/units/"},
		PreviousKeys: []PreviousKey{{PublicKey: base64.StdEncoding.EncodeToString(previousPublic), NotAfter: time.Now().Add(-time.Minute)}},
	}})
	v, err := NewVerifier(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://core:8080/internal/units/u", nil)
	if err := Sign(r, "core", base64.StdEncoding.EncodeToString(previousPrivate.Seed())); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired previous key status = %d, want 401", w.Code)
	}
}

func TestCurrentServiceKeyRemainsValidWithExpiredPrevious(t *testing.T) {
	currentPublic, currentPrivate, _ := ed25519.GenerateKey(rand.Reader)
	previousPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	raw, _ := json.Marshal(map[string]Peer{"core": {
		PublicKey:    base64.StdEncoding.EncodeToString(currentPublic),
		Allow:        []string{"GET /internal/units/"},
		PreviousKeys: []PreviousKey{{PublicKey: base64.StdEncoding.EncodeToString(previousPublic), NotAfter: time.Now().Add(-time.Minute)}},
	}})
	v, err := NewVerifier(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://core:8080/internal/units/u", nil)
	if err := Sign(r, "core", base64.StdEncoding.EncodeToString(currentPrivate.Seed())); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("current key with expired previous status = %d, want 204", w.Code)
	}
}

func TestPreviousServiceKeyRequiresExplicitExpiry(t *testing.T) {
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	previous, _, _ := ed25519.GenerateKey(rand.Reader)
	raw, _ := json.Marshal(map[string]Peer{"core": {
		PublicKey:    base64.StdEncoding.EncodeToString(public),
		Allow:        []string{"GET /internal/units/"},
		PreviousKeys: []PreviousKey{{PublicKey: base64.StdEncoding.EncodeToString(previous)}},
	}})
	if _, err := NewVerifier(string(raw)); err == nil || !strings.Contains(err.Error(), "notAfter is required") {
		t.Fatalf("missing previous-key expiry error = %v", err)
	}
}

func TestExpiredPreviousKeyDoesNotClaimReplayNonce(t *testing.T) {
	currentPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	previousPublic, previousPrivate, _ := ed25519.GenerateKey(rand.Reader)
	raw, _ := json.Marshal(map[string]Peer{"core": {
		PublicKey:    base64.StdEncoding.EncodeToString(currentPublic),
		Allow:        []string{"GET /internal/units/"},
		PreviousKeys: []PreviousKey{{PublicKey: base64.StdEncoding.EncodeToString(previousPublic), NotAfter: time.Now().Add(-time.Minute)}},
	}})
	store := &replayStoreStub{}
	v, err := NewVerifier(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	v.WithReplayStore(store)
	r := httptest.NewRequest("GET", "http://core:8080/internal/units/u", nil)
	if err := Sign(r, "core", base64.StdEncoding.EncodeToString(previousPrivate.Seed())); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || store.claims != 0 {
		t.Fatalf("expired previous key status=%d replay claims=%d, want 401 and 0", w.Code, store.claims)
	}
}

func TestValidateIdentityAcceptsOnlyCurrentOrUnexpiredPreviousKey(t *testing.T) {
	currentPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	previousPublic, previousPrivate, _ := ed25519.GenerateKey(rand.Reader)
	peers := func(notAfter time.Time) string {
		raw, _ := json.Marshal(map[string]Peer{"core": {
			PublicKey:    base64.StdEncoding.EncodeToString(currentPublic),
			Allow:        []string{"GET /internal/units/"},
			PreviousKeys: []PreviousKey{{PublicKey: base64.StdEncoding.EncodeToString(previousPublic), NotAfter: notAfter}},
		}})
		return string(raw)
	}
	seed := base64.StdEncoding.EncodeToString(previousPrivate.Seed())
	if err := ValidateIdentity("core", seed, peers(time.Now().Add(time.Minute))); err != nil {
		t.Fatalf("unexpired previous private key was rejected: %v", err)
	}
	if err := ValidateIdentity("core", seed, peers(time.Now().Add(-time.Minute))); err == nil {
		t.Fatal("expired previous private key was accepted")
	}
}
