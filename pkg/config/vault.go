package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Secret custody through a deployed Vault (#130, custody ruling 2026-08-29).
//
// G1 #7 settled the model — one deployment signing key, held by the operator —
// and §5 requires that custody stop being implicit. An environment variable is
// implicit custody: every process, log filter and platform dashboard that can
// see the environment can see the key. A Vault read is explicit: the secret
// lives in one audited place, reaches the one service that needs it over the
// private network, and a sealed Vault stops the signer from booting rather
// than letting it boot with a key nobody can account for.
//
// Deliberately minimal: one KV-v2 GET over HTTP, no client library, because
// the whole surface this deployment needs is "fetch one field of one secret
// at startup". Renewal, dynamic secrets and leases can arrive when something
// needs them.

// SecretStr resolves a secret by name: from Vault when VAULT_ADDR is set,
// from the environment otherwise, falling back to def.
//
// The Vault path is VAULT_SECRET_PATH (a KV-v2 data path such as
// "secret/data/crest/issuer"), the field is the lowercase of the env key, and
// the token comes from VAULT_TOKEN. When Vault is configured but unreachable,
// sealed, or missing the field, the process must not fall back to the
// environment silently — a signer that quietly signs with a different key than
// the operator escrowed is the exact implicitness this exists to end — so the
// error is returned for the caller to fail loudly on.
func SecretStr(key, def string) (string, error) {
	addr := os.Getenv("VAULT_ADDR")
	if addr == "" {
		return Str(key, def), nil
	}
	path := os.Getenv("VAULT_SECRET_PATH")
	if path == "" {
		return "", fmt.Errorf("config: VAULT_ADDR is set but VAULT_SECRET_PATH is not; half a custody configuration is none")
	}
	field := strings.ToLower(key)
	v, err := vaultField(addr, os.Getenv("VAULT_TOKEN"), path, field)
	if err != nil {
		return "", fmt.Errorf("config: %s from vault %s (field %s): %w", key, path, field, err)
	}
	return v, nil
}

func vaultField(addr, token, path, field string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(addr, "/")+"/v1/"+strings.TrimLeft(path, "/"), nil)
	if err != nil {
		return "", err
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	// KV v2 wraps the fields in data.data.
	var doc struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("vault's answer is not KV-v2 shaped: %w", err)
	}
	v, ok := doc.Data.Data[field]
	if !ok || v == "" {
		return "", fmt.Errorf("the secret has no %q field", field)
	}
	return v, nil
}
