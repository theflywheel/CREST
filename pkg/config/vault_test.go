package config

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The custody property under test (#130): with Vault configured, the secret
// comes from Vault or the caller fails — never a silent fall-through to the
// environment, because a signer that quietly signs with a different key than
// the operator escrowed is implicit custody wearing explicit custody's name.

func vaultAnswering(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-token" {
			http.Error(w, `{"errors":["permission denied"]}`, http.StatusForbidden)
			return
		}
		if r.URL.Path != "/v1/secret/data/crest/issuer" {
			http.Error(w, `{"errors":["no handler"]}`, http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestSecretStrReadsTheVaultField(t *testing.T) {
	srv := vaultAnswering(t, http.StatusOK,
		`{"data":{"data":{"issuer_seed":"c2VlZA=="}}}`)
	defer srv.Close()
	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "test-token")
	t.Setenv("VAULT_SECRET_PATH", "secret/data/crest/issuer")
	t.Setenv("ISSUER_SEED", "the-environment-copy-that-must-not-win")

	v, err := SecretStr("ISSUER_SEED", "default")
	if err != nil {
		t.Fatal(err)
	}
	if v != "c2VlZA==" {
		t.Fatalf("got %q, want the vault's value", v)
	}
}

func TestSecretStrRefusesToFallBackWhenVaultCannotAnswer(t *testing.T) {
	srv := vaultAnswering(t, http.StatusServiceUnavailable,
		`{"errors":["Vault is sealed"]}`)
	defer srv.Close()
	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "test-token")
	t.Setenv("VAULT_SECRET_PATH", "secret/data/crest/issuer")
	t.Setenv("ISSUER_SEED", "the-environment-copy-that-must-not-win")

	if _, err := SecretStr("ISSUER_SEED", "default"); err == nil {
		t.Fatal("a sealed vault fell back to the environment silently")
	} else if !strings.Contains(err.Error(), "503") {
		t.Fatalf("the error does not carry the vault's answer: %v", err)
	}
}

func TestSecretStrRefusesAMissingField(t *testing.T) {
	srv := vaultAnswering(t, http.StatusOK, `{"data":{"data":{"other":"x"}}}`)
	defer srv.Close()
	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "test-token")
	t.Setenv("VAULT_SECRET_PATH", "secret/data/crest/issuer")

	if _, err := SecretStr("ISSUER_SEED", "default"); err == nil {
		t.Fatal("a secret without the field answered anyway")
	}
}

func TestSecretStrRefusesHalfAConfiguration(t *testing.T) {
	t.Setenv("VAULT_ADDR", "http://vault:8200")
	t.Setenv("VAULT_SECRET_PATH", "")
	if _, err := SecretStr("ISSUER_SEED", "default"); err == nil {
		t.Fatal("VAULT_ADDR without VAULT_SECRET_PATH resolved anyway")
	}
}

func TestSecretStrWithoutVaultReadsTheEnvironment(t *testing.T) {
	t.Setenv("VAULT_ADDR", "")
	t.Setenv("ISSUER_SEED", "from-env")
	v, err := SecretStr("ISSUER_SEED", "default")
	if err != nil || v != "from-env" {
		t.Fatalf("got %q, %v", v, err)
	}
}
