package dedi

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The two vectors below were produced by DeDi-node's own publisher package
// (internal/publisher.Preimage) and are the reason it is acceptable for CREST
// to have a second implementation of someone else's wire contract at all.
//
// If DeDi changes the scheme string, the field order, the separator, the body
// hash or the timestamp format, this test fails here — at `make test-unit`,
// naming the field that moved — rather than in a deployment, where the only
// symptom is every registry write returning "signature does not verify".
const (
	vectorReplace = "ZGVkaS92MS9wdWJsaXNoClBPU1QKL2FkbWluL25hbWVzcGFjZXMvY3Jlc3QvcmVnaXN0cmllcy93b3JrLWRlZmluaXRpb25zL3JlY29yZHMvV0QtNDQ3MS9wdWJsaXNoCnZEcUo0NUlRdGUvak5xbFpjRnJIM3J4dmZsUGxiVHlsdGtROW4xdXNUaVk9Cjg5OTQwODVkNmNhNzZiMzE5MmY5MDQ5NjE3OTZjZTEwNmNmM2U4YTY2YTQ2NDRjOTU2MzBjNTFhNWYzNzIwNGItbGl2ZQoKMjAyNi0wOC0yNFQwOToxNTowMFo="
	vectorCreate  = "ZGVkaS92MS9wdWJsaXNoClBVVAovYWRtaW4vbmFtZXNwYWNlcy9jcmVzdApMQ1FOSitJMjhDWDY1ZnBlOFQ4R0kyZVpRMVc3M2doUmZPR2pOWG1QTlpJPQoKKgoyMDI2LTA4LTI0VDA5OjE1OjAwWg=="
)

func vectorTime(t *testing.T) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, "2026-08-24T09:15:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestPreimageWireContract(t *testing.T) {
	ts := vectorTime(t)

	cases := []struct {
		name, method, uri, body, want string
		pre                           Precondition
	}{
		{
			name:   "replace, lowercase method upcased",
			method: "post",
			uri:    "/admin/namespaces/crest/registries/work-definitions/records/WD-4471/publish",
			body:   `{"payload":{"definitionId":"WD-4471"}}`,
			pre:    Replace("8994085d6ca76b3192f904961796ce106cf3e8a66a4644c95630c51a5f37204b-live"),
			want:   vectorReplace,
		},
		{
			name:   "create sets If-None-Match to * inside the signed bytes",
			method: "PUT",
			uri:    "/admin/namespaces/crest",
			body:   `{"payload":{"name":"CREST"}}`,
			pre:    Create(),
			want:   vectorCreate,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := base64.StdEncoding.EncodeToString(Preimage(c.method, c.uri, []byte(c.body), c.pre, ts))
			if got != c.want {
				want, _ := base64.StdEncoding.DecodeString(c.want)
				gotRaw, _ := base64.StdEncoding.DecodeString(got)
				t.Fatalf("preimage drifted from DeDi's wire contract\n got: %q\nwant: %q", gotRaw, want)
			}
		})
	}
}

// A signature that would verify under a different timestamp, path or body is a
// signature that can be replayed onto a different write.
func TestPreimageBindsEveryField(t *testing.T) {
	ts := vectorTime(t)
	base := Preimage("POST", "/a", []byte(`{}`), Create(), ts)

	for name, other := range map[string][]byte{
		"different method":       Preimage("PUT", "/a", []byte(`{}`), Create(), ts),
		"different path":         Preimage("POST", "/b", []byte(`{}`), Create(), ts),
		"different body":         Preimage("POST", "/a", []byte(`{"x":1}`), Create(), ts),
		"different precondition": Preimage("POST", "/a", []byte(`{}`), Replace("abc-live"), ts),
		"different timestamp":    Preimage("POST", "/a", []byte(`{}`), Create(), ts.Add(time.Second)),
	} {
		if string(other) == string(base) {
			t.Errorf("%s produced the same preimage; that field is not bound", name)
		}
	}
}

func TestSignSetsHeadersThatMatchTheSignedBytes(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ID: "crest", Private: priv}
	ts := vectorTime(t)

	req, err := http.NewRequest(http.MethodPost, "https://node.example/admin/x", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"payload":{}}`)
	pre := Replace("deadbeef-live")
	key.sign(req, "/admin/x", body, pre, ts)

	// The precondition header and the signed precondition have to agree. They
	// are set in one place precisely so they cannot drift, and this asserts it.
	if req.Header.Get("If-Match") != "deadbeef-live" {
		t.Errorf("If-Match header = %q", req.Header.Get("If-Match"))
	}
	if req.Header.Get("If-None-Match") != "" {
		t.Errorf("a replace must not send If-None-Match")
	}
	sig, err := base64.StdEncoding.DecodeString(req.Header.Get(headerSignature))
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(pub, Preimage(http.MethodPost, "/admin/x", body, pre, ts), sig) {
		t.Fatal("the signature does not verify against the preimage the headers describe")
	}
	if got := req.Header.Get(headerTimestamp); got != "2026-08-24T09:15:00Z" {
		t.Errorf("timestamp header = %q, want RFC 3339 UTC", got)
	}
}

func TestParseKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	good := base64.StdEncoding.EncodeToString(priv)

	if _, err := ParseKey("crest", good+"\n"); err != nil {
		t.Errorf("a key file's trailing newline should not be fatal: %v", err)
	}
	for name, spec := range map[string][2]string{
		"no key id":    {"", good},
		"not base64":   {"crest", "not base64!"},
		"wrong length": {"crest", base64.StdEncoding.EncodeToString([]byte("short"))},
	} {
		if _, err := ParseKey(spec[0], spec[1]); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// An unparseable secret is still a secret.
	_, err := ParseKey("crest", "AAAA!!!!")
	if err != nil && strings.Contains(err.Error(), "AAAA") {
		t.Errorf("the error echoes the key material: %v", err)
	}
}
