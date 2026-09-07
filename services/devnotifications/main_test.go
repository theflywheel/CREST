package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theflywheel/crest/pkg/notify"
	"github.com/theflywheel/crest/pkg/service"
)

func TestReceiveRequiresProviderAuthenticationBeforeDatabaseAccess(t *testing.T) {
	h := &handler{d: service.Deps{}}
	for _, authorization := range []string{"", "Bearer wrong"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/notifications", strings.NewReader(`{"to":"worker@example.org"}`))
		req.Header.Set("Authorization", authorization)
		rec := httptest.NewRecorder()
		h.receive(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("authorization %q status = %d, want 401", authorization, rec.Code)
		}
	}
}

func TestReceiveRejectsMalformedNotificationWithoutDatabaseAccess(t *testing.T) {
	h := &handler{d: service.Deps{}, token: "inbox-secret"}
	req := httptest.NewRequest(http.MethodPost, "/v1/notifications", strings.NewReader(`{"to":"worker@example.org","subject":"review","body":"open"} trailing`))
	req.Header.Set("Authorization", "Bearer inbox-secret")
	rec := httptest.NewRecorder()
	h.receive(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed notification status = %d, want 400", rec.Code)
	}
}

func TestMessageDigestBindsAllDeliveredFields(t *testing.T) {
	base := notify.Message{To: "worker@example.org", Subject: "review", Body: "open", Acknowledgment: "https://core/review/a"}
	if messageDigest(base) == messageDigest(notify.Message{To: "worker@example.org", Subject: "review", Body: "changed", Acknowledgment: base.Acknowledgment}) {
		t.Fatal("message digest ignored body")
	}
	if messageDigest(base) == messageDigest(notify.Message{To: base.To, Subject: base.Subject, Body: base.Body, Acknowledgment: "https://core/review/b"}) {
		t.Fatal("message digest ignored acknowledgement URL")
	}
}

func TestInboxPageIsAvailableWithoutMessageAccess(t *testing.T) {
	h := &handler{}
	rec := httptest.NewRecorder()
	h.inbox(rec, httptest.NewRequest(http.MethodGet, "/inbox", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Development notification inbox") {
		t.Fatalf("inbox page status/body = %d/%q", rec.Code, rec.Body.String())
	}
}

func TestValidateMessageRejectsHeaderInjection(t *testing.T) {
	if err := validateMessage(notify.Message{To: "worker@example.org\r\nBcc: attacker@example.org", Subject: "review", Body: "open"}); err == nil {
		t.Fatal("header-injection recipient was accepted")
	}
}

func TestValidateConfigFailsClosedOutsideLocalOrWithoutToken(t *testing.T) {
	if err := validateConfig("production", "secret"); err == nil {
		t.Fatal("production devnotifications configuration was accepted")
	}
	if err := validateConfig("local", ""); err == nil {
		t.Fatal("unauthenticated devnotifications configuration was accepted")
	}
	if err := validateConfig("local", "secret"); err != nil {
		t.Fatalf("valid local devnotifications configuration was rejected: %v", err)
	}
}
