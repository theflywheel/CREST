package main

import (
	"fmt"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/services/payments/providers"
)

func configuredProvider(d service.Deps) (providers.Provider, error) {
	name := config.Str("PAYMENT_PROVIDER", "http")
	return providers.NewCatalogue().Build(providers.Config{
		Name: name,
		URL:  config.Str("RAIL_URL", ""),
		Env:  d.Config.Env,
		DB:   d.DB.Q(),
		Now:  d.Clock.Now,
	})
}

func mustConfiguredProvider(d service.Deps) providers.Provider {
	p, err := configuredProvider(d)
	if err != nil {
		panic(fmt.Sprintf("payment provider configuration invalid: %v", err))
	}
	return p
}
