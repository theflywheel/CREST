// Command bootstrap-operator stands up the one party a clean deployment
// cannot create through any door: the instance operator.
//
// Everything else arrives through a door — an organisation registers itself
// at the open onboarding door and claims its record with its own login; a
// person is invited by an approved organisation; a worker enrols themselves.
// The operator is the party those doors answer to (it approves the first
// organisation), so on an empty registry there is nobody to invite it. Stand-up
// is deploy-time (Blueprint §15 G-1, "the first screen anyone ever sees"), and
// this is that deploy-time act: it writes the operator's party straight into
// the registry's own schema and mints the invitation the operator's first
// login claims. Print, set CREST_OPERATOR_PARTY_ID to the id, restart the
// core service, open the claim link, sign in with eSignet.
//
//	DATABASE_URL=postgres://… go run ./tools/bootstrap-operator \
//	    -name "CREST production operator" -email ops@example.org \
//	    -door https://crest-console-production.up.railway.app
//
// Flags may also arrive as BOOTSTRAP_NAME, BOOTSTRAP_EMAIL, BOOTSTRAP_PHONE
// and BOOTSTRAP_DOOR, because on Railway this runs as a one-off job inside
// the private network (the same shape as crest-seed) where there is no
// command line to type on.
//
// BOOTSTRAP_MODE=wipe does the other deploy-time act the runbook names and
// nothing else: it drops this deployment's five service schemas so every
// service re-creates its own from migrations on its next boot. It never
// touches a schema that is not CREST's — the identity provider shares the
// database — and it refuses to combine with a bootstrap in one run, because
// the operator's tables do not exist again until the registry has booted.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
	"github.com/theflywheel/crest/services/core/parties"
)

func main() {
	name := flag.String("name", os.Getenv("BOOTSTRAP_NAME"), "the operator organisation's display name")
	email := flag.String("email", os.Getenv("BOOTSTRAP_EMAIL"), "a contact route for the operator (email)")
	phone := flag.String("phone", os.Getenv("BOOTSTRAP_PHONE"), "a contact route for the operator (phone)")
	door := flag.String("door", os.Getenv("BOOTSTRAP_DOOR"), "the console door's origin, to print a ready claim link")
	ttl := flag.Duration("ttl", 7*24*time.Hour, "how long the claim code lives (capped by the registry's rule)")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "need DATABASE_URL")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	clk := clock.System{}

	if os.Getenv("BOOTSTRAP_MODE") == "wipe" {
		if err := wipe(ctx, dsn, clk); err != nil {
			fmt.Fprintln(os.Stderr, "wipe:", err)
			os.Exit(1)
		}
		fmt.Println(`{"wiped":["parties","definitions","evidence","verification","payments"],"next":"redeploy crest-core and crest-payments so each re-creates its schema, then run the bootstrap"}`)
		return
	}
	if *name == "" || (*email == "" && *phone == "") {
		fmt.Fprintln(os.Stderr, "need -name (BOOTSTRAP_NAME), and -email or -phone")
		os.Exit(2)
	}
	db, err := store.Open(ctx, dsn, "parties", clk)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open registry:", err)
		os.Exit(1)
	}
	defer db.Close()

	p := schema.Party{
		ID:          id.Party(clk),
		Kind:        schema.PartyKindOrganisation,
		DisplayName: *name,
		CreatedAt:   clk.Now(),
	}
	if *email != "" {
		p.ContactRoutes = append(p.ContactRoutes, schema.PartyContactRoutesItem{Kind: schema.PartyContactRoutesItemKindEmail, Value: *email})
	}
	if *phone != "" {
		p.ContactRoutes = append(p.ContactRoutes, schema.PartyContactRoutesItem{Kind: schema.PartyContactRoutesItemKindPhone, Value: *phone})
	}
	if err := schema.Validate(schema.IDParty, p); err != nil {
		fmt.Fprintln(os.Stderr, "operator party does not validate:", err)
		os.Exit(1)
	}

	code, err := parties.BootstrapOperator(ctx, db, p, *ttl)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap:", err)
		os.Exit(1)
	}
	out := map[string]any{
		"partyId":    p.ID,
		"inviteCode": code,
		"setEnv":     "CREST_OPERATOR_PARTY_ID=" + p.ID,
	}
	if *door != "" {
		out["claimUrl"] = *door + "/#/claim/" + code
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// wipe drops exactly the five schemas CREST's services own. Each service
// re-creates its own from embedded migrations on boot, so this is the whole
// of "start over" — and nothing else in the database is CREST's to drop.
func wipe(ctx context.Context, dsn string, clk clock.Clock) error {
	db, err := store.Open(ctx, dsn, "public", clk)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, s := range []string{"parties", "definitions", "evidence", "verification", "payments"} {
		if _, err := db.Q().Exec(ctx, "DROP SCHEMA IF EXISTS "+s+" CASCADE"); err != nil {
			return fmt.Errorf("drop %s: %w", s, err)
		}
	}
	return nil
}
