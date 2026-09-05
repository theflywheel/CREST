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
// Refuses to run against a registry that already has an operator-shaped party
// bound to an identity unless -force is given: the point is a clean slate,
// not a second operator.
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
	name := flag.String("name", "", "the operator organisation's display name")
	email := flag.String("email", "", "a contact route for the operator (email)")
	phone := flag.String("phone", "", "a contact route for the operator (phone)")
	door := flag.String("door", "", "the console door's origin, to print a ready claim link")
	ttl := flag.Duration("ttl", 7*24*time.Hour, "how long the claim code lives (capped by the registry's rule)")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" || *name == "" || (*email == "" && *phone == "") {
		fmt.Fprintln(os.Stderr, "need DATABASE_URL, -name, and -email or -phone")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	clk := clock.System{}
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
