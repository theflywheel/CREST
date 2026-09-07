package providers

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/theflywheel/crest/pkg/store"
)

// Catalogue is the explicit provider registry. Names are configuration, not
// values accepted from a payment request.
type Catalogue struct {
	factories map[string]Factory
}

// Factory constructs a provider from its configuration.
type Factory func(Config) (Provider, error)

// Config selects and configures a payment provider.
type Config struct {
	Name string
	URL  string
	Env  string
	DB   store.Querier
	Now  Clock
}

// NewCatalogue returns the built-in payment provider catalogue.
func NewCatalogue() Catalogue {
	c := Catalogue{factories: map[string]Factory{}}
	_ = c.Register("http", func(cfg Config) (Provider, error) { return NewHTTP(cfg.URL) })
	_ = c.Register("simulator", func(cfg Config) (Provider, error) {
		if cfg.Env != "local" && cfg.Env != "development" {
			return nil, errors.New("the simulator provider is permitted only in local or development environments")
		}
		if cfg.DB == nil {
			return nil, errors.New("the simulator provider requires the payments database")
		}
		return NewSimulator(cfg.DB, cfg.Now), nil
	})
	return c
}

// Names returns the registered provider names in stable order.
func (c Catalogue) Names() []string {
	out := make([]string, 0, len(c.factories))
	for name := range c.factories {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Register adds a provider factory under a unique name.
func (c *Catalogue) Register(name string, factory Factory) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || factory == nil {
		return errors.New("provider registration needs a name and factory")
	}
	if c.factories == nil {
		c.factories = map[string]Factory{}
	}
	if _, exists := c.factories[name]; exists {
		return fmt.Errorf("payment provider %q is already registered", name)
	}
	c.factories[name] = factory
	return nil
}

// Build constructs the provider selected by cfg.
func (c Catalogue) Build(cfg Config) (Provider, error) {
	name := strings.ToLower(strings.TrimSpace(cfg.Name))
	factory, ok := c.factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown payment provider %q", cfg.Name)
	}
	cfg.Name = name
	return factory(cfg)
}
