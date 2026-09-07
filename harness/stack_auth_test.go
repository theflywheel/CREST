package harness

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceAddsTokenOnlyToInternalRequests(t *testing.T) {
	const token = "local-service-token-with-at-least-32-bytes"
	t.Setenv("CREST_SERVICE_TOKEN", token)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/clock" && r.Header.Get("X-CREST-Service-Token") != token {
			t.Errorf("internal request token = %q, want configured token", r.Header.Get("X-CREST-Service-Token"))
		}
		if r.URL.Path == "/readyz" && r.Header.Get("X-CREST-Service-Token") != "" {
			t.Errorf("public request carried service token")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	svc := &Service{Name: "test", Base: server.URL, http: server.Client()}
	if err := svc.Get(t.Context(), "/internal/clock", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Get(t.Context(), "/readyz", nil); err != nil {
		t.Fatal(err)
	}
}
