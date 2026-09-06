package verification

import (
	"fmt"
	"strings"
)

func validateIssuerDeployment(env, issuerID, seed string) error {
	if strings.TrimSpace(issuerID) == "" || strings.TrimSpace(seed) == "" {
		return fmt.Errorf("explicit ISSUER_ID and private ISSUER_SEED are required")
	}
	if env != "local" && (issuerID == "did:crest:issuer:local" || seed == "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=") {
		return fmt.Errorf("development issuer identity or published seed refused outside local development")
	}
	return nil
}
