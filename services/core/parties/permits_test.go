package parties

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/store"
)

func TestPermitsRejectsPhantomContextForInstanceGrant(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("permits_%d", time.Now().UnixNano())
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
		partyID  = "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAV"
		grantID  = "crest:authorization:01ARZ3NDEKTSV4RRFFQ69G5FAV"
		existing = "crest:project:01ARZ3NDEKTSV4RRFFQ69G5FAV"
		phantom  = "crest:project:01ARZ3NDEKTSV4RRFFQ69G5FB0"
		function = "work-definition-source-owner"
	)
	now := clock.System{}.Now()
	if _, err := db.Q().Exec(ctx, `
		INSERT INTO contexts (id, kind, state, doc) VALUES ($1, 'project', 'ACTIVE', '{}')`, existing); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Q().Exec(ctx, `
		INSERT INTO authorizations
		    (id, party_id, scope_kind, context_id, functions, period_start, state, doc)
		VALUES ($1, $2, 'instance', NULL, $3, $4, 'ACTIVE', '{}')`,
		grantID, partyID, []string{function}, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	permitted, _, err := permits(ctx, db.Q(), partyID, function, existing, now)
	if err != nil || !permitted {
		t.Fatalf("instance grant for existing context = %v, err %v; want permitted", permitted, err)
	}
	permitted, _, err = permits(ctx, db.Q(), partyID, function, phantom, now)
	if err != nil {
		t.Fatal(err)
	}
	if permitted {
		t.Fatal("instance grant permitted a nonexistent context")
	}
	// The existing empty-context query remains the instance-scope predicate.
	permitted, _, err = permits(ctx, db.Q(), partyID, function, "", now)
	if err != nil || !permitted {
		t.Fatalf("instance grant without context = %v, err %v; want permitted", permitted, err)
	}
}
