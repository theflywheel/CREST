package service

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/serviceauth"

	"github.com/theflywheel/crest/pkg/config"
)

func deploymentRefusal(env string, get func(string) string) error {
	// Local development may use the shared-token boundary while the generated
	// infrastructure credential is being bootstrapped. A blank profile remains
	// runnable, but any configured token must still meet the same length guard.
	if env == "local" && get("CREST_SERVICE_PEERS_JSON") == "" && get("CREST_SERVICE_PRIVATE_KEY") == "" {
		if get("CREST_SERVICE_TOKEN") == "" {
			return nil
		}
		if len(get("CREST_SERVICE_TOKEN")) < 32 {
			return fmt.Errorf("CREST_SERVICE_TOKEN must contain at least 32 bytes")
		}
		return nil
	}
	if len(get("CREST_SERVICE_TOKEN")) < 32 && get("CREST_SERVICE_PRIVATE_KEY") == "" {
		return fmt.Errorf("CREST_SERVICE_TOKEN must contain at least 32 bytes")
	}
	if env != "production" && env != "staging" && env != "acceptance" && env != "development" {
		return nil
	}
	if _, err := serviceauth.NewVerifier(get("CREST_SERVICE_PEERS_JSON")); err != nil {
		return fmt.Errorf("signed service peers are required in %s: %w", env, err)
	}
	if err := serviceauth.ValidateIdentity(get("CREST_SERVICE_ID"), get("CREST_SERVICE_PRIVATE_KEY"), get("CREST_SERVICE_PEERS_JSON")); err != nil {
		return err
	}
	if get("CREST_SERVICE_ID") == "" || get("CREST_SERVICE_PRIVATE_KEY") == "" {
		return fmt.Errorf("signed service identity is required in %s", env)
	}
	for _, key := range []string{"CREST_OIDC_ISSUER", "CREST_OIDC_JWKS_URL", "CREST_OIDC_AUDIENCE", "CREST_SUBJECT_SALT", "CREST_INSTANCE_ID", "CREST_OPERATOR_PARTY_ID"} {
		if strings.TrimSpace(get(key)) == "" {
			return fmt.Errorf("%s is required in %s", key, env)
		}
	}
	if len(get("CREST_SUBJECT_SALT")) < 32 || strings.Contains(get("CREST_SUBJECT_SALT"), "local-development") {
		return fmt.Errorf("configure a private CREST_SUBJECT_SALT of at least 32 bytes")
	}
	for _, key := range []string{"CREST_OIDC_ISSUER", "CREST_OIDC_JWKS_URL", "RAIL_URL"} {
		if v := get(key); v != "" {
			u, err := url.Parse(v)
			if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return fmt.Errorf("%s must be an HTTP service URL without credentials", key)
			}
			if env != "development" && strings.Contains(strings.ToLower(u.Hostname()), "mock") {
				return fmt.Errorf("%s selects a mock provider in %s", key, env)
			}
			if env == "production" && u.Scheme != "https" {
				return fmt.Errorf("%s requires HTTPS in production", key)
			}
		}
	}
	if get("CLOCK_DRIVEABLE") != "" && get("CLOCK_DRIVEABLE") != "false" {
		return fmt.Errorf("driveable time is forbidden in %s", env)
	}
	if get("CLOCK_START") != "" {
		return fmt.Errorf("CLOCK_START must be empty in %s", env)
	}
	if v := get("SWEEP_EVERY"); v != "" {
		d, e := time.ParseDuration(v)
		if e != nil || d <= 0 {
			return fmt.Errorf("SWEEP_EVERY must be a positive duration in %s", env)
		}
	}
	return nil
}

func deploymentSettingsRefusal(env string) error {
	return deploymentRefusal(env, func(key string) string { return config.Str(key, "") })
}
