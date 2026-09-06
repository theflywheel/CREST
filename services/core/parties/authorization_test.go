package parties

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
)

func TestRegistryCustodianRequiresARealCaller(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/holds?contextId=project-1", nil)
	w := httptest.NewRecorder()
	if _, ok := requireRegistryCustodian(w, r, service.Deps{Log: slog.Default(), Authenticating: true}, "project-1"); ok {
		t.Fatal("an anonymous request was accepted as a custodian")
	}
	if w.Code != 401 {
		t.Fatalf("anonymous custodian request status = %d, want 401", w.Code)
	}
}

func TestRegistryCustodianCannotBeForgedByAnAuthenticatedWorker(t *testing.T) {
	caller := identity.Caller{Subject: "subject-1", PartyID: "worker-1"}
	r := httptest.NewRequest("GET", "/v1/holds?contextId=project-1", nil)
	r = r.WithContext(identity.NewContext(context.Background(), caller))
	w := httptest.NewRecorder()
	d := service.Deps{
		Log:            slog.Default(),
		Authenticating: true,
		Permits: func(context.Context, string, string, string) (bool, error) {
			return false, nil
		},
	}
	if _, ok := requireRegistryCustodian(w, r, d, "project-1"); ok {
		t.Fatal("an authenticated worker without a custodian grant was accepted")
	}
	if w.Code != 403 {
		t.Fatalf("unassigned worker status = %d, want 403", w.Code)
	}
}

func TestActualCallerDoesNotUseRequestedParty(t *testing.T) {
	caller := identity.Caller{Subject: "subject-1", PartyID: "custodian-1"}
	r := httptest.NewRequest("POST", "/v1/holds/h1/resolve", nil)
	r = r.WithContext(identity.NewContext(context.Background(), caller))
	got, ok := actualCaller(r)
	if !ok || got != "custodian-1" {
		t.Fatalf("actualCaller = %q, %v; want authenticated custodian-1", got, ok)
	}
}
