package verification

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/dedi"
	"github.com/theflywheel/crest/pkg/store"
)

// Uses only the disposable database named by CREST_TEST_DATABASE_URL. It proves
// the pin round-trips through the singleton row and that a later pin replaces
// the earlier one rather than accumulating (#241).
func checkpointStore(t *testing.T) (pgCheckpointStore, func()) {
	t.Helper()
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	db, err := store.Open(ctx, dsn, fmt.Sprintf("verification_cp_%d", time.Now().UnixNano()))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		cancel()
		t.Fatal(err)
	}
	return pgCheckpointStore{db.Q()}, func() { db.Close(); cancel() }
}

// noteFor builds a parseable (unsigned) checkpoint note with a distinct root per
// size, so the test can assert what came back.
func noteFor(t *testing.T, origin string, size int64) dedi.Checkpoint {
	t.Helper()
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s@%d", origin, size)))
	raw := []byte(fmt.Sprintf("%s\n%d\n%s\n", origin, size, base64.StdEncoding.EncodeToString(sum[:])))
	cp, err := dedi.ParseCheckpointNote(raw)
	if err != nil {
		t.Fatalf("parse note: %v", err)
	}
	return cp
}

func TestCheckpointPinRoundTripsAndIsSingleton(t *testing.T) {
	s, done := checkpointStore(t)
	defer done()
	ctx := context.Background()

	// Nothing pinned yet.
	if _, ok, err := s.Load(ctx); err != nil || ok {
		t.Fatalf("empty load: ok=%v err=%v", ok, err)
	}

	first := noteFor(t, "crest.test/dedi", 2)
	first.Verified = true
	if err := s.Save(ctx, first); err != nil {
		t.Fatalf("save first: %v", err)
	}
	got, ok, err := s.Load(ctx)
	if err != nil || !ok {
		t.Fatalf("load first: ok=%v err=%v", ok, err)
	}
	if got.Size != 2 || got.Origin != "crest.test/dedi" || got.Root != first.Root || !got.Verified {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	// A later pin replaces the earlier one; the row stays singular.
	second := noteFor(t, "crest.test/dedi", 5)
	if err := s.Save(ctx, second); err != nil {
		t.Fatalf("save second: %v", err)
	}
	got, ok, err = s.Load(ctx)
	if err != nil || !ok {
		t.Fatalf("load second: ok=%v err=%v", ok, err)
	}
	if got.Size != 5 || got.Root != second.Root {
		t.Fatalf("pin did not advance to size 5: %+v", got)
	}
	if got.Verified {
		t.Fatal("second pin was saved with Verified=false; Load should reflect that")
	}

	var rows int
	if err := s.q.QueryRow(ctx, `SELECT count(*) FROM dedi_checkpoint_pin`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected a single pin row, got %d", rows)
	}
}
