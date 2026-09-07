package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/theflywheel/crest/pkg/serviceauth"
)

func TestDefaultLocalProfileCanStartWithoutServiceCredentials(t *testing.T) {
	if err := deploymentRefusal("local", func(string) string { return "" }); err != nil {
		t.Fatalf("blank local profile was refused: %v", err)
	}
}

func TestLocalGeneratedTokenUsesSharedServiceBoundary(t *testing.T) {
	values := map[string]string{"CREST_SERVICE_TOKEN": "local-service-token-with-at-least-32-bytes"}
	if err := deploymentRefusal("local", func(key string) string { return values[key] }); err != nil {
		t.Fatalf("local token profile was refused: %v", err)
	}
	values["CREST_SERVICE_TOKEN"] = "too-short"
	if err := deploymentRefusal("local", func(key string) string { return values[key] }); err == nil {
		t.Fatal("short local service token was accepted")
	}
}

func TestStrictDeploymentConfigurationFailsClosed(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	peers, _ := json.Marshal(map[string]serviceauth.Peer{"core": {PublicKey: base64.StdEncoding.EncodeToString(pub), Allow: []string{"* /internal/"}}})
	valid := map[string]string{
		"CREST_SERVICE_ID": "core", "CREST_SERVICE_PRIVATE_KEY": base64.StdEncoding.EncodeToString(priv.Seed()), "CREST_SERVICE_PEERS_JSON": string(peers),
		"CREST_SERVICE_TOKEN":     "a-unique-service-credential-with-32-bytes",
		"CREST_SUBJECT_SALT":      "a-unique-subject-derivation-salt-32-bytes",
		"CREST_OIDC_ISSUER":       "https://identity.example.test",
		"CREST_OIDC_AUDIENCE":     "crest-validation-client",
		"CREST_OIDC_JWKS_URL":     "https://identity.example.test/keys",
		"CREST_INSTANCE_ID":       "crest:instance:acceptance",
		"CREST_OPERATOR_PARTY_ID": "did:crest:party:operator",
		"CLOCK_DRIVEABLE":         "false", "SWEEP_EVERY": "1m",
	}
	for _, env := range []string{"acceptance", "development"} {
		t.Run(env, func(t *testing.T) {
			if err := deploymentRefusal(env, func(k string) string { return valid[k] }); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct{ key, value string }{
				{"CREST_SERVICE_PRIVATE_KEY", ""}, {"CREST_OIDC_ISSUER", ""}, {"CREST_OIDC_AUDIENCE", ""}, {"CREST_SUBJECT_SALT", "short"},
				{"CREST_OIDC_JWKS_URL", "http://mock-oidc:8080/keys"}, {"RAIL_URL", "http://mock-rail:8080"},
				{"CLOCK_DRIVEABLE", "true"}, {"CLOCK_START", "2020-01-01T00:00:00Z"}, {"SWEEP_EVERY", "0"},
			} {
				if env == "development" && (tc.key == "RAIL_URL" || tc.key == "CREST_OIDC_JWKS_URL") {
					if err := deploymentRefusal(env, func(k string) string {
						if k == tc.key {
							return tc.value
						}
						return valid[k]
					}); err != nil {
						t.Fatal(err)
					}
					continue
				}
				t.Run(tc.key+tc.value, func(t *testing.T) {
					if err := deploymentRefusal(env, func(k string) string {
						if k == tc.key {
							return tc.value
						}
						return valid[k]
					}); err == nil {
						t.Fatal("unsafe acceptance configuration allowed")
					}
				})
			}
		})
	}
}
