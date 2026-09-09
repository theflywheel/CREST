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
	// Every time-bound behaviour is a duration now that there is no clock to
	// drive (ruled 2026-09-09), so what a deployment gets refused for is a
	// duration that is not a duration, or one that is not positive. A
	// window of zero is not a short window; it is a worker with no chance to
	// object at all.
	for _, key := range []string{
		"CONFIRMATION_WINDOW", "SWEEP_EVERY", "SOURCE_MONITOR_EVERY",
		"CLOCK_SKEW_ALERT", "HELD_RETRY_EVERY", "OUTBOX_RETRY_EVERY",
		"CREST_RECOVERY_OVERRIDE_REVIEW", "CREST_INVITE_TTL",
		"CREST_PRESENTATION_REQUEST_TTL", "CREST_BINDING_CACHE_TTL",
	} {
		v := get(key)
		if v == "" {
			continue
		}
		d, e := time.ParseDuration(v)
		if e != nil || d <= 0 {
			return fmt.Errorf("%s must be a positive duration in %s", key, env)
		}
	}
	return nil
}

func deploymentSettingsRefusal(env string) error {
	return deploymentRefusal(env, func(key string) string { return config.Str(key, "") })
}
