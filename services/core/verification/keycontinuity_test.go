package verification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/store"
)

// #232: a key that was renamed, not rotated, must keep answering under its old
// name; and a deployment holding credentials it can no longer answer for must
// refuse to start rather than answer "not valid" for every one of them.

func TestTheLegacyKeyIdStillAnswersForTheCurrentKey(t *testing.T) {
	issuer, err := credential.NewIssuer("did:crest:issuer:t", bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ISSUER_HISTORICAL_KEYS_JSON", "")
	keys, err := loadIssuerKeys(issuer)
	if err != nil {
		t.Fatal(err)
	}
	if keys["did:crest:issuer:t#key-1"] != issuer.PublicKeyMultibase() {
		t.Fatalf("the pre-#207 method id does not answer for the current key: %v", keys)
	}
	if keys[issuer.VerificationMethod()] != issuer.PublicKeyMultibase() {
		t.Fatal("the current method id is missing")
	}
	// A deployment whose #key-1 really was another key says so, and wins.
	t.Setenv("ISSUER_HISTORICAL_KEYS_JSON", `{"did:crest:issuer:t#key-1":"z6MkOtherKey"}`)
	keys, err = loadIssuerKeys(issuer)
	if err != nil {
		t.Fatal(err)
	}
	if keys["did:crest:issuer:t#key-1"] != "z6MkOtherKey" {
		t.Fatal("an explicit historical key did not override the legacy alias")
	}
}

func TestBootRefusesCredentialsItCanNoLongerAnswerFor(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, dsn, fmt.Sprintf("verification_keys_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	schemaName := db.Schema()
	defer db.Close()
	defer func() { _, _ = db.Q().Exec(context.Background(), `DROP SCHEMA "`+schemaName+`" CASCADE`) }()
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		t.Fatal(err)
	}
	const issuerID = "did:crest:issuer:t"
	insert := func(id, method string, revoked bool) {
		t.Helper()
		doc, _ := json.Marshal(map[string]any{"id": id, "issuer": issuerID,
			"proof": map[string]any{"verificationMethod": method}})
		var revokedAt *time.Time
		if revoked {
			now := time.Now().UTC()
			revokedAt = &now
		}
		if _, err := db.Q().Exec(ctx, `
			INSERT INTO credentials (id, claim_id, subject_ref, status_index, digest, doc, issued_at, revoked_at)
			VALUES ($1, $1, 'party', (SELECT coalesce(max(status_index),0)+1 FROM credentials), 'd', $2, now(), $3)`,
			id, doc, revokedAt); err != nil {
			t.Fatal(err)
		}
	}
	keys := map[string]string{issuerID + "#key-now": "z6MkNow", issuerID + "#key-1": "z6MkNow"}

	insert("c1", issuerID+"#key-now", false)
	insert("c2", issuerID+"#key-1", false)
	if unanswered, err := methodsWithoutAKey(ctx, db.Q(), issuerID, keys); err != nil || len(unanswered) != 0 {
		t.Fatalf("credentials under known methods were reported unanswered: %v %v", unanswered, err)
	}

	// A method nothing answers for — the rename that #232 found.
	insert("c3", issuerID+"#key-old", false)
	unanswered, err := methodsWithoutAKey(ctx, db.Q(), issuerID, keys)
	if err != nil || len(unanswered) != 1 || unanswered[0] != issuerID+"#key-old" {
		t.Fatalf("the unanswerable method was not named: %v %v", unanswered, err)
	}
	// A revoked credential is already withdrawn; it does not hold the boot.
	insert("c4", issuerID+"#key-gone", true)
	if unanswered, _ = methodsWithoutAKey(ctx, db.Q(), issuerID, keys); strings.Join(unanswered, ",") != issuerID+"#key-old" {
		t.Fatalf("a revoked credential's method held the boot: %v", unanswered)
	}
	// Another issuer's credentials are its own concern.
	doc, _ := json.Marshal(map[string]any{"id": "c5", "issuer": "did:crest:issuer:other",
		"proof": map[string]any{"verificationMethod": "did:crest:issuer:other#key-1"}})
	if _, err := db.Q().Exec(ctx, `INSERT INTO credentials (id, claim_id, subject_ref, status_index, digest, doc, issued_at)
		VALUES ('c5','c5','party',99,'d',$1,now())`, doc); err != nil {
		t.Fatal(err)
	}
	if unanswered, _ = methodsWithoutAKey(ctx, db.Q(), issuerID, keys); len(unanswered) != 1 {
		t.Fatalf("another issuer's method was counted: %v", unanswered)
	}
}
