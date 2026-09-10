package parties

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/dedi"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// The operator is a public fact (§3), and every credential this deployment
// issues names it. Found on the fleet on 2026-09-10: an operator stood up
// straight into the schema by the bootstrap tool was never published, so a
// verifier walking any credential's chain met an organisation nobody could
// resolve. Both ways an operator comes to exist must publish it, and boot must
// repair one that was not.
func operatorDB(t *testing.T) (*store.DB, func()) {
	t.Helper()
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("operator_%d", time.Now().UnixNano())
	db, err := store.Open(ctx, dsn, schemaName)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		t.Fatal(err)
	}
	return db, func() {
		_, _ = db.Q().Exec(ctx, "DROP SCHEMA \""+schemaName+"\" CASCADE")
		db.Close()
	}
}

func queuedFacts(t *testing.T, db *store.DB) []factMessage {
	t.Helper()
	rows, err := db.Q().Query(context.Background(), `SELECT payload FROM outbox WHERE topic = $1 ORDER BY id`, topicPublishFact)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out, err := store.Collect(rows, func(r store.Row) (factMessage, error) {
		var raw []byte
		if err := r.Scan(&raw); err != nil {
			return factMessage{}, err
		}
		var m factMessage
		return m, json.Unmarshal(raw, &m)
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheOperatorIsPublishedByBootstrapAndByBoot(t *testing.T) {
	db, done := operatorDB(t)
	defer done()
	ctx := context.Background()
	const operator = "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAV"
	org := schema.Party{
		ID: operator, Kind: schema.PartyKindOrganisation, DisplayName: "The operator", CreatedAt: time.Now().UTC(),
		ContactRoutes: []schema.PartyContactRoutesItem{{Kind: schema.PartyContactRoutesItemKind("email"), Value: "ops@example.org"}},
	}

	// The deploy-time act: the bootstrap tool's path.
	if _, err := BootstrapOperator(ctx, db, org, time.Hour); err != nil {
		t.Fatal(err)
	}
	facts := queuedFacts(t, db)
	if len(facts) != 1 || facts[0].Kind != "organisation" || facts[0].ID != operator {
		t.Fatalf("bootstrap did not queue the operator's organisation fact: %+v", facts)
	}

	// Boot: the instance and the operator go together, every time.
	withEnv(t, map[string]string{
		"CREST_INSTANCE_ID": "crest:instance:test", "CREST_INSTANCE_NAME": "Test",
		"CREST_OPERATOR_PARTY_ID": operator, "DEDI_URL": "",
	})
	deps := service.Deps{DB: db, DeDi: dedi.NewFallback(db), DeDiNamespace: "crest",
		Log: slog.New(slog.NewTextHandler(&strings.Builder{}, nil)), Config: config.Base{Env: "local"}}
	if err := publishInstance(ctx, deps); err != nil {
		t.Fatal(err)
	}
	facts = queuedFacts(t, db)
	kinds := map[string]int{}
	for _, f := range facts {
		kinds[f.Kind+":"+f.ID]++
	}
	if kinds["instance:crest:instance:test"] != 1 || kinds["organisation:"+operator] != 2 {
		t.Fatalf("boot did not queue the instance and the operator: %v", kinds)
	}

	// An operator that is not an organisation is refused at boot, not
	// published as one.
	if _, err := db.Q().Exec(ctx, `UPDATE parties SET kind = 'person', doc = jsonb_set(doc::jsonb, '{kind}', '"person"')::jsonb WHERE id = $1`, operator); err != nil {
		t.Fatal(err)
	}
	if err := publishInstance(ctx, deps); err == nil || !strings.Contains(err.Error(), "not an organisation") {
		t.Fatalf("a person configured as operator was published: %v", err)
	}
}
