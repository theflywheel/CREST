package verification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// These use only the disposable database named by CREST_TEST_DATABASE_URL.
// They prove the two things the pass table and the trail index exist for:
// one pass per contact, and a cap counted from the trail.

func passHandlers(t *testing.T, rate rateCap) (*handlers, func()) {
	t.Helper()
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	db, err := store.Open(ctx, dsn, fmt.Sprintf("verification_passes_%d", time.Now().UnixNano()))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	schemaName := db.Schema()
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		cancel()
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	h := &handlers{d: service.Deps{DB: db, Log: log}, rate: rate}
	return h, func() {
		_, _ = db.Q().Exec(context.Background(), `DROP SCHEMA "`+schemaName+`" CASCADE`)
		db.Close()
		cancel()
	}
}

func issue(t *testing.T, h *handlers, name, contact string) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name, "contact": contact})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/verifier-passes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.issuePass(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAPassIsOnePerContactAndAskingAgainRotatesTheToken(t *testing.T) {
	h, done := passHandlers(t, rateCap{Cap: 100, Window: time.Hour})
	defer done()

	code, first := issue(t, h, "Joseph Mwangi", "+254 700 000 412")
	if code != http.StatusCreated {
		t.Fatalf("issue: %d %v", code, first)
	}
	firstPass := first["pass"].(map[string]any)
	firstToken := first["token"].(string)
	if !strings.HasPrefix(firstPass["id"].(string), "crest:pass:") || firstToken == "" {
		t.Fatalf("a pass came back without an id or a token: %v", first)
	}

	// The token resolves to the pass; a made-up one resolves to nothing.
	req := httptest.NewRequest(http.MethodPost, "/v1/verify", nil)
	req.Header.Set(HeaderPass, firstToken)
	p, presented, err := h.passFromRequest(context.Background(), req)
	if err != nil || !presented || p.ID != firstPass["id"] || p.Name != "Joseph Mwangi" {
		t.Fatalf("the issued token did not resolve: %+v presented=%v err=%v", p, presented, err)
	}
	req.Header.Set(HeaderPass, "not-a-token")
	if _, _, err := h.passFromRequest(context.Background(), req); !errors.Is(err, errNoPass) {
		t.Errorf("a made-up token resolved: %v", err)
	}

	// The same contact, differently spelled, is the same pass: same id, new
	// token, the old one dead. A cap per pass would otherwise be a cap per
	// request for a pass.
	code, second := issue(t, h, "J. Mwangi", "+254700000412")
	if code != http.StatusCreated {
		t.Fatalf("re-issue: %d %v", code, second)
	}
	secondPass := second["pass"].(map[string]any)
	if secondPass["id"] != firstPass["id"] || secondPass["rotated"] != true {
		t.Errorf("asking again minted a second identity: %v then %v", firstPass, secondPass)
	}
	req.Header.Set(HeaderPass, firstToken)
	if _, _, err := h.passFromRequest(context.Background(), req); !errors.Is(err, errNoPass) {
		t.Error("the replaced token still resolves")
	}
	req.Header.Set(HeaderPass, second["token"].(string))
	if p, _, err := h.passFromRequest(context.Background(), req); err != nil || p.Name != "J. Mwangi" {
		t.Errorf("the new token did not resolve to the updated pass: %+v %v", p, err)
	}

	// A different contact is a different pass.
	_, third := issue(t, h, "Ruth Njeri", "ruth@example.org")
	if third["pass"].(map[string]any)["id"] == firstPass["id"] {
		t.Error("two contacts share a pass")
	}
}

func TestTheRateCapIsCountedFromTheTrail(t *testing.T) {
	h, done := passHandlers(t, rateCap{Cap: 3, Window: time.Hour})
	defer done()
	ctx := context.Background()
	who := requester{ID: "crest:pass:01TESTPASS00000000000000000", Scope: "pass"}
	now := time.Now().UTC()

	leave := func(at time.Time) {
		t.Helper()
		if err := h.record(ctx, presentation{ID: fmt.Sprintf("crest:presentation:%d", at.UnixNano()),
			RequestedBy: who.ID, Scope: who.Scope, Outcome: "valid", CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	try := func(wanted int) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/verify", nil)
		ok := h.underRateCap(rec, req, who, wanted, now)
		if ok {
			return http.StatusOK, rec.Header().Get("X-CREST-Rate-Remaining")
		}
		return rec.Code, rec.Body.String()
	}

	if code, rem := try(1); code != http.StatusOK || rem != "2" {
		t.Fatalf("an empty trail was capped: %d %s", code, rem)
	}
	leave(now.Add(-2 * time.Minute))
	leave(now.Add(-time.Minute))
	// A row outside the window does not count.
	leave(now.Add(-2 * time.Hour))
	if code, rem := try(1); code != http.StatusOK || rem != "0" {
		t.Fatalf("two in the window plus one wanted should just fit: %d %s", code, rem)
	}
	// A batch that would cross the cap is refused whole.
	if code, body := try(2); code != http.StatusTooManyRequests || !strings.Contains(body, "rate_cap_exceeded") {
		t.Fatalf("a batch crossing the cap was admitted: %d %s", code, body)
	}
	leave(now.Add(-30 * time.Second))
	if code, body := try(1); code != http.StatusTooManyRequests {
		t.Fatalf("the cap did not hold at %d: %d %s", h.rate.Cap, code, body)
	}
	// Another requester's trail is their own.
	other := requester{ID: "did:crest:party:someone-else", Scope: "scoped"}
	rec := httptest.NewRecorder()
	if !h.underRateCap(rec, httptest.NewRequest(http.MethodPost, "/v1/verify", nil), other, 1, now) {
		t.Error("one requester's checks capped another")
	}
}
