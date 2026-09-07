package parties

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

func TestOnlyConfiguredUnboundAdministratorMaySetup(t *testing.T) {
	for _, tc := range []struct {
		name            string
		caller          identity.Caller
		subject, issuer string
		want            bool
	}{
		{"intended identity", identity.Caller{Subject: "bound-ref", Issuer: "trusted-issuer"}, "bound-ref", "trusted-issuer", true},
		{"anonymous", identity.Caller{}, "bound-ref", "trusted-issuer", false},
		{"wrong issuer", identity.Caller{Subject: "bound-ref", Issuer: "untrusted"}, "bound-ref", "trusted-issuer", false},
		{"wrong subject", identity.Caller{Subject: "someone-else", Issuer: "trusted-issuer"}, "bound-ref", "trusted-issuer", false},
		{"unconfigured", identity.Caller{Subject: "bound-ref", Issuer: "trusted-issuer"}, "", "", false},
		{"already enrolled", identity.Caller{Subject: "bound-ref", Issuer: "trusted-issuer", PartyID: "already-bound"}, "bound-ref", "trusted-issuer", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := setupCallerAllowed(tc.caller, tc.subject, tc.issuer); got != tc.want {
				t.Fatalf("allowed=%v want %v", got, tc.want)
			}
		})
	}
}

// This exercises the setup transaction against a fresh service schema. In
// particular, the first-run registration must be linked to its explicit setup
// decision; using the operator as an ordinary approver is rejected by the
// database constraint.
func TestSetupInstanceIsOneTimeAndUsesSetupDecision(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("setup_%d", time.Now().UnixNano())
	db, err := store.Open(ctx, dsn, schemaName, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer func() { _, _ = db.Q().Exec(ctx, "DROP SCHEMA \""+schemaName+"\" CASCADE") }()
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		t.Fatal(err)
	}

	const (
		instanceID = "crest:instance:01ARZ3NDEKTSV4RRFFQ69G5FAV"
		operatorID = "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAV"
		subject    = "setup-admin"
		issuer     = "https://issuer.example"
	)
	withEnv(t, map[string]string{
		"CREST_INSTANCE_ID":       instanceID,
		"CREST_INSTANCE_NAME":     "Test deployment",
		"CREST_OPERATOR_PARTY_ID": operatorID,
		"CREST_SETUP_SUBJECT_REF": subject,
		"CREST_SETUP_ISSUER":      issuer,
	})

	d := service.Deps{
		Config: config.Base{Env: "local"}, DB: db, Clock: clock.System{},
		Log: slog.Default(), Authenticating: true,
	}
	h := &handlers{d: d}
	request := func() *httptest.ResponseRecorder {
		body, marshalErr := json.Marshal(map[string]any{
			"displayName": "Test operating organisation",
			"contactRoutes": []schema.PartyContactRoutesItem{{
				Kind: schema.PartyContactRoutesItemKindEmail, Value: "operator@example.test",
			}},
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		r := httptest.NewRequest(http.MethodPost, "/v1/instance/setup", bytes.NewReader(body))
		r = r.WithContext(identity.NewContext(r.Context(), identity.Caller{Subject: subject, Issuer: issuer}))
		w := httptest.NewRecorder()
		h.setupInstance(w, r)
		return w
	}

	first := request()
	if got := first.Code; got != http.StatusCreated {
		t.Fatalf("first setup status = %d, want %d (body: %s)", got, http.StatusCreated, first.Body.String())
	}
	if got := request().Code; got != http.StatusConflict {
		t.Fatalf("setup replay status = %d, want %d", got, http.StatusConflict)
	}

	var source, setupID, decidedBy, partyID string
	if err := db.Q().QueryRow(ctx, `
		SELECT decision_source, setup_instance_id, decided_by, party_id
		FROM org_registrations WHERE party_id = $1`, operatorID).
		Scan(&source, &setupID, &decidedBy, &partyID); err != nil {
		t.Fatal(err)
	}
	if source != "INSTANCE_SETUP" || setupID != instanceID || decidedBy != operatorID || partyID != operatorID {
		t.Fatalf("setup registration = source %q setup %q decider %q party %q", source, setupID, decidedBy, partyID)
	}
	var setupCount int
	if err := db.Q().QueryRow(ctx, `SELECT count(*) FROM instance_setup`).Scan(&setupCount); err != nil {
		t.Fatal(err)
	}
	if setupCount != 1 {
		t.Fatalf("setup records = %d, want one", setupCount)
	}

	// Reclassifying this row as an ordinary approval must fail: the migration
	// keeps the non-self-approval invariant for every normal decision.
	if _, err := db.Q().Exec(ctx, `
		UPDATE org_registrations
		SET decision_source = 'REGISTRY', setup_instance_id = NULL
		WHERE party_id = $1`, operatorID); err == nil {
		t.Fatal("ordinary self-approved registration was accepted")
	}

	// A caller cannot turn the source marker into a bootstrap bypass for a
	// different party: the composite FK binds it to the operator established by
	// that setup decision.
	const forgedID = "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FB0"
	if _, err := db.Q().Exec(ctx, `
		INSERT INTO parties (id, kind, doc, created_at) VALUES ($1, 'PERSON', '{}', now())`, forgedID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Q().Exec(ctx, `
		INSERT INTO org_registrations
		    (party_id, state, decided_by, decided_at, applied_at, decision_source, setup_instance_id)
		VALUES ($1, 'APPROVED', $1, now(), now(), 'INSTANCE_SETUP', $2)`, forgedID, instanceID); err == nil {
		t.Fatal("forged instance-setup approval was accepted for another party")
	}
	if _, err := db.Q().Exec(ctx, `
		INSERT INTO org_registrations
		    (party_id, state, decided_by, decided_at, applied_at, decision_source)
		VALUES ($1, 'REJECTED', $1, now(), now(), 'REGISTRY')`, forgedID); err == nil {
		t.Fatal("ordinary self-rejected registration was accepted")
	}
}
