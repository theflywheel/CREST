package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

func TestReceiveIsDurablyIdempotent(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("devnotify_contract_%d", time.Now().UnixNano())
	db, err := store.Open(ctx, dsn, schema, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Q().Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		db.Close()
	})
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		t.Fatal(err)
	}
	h := &handler{d: service.Deps{DB: db, Clock: clock.System{}}, token: "inbox-secret"}
	body := `{"to":"worker@example.org","subject":"review","body":"Open the link","acknowledgmentUrl":"https://core/review/token"}`
	providers := make([]string, 0, 2)
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/v1/notifications", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer inbox-secret")
		rec := httptest.NewRecorder()
		h.receive(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("receive status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var response struct {
			ProviderID string `json:"providerId"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		providers = append(providers, response.ProviderID)
	}
	if providers[0] == "" || providers[0] != providers[1] {
		t.Fatalf("replay provider ids = %v", providers)
	}
	var count int
	if err := db.Q().QueryRow(ctx, "SELECT count(*) FROM inbox_messages").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("durable inbox rows = %d, want 1", count)
	}
}
