package evidence

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
)

func sourceOwnerRequest(owner string) (*http.Request, *httptest.ResponseRecorder) {
	r := httptest.NewRequest(http.MethodPost, "/v1/sources", nil)
	r = r.WithContext(identity.NewContext(context.Background(), identity.Caller{
		Subject: "verified-subject", Issuer: "trusted-issuer", PartyID: owner,
	}))
	return r, httptest.NewRecorder()
}

func TestAuthorizeSourceOwnerRequiresProjectFunction(t *testing.T) {
	called := false
	h := &handlers{d: service.Deps{
		Log: slog.Default(),
		Permits: func(_ context.Context, partyID, function, contextID string) (bool, error) {
			called = true
			if partyID != "did:crest:party:OWNER" || function != functionDefinitionSourceOwner || contextID != "crest:project:01ARZ3NDEKTSV4RRFFQ69G5FAV" {
				t.Fatalf("unexpected authorization query: %q %q %q", partyID, function, contextID)
			}
			return true, nil
		},
	}}
	r, w := sourceOwnerRequest("did:crest:party:OWNER")
	if !h.authorizeSourceOwner(w, r, "did:crest:party:OWNER", "crest:project:01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Fatal("a granted source owner was refused")
	}
	if !called {
		t.Fatal("source-owner function was not checked")
	}
}

func TestAuthorizeSourceOwnerDeniesUnpermittedAndForgedOwner(t *testing.T) {
	h := &handlers{d: service.Deps{
		Log:     slog.Default(),
		Permits: func(context.Context, string, string, string) (bool, error) { return false, nil },
	}}
	r, w := sourceOwnerRequest("did:crest:party:OWNER")
	if h.authorizeSourceOwner(w, r, "did:crest:party:OWNER", "crest:project:01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Fatal("an owner without the source-owner function was accepted")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("unpermitted owner status = %d, want %d", w.Code, http.StatusForbidden)
	}

	r, w = sourceOwnerRequest("did:crest:party:ACTUAL")
	if h.authorizeSourceOwner(w, r, "did:crest:party:FORGED", "crest:project:01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Fatal("a caller naming another owner was accepted")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("forged owner status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestAuthorizeSourceOwnerFailsClosedWhenRegistryUnavailable(t *testing.T) {
	h := &handlers{d: service.Deps{Log: slog.Default()}}
	r, w := sourceOwnerRequest("did:crest:party:OWNER")
	if h.authorizeSourceOwner(w, r, "did:crest:party:OWNER", "crest:project:01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Fatal("source registration succeeded without authorization")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing registry status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	h.d.Permits = func(context.Context, string, string, string) (bool, error) {
		return false, errors.New("registry down")
	}
	r, w = sourceOwnerRequest("did:crest:party:OWNER")
	if h.authorizeSourceOwner(w, r, "did:crest:party:OWNER", "crest:project:01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Fatal("source registration succeeded on authorization error")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("authorization error status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}
