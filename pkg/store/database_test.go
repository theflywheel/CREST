package store

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
)

func databaseForTest(t *testing.T) *DB {
	t.Helper()
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	name := fmt.Sprintf("contract_%d", time.Now().UnixNano())
	db, err := Open(context.Background(), dsn, name, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.pool.Exec(context.Background(), "DROP SCHEMA "+quoteIdent(name)+" CASCADE")
		db.Close()
	})
	return db
}

func TestConcurrentMigrationsSerializeAndDetectEditedHistory(t *testing.T) {
	db := databaseForTest(t)
	fs := fstest.MapFS{"migrations/0001.sql": {Data: []byte("CREATE TABLE once_only(id integer PRIMARY KEY); INSERT INTO once_only VALUES (1);")}}
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- db.Migrate(context.Background(), fs, "migrations") }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	fs["migrations/0001.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE changed_history(id integer);")}
	if err := db.Migrate(context.Background(), fs, "migrations"); err == nil {
		t.Fatal("edited applied migration was accepted")
	}
}

func TestOutboxLeaseAndStaleAcknowledgement(t *testing.T) {
	db := databaseForTest(t)
	ctx := context.Background()
	fs := fstest.MapFS{"migrations/0001.sql": {Data: []byte(`CREATE TABLE outbox (
 id bigserial PRIMARY KEY,topic text NOT NULL,payload jsonb NOT NULL,
 attempts integer NOT NULL DEFAULT 0,created_at timestamptz NOT NULL DEFAULT now(),
 claimed_at timestamptz,next_attempt_at timestamptz,delivered_at timestamptz,last_error text);`)}}
	if err := db.Migrate(ctx, fs, "migrations"); err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(q Querier) error { return Enqueue(ctx, q, "contract", map[string]string{"operation": "test"}) }); err != nil {
		t.Fatal(err)
	}
	first, err := db.Claim(ctx, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %v %v", first, err)
	}
	second, err := db.Claim(ctx, 1)
	if err != nil || len(second) != 0 {
		t.Fatalf("live lease reclaimed: %v %v", second, err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE outbox SET claimed_at=now()-interval '3 minutes'"); err != nil {
		t.Fatal(err)
	}
	retry, err := db.Claim(ctx, 1)
	if err != nil || len(retry) != 1 {
		t.Fatalf("expired lease not recovered: %v %v", retry, err)
	}
	if err := db.Delivered(ctx, first[0].ID, first[0].Attempts); err != nil {
		t.Fatal(err)
	}
	if pending, err := db.Pending(ctx); err != nil || pending != 1 {
		t.Fatalf("stale acknowledgement completed retry: %d %v", pending, err)
	}
	if err := db.Delivered(ctx, retry[0].ID, retry[0].Attempts); err != nil {
		t.Fatal(err)
	}
	if pending, err := db.Pending(ctx); err != nil || pending != 0 {
		t.Fatalf("current acknowledgement failed: %d %v", pending, err)
	}
}

func TestServiceAuthNonceClaimIsAtomicAndBounded(t *testing.T) {
	db := databaseForTest(t)
	if err := db.Migrate(context.Background(), fstest.MapFS{
		"migrations/0001.sql": {Data: []byte("SELECT 1;")},
	}, "migrations"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	expires := time.Now().Add(time.Minute)
	const callers = 16
	results := make(chan bool, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := db.ClaimServiceNonce(ctx, "payments", "nonce-1", expires)
			if err != nil {
				t.Errorf("claim nonce: %v", err)
				return
			}
			results <- claimed
		}()
	}
	wg.Wait()
	close(results)
	claimed := 0
	for result := range results {
		if result {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("concurrent nonce claims = %d, want exactly one", claimed)
	}

	var rows int
	if err := db.Q().QueryRow(ctx,
		`SELECT count(*) FROM service_auth_nonces WHERE service_id = $1`, "payments").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("nonce retention rows = %d, want 1", rows)
	}
	claimedExpired, err := db.ClaimServiceNonce(ctx, "payments", "expired", time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !claimedExpired {
		t.Fatal("an expired nonce was not claimable")
	}
	if claimedFresh, err := db.ClaimServiceNonce(ctx, "payments", "fresh", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	} else if !claimedFresh {
		t.Fatal("a fresh nonce was not claimable")
	}
	if err := db.Q().QueryRow(ctx,
		`SELECT count(*) FROM service_auth_nonces WHERE expires_at <= now()`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("expired nonce rows after prune = %d, want 0", rows)
	}
}

func TestAdoptLegacySchemaConcurrentRenameIsSerialized(t *testing.T) {
	db := databaseForTest(t)
	former := db.Schema() + "_legacy"
	if _, err := db.pool.Exec(context.Background(), "CREATE SCHEMA "+quoteIdent(former)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(context.Background(), "CREATE TABLE "+quoteIdent(former)+`.marker (id integer PRIMARY KEY); INSERT INTO `+quoteIdent(former)+`.marker VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	db2, err := Open(context.Background(), dsn, db.Schema(), clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	errs := make(chan error, 2)
	go func() { errs <- db.AdoptLegacySchema(context.Background(), former) }()
	go func() { errs <- db2.AdoptLegacySchema(context.Background(), former) }()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	var found bool
	if err := db.Q().QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'marker')`, db.Schema()).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("legacy marker was not preserved by concurrent adoption")
	}
	if err := db.Q().QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)`, former).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("legacy schema remained after adoption")
	}
}
