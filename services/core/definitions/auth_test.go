package definitions

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
)

func authRequest(c identity.Caller) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/definitions", nil)
	return r.WithContext(identity.NewContext(context.Background(), c))
}

func TestAuthorizeFunctionRejectsForgedPartyClaim(t *testing.T) {
	rr := httptest.NewRecorder()
	r := authRequest(identity.Caller{Subject: "verified-subject", PartyID: "did:crest:party:actual"})
	d := service.Deps{Log: slog.Default(), Permits: func(context.Context, string, string, string) (bool, error) {
		return true, nil
	}}
	if _, ok := authorizeFunction(rr, r, d, "did:crest:party:forged", "project-1", FunctionDefinitionAuthor); ok {
		t.Fatal("forged party claim was accepted")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestAuthorizeFunctionRejectsAnonymousCaller(t *testing.T) {
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/definitions", nil)
	d := service.Deps{Log: slog.Default(), Permits: func(context.Context, string, string, string) (bool, error) {
		t.Fatal("permission check ran for an anonymous caller")
		return false, nil
	}}
	if _, ok := authorizeFunction(rr, r, d, "did:crest:party:author", "project-1", FunctionDefinitionAuthor); ok {
		t.Fatal("anonymous caller was accepted")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAuthorizeFunctionChecksProjectFunction(t *testing.T) {
	rr := httptest.NewRecorder()
	r := authRequest(identity.Caller{Subject: "verified-subject", PartyID: "did:crest:party:author"})
	var gotParty, gotFunction, gotContext string
	d := service.Deps{Log: slog.Default(), Permits: func(_ context.Context, party, function, ctx string) (bool, error) {
		gotParty, gotFunction, gotContext = party, function, ctx
		return false, nil
	}}
	if _, ok := authorizeFunction(rr, r, d, "did:crest:party:author", "project-1", FunctionDefinitionAuthor); ok {
		t.Fatal("caller without the project function was accepted")
	}
	if gotParty != "did:crest:party:author" || gotFunction != FunctionDefinitionAuthor || gotContext != "project-1" {
		t.Fatalf("permission check = (%q, %q, %q)", gotParty, gotFunction, gotContext)
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}
