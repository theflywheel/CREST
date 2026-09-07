package verification

import "testing"

func TestDeploymentRejectsMissingAndPublishedIssuerSecrets(t *testing.T) {
	for _, tc := range []struct{ env, id, seed string }{
		{"local", "", ""}, {"acceptance", "issuer", ""}, {"production", "did:crest:issuer:local", "private"},
		{"acceptance", "issuer", "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="},
	} {
		if err := validateIssuerDeployment(tc.env, tc.id, tc.seed); err == nil {
			t.Fatal("unsafe issuer configuration allowed")
		}
	}
}
