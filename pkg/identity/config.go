package identity

import (
	"fmt"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/config"
)

// Config is how a deployment names its identity provider.
//
// Every field is configuration rather than infrastructure, and deliberately:
// two deployments will reasonably disagree about which OIDC provider
// authenticates their workers and both are still CREST (the layering test).
// What is not configurable is that a caller acting in somebody's name has to
// have been authenticated by one.
type Config struct {
	// Issuer is the exact `iss` a token must carry.
	Issuer string

	// JWKSURL is where the issuer's signing keys are read from.
	//
	// Configured explicitly rather than discovered from
	// {issuer}/.well-known/openid-configuration, because #64 is open: eSignet
	// does not serve every URL its own discovery document advertises, and a
	// deployment that cannot start because discovery is wrong is worse than
	// one that was told the answer.
	JWKSURL string

	// Audience is the `aud` this deployment expects, or empty to accept any.
	// Empty is a real choice for a deployment whose provider issues untargeted
	// tokens, and it is the weaker one — a token minted for another audience
	// then replays here.
	Audience string

	// Salt is this deployment's own key for re-salting the provider's subject.
	// See the package comment for why we do not use the provider's directly.
	Salt []byte

	// Leeway forgives clock skew between here and the issuer.
	Leeway time.Duration

	// CacheFor is how long a fetched key set is reused without asking again.
	CacheFor time.Duration

	// MinRefresh is the shortest interval between key fetches provoked by an
	// unknown key id, which is what stops a stream of forged tokens becoming a
	// stream of requests to the identity provider.
	MinRefresh time.Duration

	// Extra is any additional issuers this deployment accepts (#155): the
	// migration shape where eSignet authenticates the doors while the dev
	// issuer still serves the externally-shared PoC. Each entry keeps its own
	// JWKS; audience, salt and cache posture are shared with the primary.
	Extra []Provider
}

// Provider is one additional issuer/JWKS pair.
type Provider struct {
	Issuer  string
	JWKSURL string
}

// LoadConfig reads the identity provider from the environment.
//
// The second return is whether identity is configured at all. It is not an
// error for it to be absent — a unit test binary and a `go run` of one service
// have no provider — but pkg/service refuses to start a production deployment
// without one, because an unauthenticated production CREST is one where any
// reachable client can withdraw a worker's consent.
func LoadConfig() (Config, bool, error) {
	issuer := config.Str("CREST_OIDC_ISSUER", "")
	if issuer == "" {
		return Config{}, false, nil
	}
	c := Config{
		Issuer:   issuer,
		JWKSURL:  config.Str("CREST_OIDC_JWKS_URL", ""),
		Audience: config.Str("CREST_OIDC_AUDIENCE", ""),
		Salt:     []byte(config.Str("CREST_SUBJECT_SALT", "")),
	}
	if c.JWKSURL == "" {
		return c, true, fmt.Errorf("config: CREST_OIDC_JWKS_URL is required when CREST_OIDC_ISSUER is set")
	}
	if len(c.Salt) == 0 {
		// Refused rather than defaulted to the empty key. An HMAC under a
		// known salt is a pairwise subject anybody holding the provider's
		// `sub` can recompute, which is the whole of #63 reproduced inside
		// CREST after going to the trouble of not inheriting it.
		return c, true, fmt.Errorf("config: CREST_SUBJECT_SALT is required when an identity provider is configured")
	}

	var err error
	if c.Leeway, err = config.Duration("CREST_OIDC_LEEWAY", 60*time.Second); err != nil {
		return c, true, err
	}
	if c.CacheFor, err = config.Duration("CREST_OIDC_JWKS_CACHE", 15*time.Minute); err != nil {
		return c, true, err
	}
	if c.MinRefresh, err = config.Duration("CREST_OIDC_JWKS_MIN_REFRESH", time.Minute); err != nil {
		return c, true, err
	}
	// "issuer|jwks_url" pairs, comma-separated. Malformed entries are refused,
	// not skipped: a provider that silently is not there is a login outage
	// diagnosed from the wrong end.
	if extra := config.Str("CREST_OIDC_EXTRA_PROVIDERS", ""); extra != "" {
		for _, pair := range strings.Split(extra, ",") {
			iss, jwks, ok := strings.Cut(strings.TrimSpace(pair), "|")
			if !ok || iss == "" || jwks == "" {
				return c, true, fmt.Errorf("config: CREST_OIDC_EXTRA_PROVIDERS entry %q is not issuer|jwks_url", pair)
			}
			c.Extra = append(c.Extra, Provider{Issuer: iss, JWKSURL: jwks})
		}
	}
	return c, true, nil
}
