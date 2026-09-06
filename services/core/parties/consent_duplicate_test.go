package parties

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
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

type consentDuplicateBlobs struct {
	putKeys    []string
	deleteKeys []string
}

func (b *consentDuplicateBlobs) Put(context.Context, string, io.Reader, string) (store.Blob, error) {
	return store.Blob{}, os.ErrInvalid
}

func (b *consentDuplicateBlobs) PutPrepared(_ context.Context, key string, body io.Reader, contentType string) (store.Blob, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return store.Blob{}, err
	}
	b.putKeys = append(b.putKeys, key)
	return store.Blob{Key: key, Digest: "sha256:" + hex.EncodeToString(make([]byte, 32)), Size: int64(len(raw)), ContentType: contentType}, nil
}

func (b *consentDuplicateBlobs) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}

func (b *consentDuplicateBlobs) Delete(_ context.Context, key string) error {
	b.deleteKeys = append(b.deleteKeys, key)
	return nil
}

func (b *consentDuplicateBlobs) Exists(context.Context, string) (bool, error) { return false, nil }

func TestDuplicateLiveEnrolmentConsentIsConflictAndDoesNotReplaceRecording(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	schemaName := "consent_duplicate_" + time.Now().UTC().Format("20060102150405.000000000")
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
		partyID   = "did:crest:party:01JCREST00000000000000WRKA"
		contextID = "crest:context:01JCREST00000000000000PRJC"
	)
	party := schema.Party{ID: partyID, Kind: schema.PartyKindPerson, DisplayName: "Consent worker",
		ContactRoutes: []schema.PartyContactRoutesItem{{Kind: schema.PartyContactRoutesItemKindPhone, Value: "+15550100011"}},
		CreatedAt:     time.Now().UTC()}
	project := schema.Context{ID: contextID, Kind: "project", Name: "Consent project", OwnerPartyID: partyID,
		Period: schema.Period{Start: time.Now().UTC()}, State: schema.ContextStateACTIVE}
	if err := db.InTx(ctx, func(tx store.Querier) error {
		if err := insertParty(ctx, tx, party); err != nil {
			return err
		}
		return insertContext(ctx, tx, project)
	}); err != nil {
		t.Fatal(err)
	}

	blobs := &consentDuplicateBlobs{}
	d := service.Deps{Config: config.Base{Env: "local"}, DB: db, Clock: clock.System{}, Log: slog.Default(),
		Authenticating: true, Blobs: blobs, Permits: func(context.Context, string, string, string) (bool, error) { return true, nil }}
	h := &handlers{d: d}
	audio, err := os.ReadFile("../../../harness/fixtures/consent.ogg")
	if err != nil {
		t.Fatal(err)
	}
	request := func(key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost,
			"/v1/parties/"+partyID+"/consents?moment=enrolment&captureMethod=voice&purpose=history&capturedBy="+partyID+"&contextId="+contextID,
			bytes.NewReader(audio))
		r.SetPathValue("id", partyID)
		r.Header.Set("Content-Type", "audio/ogg")
		r.Header.Set("Idempotency-Key", key)
		r = r.WithContext(identity.NewContext(r.Context(), identity.Caller{Subject: "worker-subject", PartyID: partyID}))
		w := httptest.NewRecorder()
		h.recordConsent(w, r)
		return w
	}

	first := request("consent-first")
	if first.Code != http.StatusCreated {
		t.Fatalf("first consent status = %d: %s", first.Code, first.Body.String())
	}
	duplicate := request("consent-second")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("distinct-key duplicate status = %d: %s", duplicate.Code, duplicate.Body.String())
	}
	if !bytes.Contains(duplicate.Body.Bytes(), []byte("enrolment_consent_already_live")) {
		t.Fatalf("duplicate response did not identify the live consent: %s", duplicate.Body.String())
	}
	replay := request("consent-first")
	if replay.Code != http.StatusCreated {
		t.Fatalf("same-key replay status = %d: %s", replay.Code, replay.Body.String())
	}
	var created, replayed Consent
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || replayed.ID != created.ID {
		t.Fatal("same-key replay changed the consent identity")
	}
	for _, key := range blobs.deleteKeys {
		if key == blobs.putKeys[0] {
			t.Fatal("duplicate capture deleted the original recording")
		}
	}
	var intents int
	if err := db.Q().QueryRow(ctx, `SELECT count(*) FROM consent_upload_intents i WHERE NOT EXISTS (SELECT 1 FROM consents c WHERE c.artefact_key=i.object_key)`).Scan(&intents); err != nil {
		t.Fatal(err)
	}
	if intents != 0 {
		t.Fatalf("left %d unreferenced upload intents after conflict/replay", intents)
	}
	var count int
	if err := db.Q().QueryRow(ctx, `SELECT count(*) FROM consents WHERE party_id=$1 AND context_id=$2 AND revoked_at IS NULL`, partyID, contextID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(blobs.putKeys) != 1 {
		t.Fatalf("live consents=%d uploads=%d, want one of each", count, len(blobs.putKeys))
	}
	if len(blobs.deleteKeys) != 1 {
		t.Fatalf("duplicate upload cleanup count=%d, want one", len(blobs.deleteKeys))
	}
}
