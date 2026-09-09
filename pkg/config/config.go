// Package config loads service configuration from the environment.
//
// Secrets come from the environment, never from the repo — a config value that
// had to be committed to make a deploy work is a leaked credential with extra
// steps (docs/TESTING.md, skill: verify-deploy).
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Base is the configuration every CREST service needs.
type Base struct {
	ServiceName string
	Addr        string
	DatabaseURL string
	LogLevel    string
	Env         string // local | staging | production
}

// LoadBase reads the common settings for a service.
func LoadBase(service string) (Base, error) {
	b := Base{
		ServiceName: service,
		Addr:        Str("ADDR", ":8080"),
		DatabaseURL: Str("DATABASE_URL", ""),
		LogLevel:    Str("LOG_LEVEL", "info"),
		Env:         Str("CREST_ENV", "local"),
	}
	if b.Env != "local" && b.DatabaseURL == "" {
		return b, fmt.Errorf("config: DATABASE_URL is required when CREST_ENV=%s", b.Env)
	}
	return b, nil
}

// Str reads a string from the environment, or returns def.
func Str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// Duration reads a duration such as "168h", or returns def.
func Duration(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def, fmt.Errorf("config: %s=%q is not a duration: %w", key, v, err)
	}
	return d, nil
}

// PositiveDuration reads a duration that must be greater than zero.
//
// Every time-bound behaviour in CREST is a configured duration since the
// driveable clock was removed (ruled 2026-09-09), and a zero or negative one
// is not a shorter window — it is a window that closes the instant it opens,
// or a sweep loop that spins. A deployment that asked for that has made a
// mistake, and on a system whose records decide whether someone gets paid the
// mistake must be refused at start-up rather than discovered by a worker whose
// chance to object lasted no time at all.
func PositiveDuration(key string, def time.Duration) (time.Duration, error) {
	d, err := Duration(key, def)
	if err != nil {
		return def, err
	}
	if d <= 0 {
		return def, fmt.Errorf("config: %s=%s must be a positive duration", key, d)
	}
	return d, nil
}

// Bool reads a boolean, or returns def.
func Bool(key string, def bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def, fmt.Errorf("config: %s=%q is not a boolean: %w", key, v, err)
	}
	return b, nil
}

// MustBool reads a boolean and falls back to def on anything unreadable.
//
// Used for flags where a malformed value should not stop a service starting —
// the safe reading of an unparseable "true-ish" string is the default, and the
// default is always the more conservative behaviour.
func MustBool(key string, def bool) bool {
	v, err := Bool(key, def)
	if err != nil {
		return def
	}
	return v
}
