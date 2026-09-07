package contract_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/store"
)

func TestEveryServiceMigratesAnEmptyStore(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires an isolated CREST_TEST_DATABASE_URL")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"core/parties", "core/definitions", "core/evidence", "core/verification", "core/attestation", "payments"} {
		t.Run(member, func(t *testing.T) {
			schema := fmt.Sprintf("migrations_contract_%d", time.Now().UnixNano())
			db, err := store.Open(context.Background(), dsn, schema, clock.System{})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			defer func() {
				_, err := db.Q().Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
				if err != nil {
					t.Error(err)
				}
			}()
			source := os.DirFS(filepath.Join(root, "services", member))
			if err := db.Migrate(context.Background(), source, "migrations"); err != nil {
				t.Fatal(err)
			}
			if err := db.Migrate(context.Background(), source, "migrations"); err != nil {
				t.Fatalf("migration replay: %v", err)
			}
		})
	}
}
