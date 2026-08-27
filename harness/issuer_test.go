package harness

import "testing"

// The fixture's own comment warns that naming an issuer the deployment does not
// use is not a harmless mismatch. The deployed PoC fell into it anyway, so the
// seeder now adds the real one — and must add rather than replace, because the
// fixture's entry is what a local stack verifies against.
func TestWithIssuerAddsWithoutLosingWhatWasDeclared(t *testing.T) {
	got := withIssuer([]string{"did:crest:issuer:local"}, "did:crest:issuer:railway-demo")
	if len(got) != 2 || got[0] != "did:crest:issuer:local" || got[1] != "did:crest:issuer:railway-demo" {
		t.Fatalf("want the declared issuer kept and the deployment's appended, got %v", got)
	}
}

func TestWithIssuerIsIdempotent(t *testing.T) {
	in := []string{"did:crest:issuer:local"}
	if got := withIssuer(in, "did:crest:issuer:local"); len(got) != 1 {
		t.Fatalf("an issuer already authorised must not be listed twice: %v", got)
	}
}

// The fixture list is shared across every definition in the world; appending to
// it in place would leak one definition's deployment issuer into the next.
func TestWithIssuerDoesNotMutateTheCallersSlice(t *testing.T) {
	in := make([]string, 1, 4)
	in[0] = "did:crest:issuer:local"
	_ = withIssuer(in, "did:crest:issuer:railway-demo")
	if len(in) != 1 || in[0] != "did:crest:issuer:local" {
		t.Fatalf("the caller's slice was modified: %v", in)
	}
}
