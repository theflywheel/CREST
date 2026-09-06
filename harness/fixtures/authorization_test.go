package fixtures_test

import (
	"testing"

	"github.com/theflywheel/crest/harness/fixtures"
)

func TestEveryFixtureAuthorizationFunctionIsCoveredByItsTerms(t *testing.T) {
	w, err := fixtures.Load()
	if err != nil {
		t.Fatal(err)
	}
	terms := make(map[string]map[int]map[string]struct{}, len(w.Terms))
	for _, term := range w.Terms {
		permissions := make(map[string]struct{}, len(term.Permissions))
		for _, permission := range term.Permissions {
			permissions[permission] = struct{}{}
		}
		if terms[term.ID] == nil {
			terms[term.ID] = make(map[int]map[string]struct{})
		}
		terms[term.ID][term.Version] = permissions
	}
	for _, grant := range w.Authorizations {
		permissions := terms[grant.Terms.ID][grant.Terms.Version]
		for _, function := range grant.Functions {
			if _, ok := permissions[function]; !ok {
				t.Errorf("authorization %s grants %q outside terms %s v%d", grant.ID, function, grant.Terms.ID, grant.Terms.Version)
			}
		}
	}
}
