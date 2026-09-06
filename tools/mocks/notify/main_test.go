package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAcceptRequiresProviderCredentials(t *testing.T) {
	i := &inbox{token: "secret", messages: map[string]message{}}
	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"to":"worker@example.test","acknowledgmentUrl":"http://localhost/worker/#/review/c1?token=t"}`))
	rr := httptest.NewRecorder()
	i.accept(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated transport request returned %d, want 401", rr.Code)
	}
}

func TestAcceptIsIdempotentAndInboxKeepsReviewLink(t *testing.T) {
	i := &inbox{token: "secret", messages: map[string]message{}}
	payload := `{"to":"worker@example.test","subject":"review","body":"open","acknowledgmentUrl":"http://localhost/worker/#/review/c1?token=t"}`
	for attempt := 0; attempt < 2; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer secret")
		rr := httptest.NewRecorder()
		i.accept(rr, req)
		if rr.Code != http.StatusAccepted {
			t.Fatalf("attempt %d returned %d", attempt+1, rr.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/messages?claimId=c1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	i.list(rr, req)
	var out struct {
		Messages []message `json:"messages"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 1 || out.Messages[0].ClaimID != "c1" || out.Messages[0].Acknowledgment == "" {
		t.Fatalf("inbox lost or duplicated review link: %+v", out.Messages)
	}
}

func TestInboxRequiresProviderCredentials(t *testing.T) {
	i := &inbox{token: "secret", messages: map[string]message{}}
	rr := httptest.NewRecorder()
	i.list(rr, httptest.NewRequest(http.MethodGet, "/messages", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated inbox read returned %d, want 401", rr.Code)
	}
}

func TestAcceptRejectsNonReviewLink(t *testing.T) {
	i := &inbox{token: "secret", messages: map[string]message{}}
	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"to":"worker@example.test","acknowledgmentUrl":"https://example.test/inbox"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	i.accept(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("non-review link returned %d, want 400", rr.Code)
	}
}
