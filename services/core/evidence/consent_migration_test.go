package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/store"
)

func TestConsentPayloadMigrationPurgesOnlyBlockedRows(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("evidence_consent_%d", time.Now().UnixNano())
	db, err := store.Open(ctx, dsn, schemaName, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer func() { _, _ = db.Q().Exec(ctx, "DROP SCHEMA \""+schemaName+"\" CASCADE") }()
	prior := fstest.MapFS{}
	paths, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range paths {
		if name >= "migrations/0007.sql" {
			continue
		}
		raw, err := fs.ReadFile(migrations, name)
		if err != nil {
			t.Fatal(err)
		}
		prior[name] = &fstest.MapFile{Data: raw}
	}
	if err := db.Migrate(ctx, prior, "migrations"); err != nil {
		t.Fatal(err)
	}

	created := time.Now().UTC()
	for _, batchID := range []string{"batch-consent", "batch-unattributed"} {
		if _, err := db.Q().Exec(ctx, `
			INSERT INTO batches
				(id, context_id, definition_id, definition_version, submitted_by, adapter_ref,
				 rows_total, rows_accepted, rows_unclear, created_at)
			VALUES ($1, 'crest:context:01JCREST00000000000000PROJ',
				'crest:definition:01JCREST00000000000000DEFX', 1,
				'did:crest:party:01JCREST00000000000000SPVR', 'csv-batch@1', 1, 0, 1, $2)`,
			batchID, created); err != nil {
			t.Fatal(err)
		}
	}
	const record = `{"workerJoiningIdentifier":{"kind":"national-id","value":"raw-secret"}}`
	if _, err := db.Q().Exec(ctx, `
		INSERT INTO unclear_rows (id, batch_id, row_ref, kind, reason, record, created_at)
		VALUES ('unclear-consent', 'batch-consent', 'row 2', 'consent-withdrawn', 'no consent', $1, $2),
		       ('unclear-unattributed', 'batch-unattributed', 'row 2', 'unattributed', 'no match', $1, $2)`, record, created); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		t.Fatal(err)
	}

	var blocked, retained []byte
	if err := db.Q().QueryRow(ctx, "SELECT record FROM unclear_rows WHERE id = 'unclear-consent'").Scan(&blocked); err != nil {
		t.Fatal(err)
	}
	if blocked != nil {
		t.Fatalf("consent-refused row retained a payload: %s", blocked)
	}
	if err := db.Q().QueryRow(ctx, "SELECT record FROM unclear_rows WHERE id = 'unclear-unattributed'").Scan(&retained); err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(retained, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(record), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordinary unclear row payload = %s, want %s", retained, record)
	}
}
