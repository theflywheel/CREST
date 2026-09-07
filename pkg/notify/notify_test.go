package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSMTPRequiresConfiguredRealTransport(t *testing.T) {
	if _, err := NewSMTP(SMTPConfig{}); err == nil {
		t.Fatal("empty SMTP configuration was accepted")
	}
}

func TestHTTPProviderRequiresAcceptedStructuredResponse(t *testing.T) {
	cases := []struct {
		body string
		code int
		ok   bool
	}{
		{`{"accepted":true,"providerId":"n-1"}`, http.StatusOK, true},
		{`{"accepted":false}`, http.StatusOK, false},
		{`not-json`, http.StatusOK, false},
		{`{"accepted":true}`, http.StatusBadGateway, false},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.code)
			_, _ = w.Write([]byte(tc.body))
		}))
		p, err := NewHTTP(HTTPConfig{URL: srv.URL})
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.Send(context.Background(), Message{To: "worker@example.org", Subject: "review", Body: "review"})
		srv.Close()
		if (err == nil) != tc.ok || (tc.ok && !got.Accepted) {
			t.Fatalf("response %q/%d: result=%+v err=%v, accepted=%v", tc.body, tc.code, got, err, tc.ok)
		}
	}
}

func TestCallbackSignatureFailsClosedWithoutSecret(t *testing.T) {
	body := []byte(`{"event":"delivered"}`)
	sig := CallbackSignature("secret", body)
	if !VerifyCallback("secret", sig, body) {
		t.Fatal("valid callback signature was rejected")
	}
	if VerifyCallback("", sig, body) || VerifyCallback("other", sig, body) || VerifyCallback("secret", sig+"x", body) {
		t.Fatal("invalid callback signature was accepted")
	}
}
