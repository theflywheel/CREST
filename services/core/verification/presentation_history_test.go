package verification

import (
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
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

func TestPresentationsUsesResolvedCallerSubjectForUnfilteredHistory(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	schemaName := fmt.Sprintf("verification_presentations_%d", time.Now().UnixNano())
	db, err := store.Open(ctx, dsn, schemaName, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer func() {
		_, _ = db.Q().Exec(context.Background(), `DROP SCHEMA "`+schemaName+`" CASCADE`)
	}()
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		t.Fatal(err)
	}

	created := time.Now().UTC()
	if _, err := db.Q().Exec(ctx, `
		INSERT INTO presentations
			(id, subject_ref, requested_by, purpose, scope, outcome, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7),
		       ('presentation-other', 'party-other', $3, $4, $5, $6, $7)`,
		"presentation-1", "party-1", "verifier-1", "history check", "scoped", "valid", created); err != nil {
		t.Fatal(err)
	}

	h := &handlers{d: service.Deps{DB: db, Log: slog.Default()}}
	req := httptest.NewRequest(http.MethodGet, "/v1/presentations", nil).WithContext(
		identity.NewContext(ctx, identity.Caller{Subject: "subject-1", PartyID: "party-1"}))
	res := httptest.NewRecorder()
	h.presentations(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("presentation history: HTTP %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Presentations []struct {
			ID         string `json:"id"`
			SubjectRef string `json:"subjectRef"`
		} `json:"presentations"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Presentations) != 1 || body.Presentations[0].ID != "presentation-1" ||
		body.Presentations[0].SubjectRef != "party-1" {
		t.Fatalf("resolved caller history = %+v, want presentation-1 for party-1", body.Presentations)
	}
}
