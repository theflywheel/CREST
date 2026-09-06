package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceSecretOnlyReachesConfiguredInternalOrigin(t *testing.T) {
	t.Setenv("CREST_SERVICE_TOKEN", "a-private-service-token-at-least-32-bytes")
	var received string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Get("X-CREST-Service-Token")
		w.WriteHeader(204)
	}))
	defer s.Close()
	c := New(s.URL)
	if err := c.Get(context.Background(), "/internal/data", nil); err != nil {
		t.Fatal(err)
	}
	if received != "" {
		t.Fatal("secret leaked to unconfigured origin")
	}
	t.Setenv("EVIDENCE_URL", s.URL)
	if err := c.Get(context.Background(), "/internal/data", nil); err != nil {
		t.Fatal(err)
	}
	if received == "" {
		t.Fatal("trusted service did not receive authentication")
	}
	if err := c.Get(context.Background(), "/v1/data", nil); err != nil {
		t.Fatal(err)
	}
	if received != "" {
		t.Fatal("secret leaked to public path")
	}
}

func TestServiceRedirectIsNeverFollowed(t *testing.T) {
	reached := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/internal/data", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	t.Setenv("CREST_SERVICE_TOKEN", "a-private-service-token-at-least-32-bytes")
	t.Setenv("EVIDENCE_URL", source.URL)
	err := New(source.URL).Get(context.Background(), "/internal/data", nil)
	if Code(err) != http.StatusTemporaryRedirect || reached {
		t.Fatalf("redirect followed or hidden: reached=%v err=%v", reached, err)
	}
}
