package contract

import (
	"testing"

	"github.com/theflywheel/crest/pkg/schema"
)

// A rate is terms, not a setting (F-1): a publication may carry who authored
// it and which version it supersedes, and both are optional because rates
// attached before this wave have neither — inventing an author would be a lie.
func TestPaymentSetupCarriesAuthoringProvenance(t *testing.T) {
	payload := map[string]any{
		"ratePerOutcomeUnit": map[string]any{"amountMinor": 15000, "currency": "KES"},
		"payerPartyId":       "did:crest:party:01HZX5T9G2K4M6P8Q0R2S4T6V8",
		"effectiveFrom":      "2026-09-01T00:00:00Z",
		"authoredByPartyId":  "did:crest:party:01HZX5T9G2K4M6P8Q0R2S4T6W9",
		"supersedesVersion":  1,
	}
	if err := schema.Validate(schema.IDPaymentSetup, payload); err != nil {
		t.Fatalf("a rate publication with author and supersession was refused: %v", err)
	}
}

// The legacy shape — no author, no supersession — must keep validating, or
// every rate already attached in a deployment becomes retroactively invalid.
func TestPaymentSetupWithoutProvenanceStillValidates(t *testing.T) {
	payload := map[string]any{
		"ratePerOutcomeUnit": map[string]any{"amountMinor": 15000, "currency": "KES"},
		"payerPartyId":       "did:crest:party:01HZX5T9G2K4M6P8Q0R2S4T6V8",
		"effectiveFrom":      "2026-02-01T00:00:00Z",
	}
	if err := schema.Validate(schema.IDPaymentSetup, payload); err != nil {
		t.Fatalf("the pre-wave payment-setup shape was refused: %v", err)
	}
}

// The payload stays closed: a rate that smuggles extra vocabulary — a stored
// tier, a definition override — is refused rather than stored.
func TestPaymentSetupRefusesUnknownFields(t *testing.T) {
	payload := map[string]any{
		"ratePerOutcomeUnit": map[string]any{"amountMinor": 15000, "currency": "KES"},
		"payerPartyId":       "did:crest:party:01HZX5T9G2K4M6P8Q0R2S4T6V8",
		"effectiveFrom":      "2026-09-01T00:00:00Z",
		"outcomeUnit":        "per bednet", // the definition's field, not the rate's
	}
	if err := schema.Validate(schema.IDPaymentSetup, payload); err == nil {
		t.Fatal("a rate payload redefining the unit was accepted; the author prices, never redefines (f1_3)")
	}
}
