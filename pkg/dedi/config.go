package dedi

import (
	"fmt"
	"log/slog"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/store"
)

// Config is how a deployment says where its registry substrate is.
type Config struct {
	// URL of the DeDi node. Empty selects the Postgres fallback.
	URL string
	// Namespace every CREST public fact is written under.
	Namespace string
	// KeyID and Key are the publisher credential. Key is secret and comes from
	// the environment; there is deliberately no default and no file path.
	KeyID string
	Key   string
}

// LoadConfig reads the registry settings from the environment.
func LoadConfig() Config {
	return Config{
		URL:       config.Str("DEDI_URL", ""),
		Namespace: config.Str("DEDI_NAMESPACE", "crest"),
		KeyID:     config.Str("DEDI_KEY_ID", "crest"),
		Key:       config.Str("DEDI_PUBLISHER_KEY", ""),
	}
}

// New builds the publisher this deployment is configured for, and says out loud
// which one it got.
//
// The log line is not decoration. Running on the fallback means no verifier can
// check a CREST public fact independently, and the only moment anyone would
// notice is start-up — so it is a warning that names the consequence, not an
// info line saying "fallback mode".
//
// A configured node with a missing key is an error rather than a quiet
// downgrade to the fallback. A deployment that meant to publish to a log and
// silently did not is the worst of the three states, because everything appears
// to work.
func New(cfg Config, db *store.DB, clk clock.Clock, log *slog.Logger) (Publisher, error) {
	if cfg.URL == "" {
		if db == nil {
			return nil, fmt.Errorf("dedi: no DEDI_URL and no database to fall back to")
		}
		log.Warn("registry substrate is the Postgres fallback",
			"why", "DEDI_URL is unset",
			"consequence", "public facts carry no inclusion proof; a verifier can only trust this deployment's word")
		return NewFallback(db, clk), nil
	}
	key, err := ParseKey(cfg.KeyID, cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("dedi: DEDI_URL is set but the publisher key is unusable: %w", err)
	}
	node, err := NewNode(cfg.URL, key)
	if err != nil {
		return nil, err
	}
	log.Info("registry substrate is a DeDi node", "url", cfg.URL, "namespace", cfg.Namespace, "keyId", cfg.KeyID)
	return node, nil
}
