package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPaymentOwnerIsTheDefaultFinancePermission(t *testing.T) {
	t.Setenv("PAYMENT_OPERATIONS_FUNCTION", "")
	if got := paymentOperationsFunction(); got != "payment-owner" {
		t.Fatalf("default finance permission = %q, want payment-owner", got)
	}
	t.Setenv("PAYMENT_OPERATIONS_FUNCTION", "settlement-operator")
	if got := paymentOperationsFunction(); got != "settlement-operator" {
		t.Fatalf("configured finance permission = %q, want settlement-operator", got)
	}
}

func TestRatePublicationForwardsTheVerifiedCaller(t *testing.T) {
	const bearer = "Bearer caller-token"
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer server.Close()

	h := &handlers{definitionsURL: server.URL}
	var out map[string]bool
	if err := h.postLinkedRecord(context.Background(), "/v1/definitions/d1/linked-records",
		map[string]string{"type": "payment-setup"}, bearer, &out); err != nil {
		t.Fatalf("post linked record: %v", err)
	}
	if got != bearer {
		t.Fatalf("forwarded authorization = %q, want caller bearer", got)
	}
	if !out["accepted"] {
		t.Fatal("did not decode definitions response")
	}
}

func TestRatePublicationRefusesMissingCaller(t *testing.T) {
	h := &handlers{definitionsURL: "http://127.0.0.1:1"}
	err := h.postLinkedRecord(context.Background(), "/v1/definitions/d1/linked-records",
		map[string]string{"type": "payment-setup"}, " ", nil)
	if err == nil || !strings.Contains(err.Error(), "authenticated caller token") {
		t.Fatalf("missing bearer error = %v, want authenticated caller token refusal", err)
	}
}

func TestRatePublicationDoesNotForwardBearerAcrossRedirect(t *testing.T) {
	const bearer = "Bearer caller-token"
	targetHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
			return
		}
		targetHit = true
	}))
	defer server.Close()

	h := &handlers{definitionsURL: server.URL}
	err := h.postLinkedRecord(context.Background(), "/start",
		map[string]string{"type": "payment-setup"}, bearer, nil)
	if err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("redirect response error = %v, want an explicit 307 refusal", err)
	}
	if targetHit {
		t.Fatal("the redirect target was contacted")
	}
}
