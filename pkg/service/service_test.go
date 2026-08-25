package service

import (
	"context"
	"testing"
)

func TestAProductionDeploymentWillNotStartWithoutAnIdentityProvider(t *testing.T) {
	if startupRefusal("production", false) == "" {
		t.Fatal("a production service with no identity provider was allowed to start; " +
			"any client that could reach a port could withdraw a worker's consent")
	}
	// The three that are allowed, and the reason they differ: production is the
	// only environment where an unauthenticated caller can reach a real
	// worker's record.
	for _, env := range []string{"local", "staging"} {
		if why := startupRefusal(env, false); why != "" {
			t.Errorf("%s was refused: %s", env, why)
		}
	}
	if why := startupRefusal("production", true); why != "" {
		t.Errorf("a configured production service was refused: %s", why)
	}
}

func TestAnUnconfiguredServiceRefusesRatherThanAllows(t *testing.T) {
	// The safe reading of "there is nothing to ask" is no, not yes. A
	// deployment that is more permissive because it is less configured is
	// exactly backwards.
	ok, err := refuseEverything(context.Background(), "did:crest:party:A", "act-for-party", "ctx-1")
	if ok || err != nil {
		t.Fatalf("an unconfigured authorization check returned %v, %v", ok, err)
	}
}
