// The login surface (#155 phase A): CREST's server side of the eSignet
// authorization-code flow. The browser is sent to eSignet's UI with a PKCE
// challenge; the callback exchanges the code with a private_key_jwt assertion
// and hands the door the access token — which every service then verifies the
// ordinary way (CREST_OIDC_ISSUER/JWKS pointed at eSignet).
//
// This is where the mock flow's /dev/pairwise dies: the browser never learns
// how a pairwise subject is derived. What the door gets after login is the
// token and, from /v1/auth/me, this deployment's pairwise reference for the
// person and the party bound to it (if any) — subject_not_enrolled stays a
// prompt to enrol, exactly as the middleware answers it (#102).
package parties

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/esignet"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
)

// wallClock: the login handshake runs on real time, like token verification
// (pkg/identity) — never the driveable clock.
var wallClock = clock.System{}

type authConfig struct {
	client *esignet.Client
	// doors are the exact origins a login may bounce back to — anything else
	// is an open redirect wearing CREST's hostname.
	doors []string
	salt  []byte
}

// loadAuthConfig reads the eSignet relying-party configuration; absent
// ESIGNET_URL means the surface is not served (the local stack runs on
// mock-oidc and has no redirect flow).
func loadAuthConfig(log interface{ Warn(string, ...any) }) *authConfig {
	base := config.Str("ESIGNET_URL", "")
	if base == "" {
		return nil
	}
	ui := config.Str("ESIGNET_UI_URL", base)
	rp := config.Str("ESIGNET_RELYING_PARTY_ID", "crest")
	keyPEM, err := config.SecretStr("ESIGNET_CLIENT_KEY", "")
	if err != nil {
		log.Warn("eSignet login not served: client key unreadable", "error", err)
		return nil
	}
	doors := strings.Split(config.Str("CREST_AUTH_DOORS", ""), ",")
	salt := []byte(config.Str("CREST_SUBJECT_SALT", ""))
	if keyPEM == "" || len(salt) == 0 || doors[0] == "" {
		log.Warn("eSignet login not served: ESIGNET_URL is set but ESIGNET_CLIENT_KEY, CREST_AUTH_DOORS or CREST_SUBJECT_SALT is missing")
		return nil
	}
	key, kerr := esignet.ParseKey([]byte(keyPEM))
	if kerr != nil {
		log.Warn("eSignet login not served: client key does not parse", "error", kerr)
		return nil
	}
	var clean []string
	for _, d := range doors {
		if d = strings.TrimRight(strings.TrimSpace(d), "/"); d != "" {
			clean = append(clean, d)
		}
	}
	client := esignet.New(base, ui, rp, key)
	client.LogoURI = config.Str("CREST_LOGO_URL", "")
	return &authConfig{client: client, doors: clean, salt: salt}
}

// callbackFor is the redirect URI eSignet sends the browser back to: the
// door's own origin, through the proxy alias, so the cookie set at login is
// first-party when the callback reads it.
func (a *authConfig) callbackFor(door string) string {
	return door + "/api/crest-registry/v1/auth/callback"
}

// cookiePathFor scopes the state cookie to the door's own callback prefix.
// On Railway a door is a bare origin and this is /api/crest-registry/v1/auth,
// exactly as before; locally the doors share one origin under path prefixes
// (/console, /worker, …) and the callback arrives under that prefix — a cookie
// scoped without it is never sent back, and the login dies as
// no_login_in_flight with the person having done everything right.
func cookiePathFor(door string) string {
	path := ""
	if u, err := url.Parse(door); err == nil {
		path = strings.TrimRight(u.Path, "/")
	}
	return path + "/api/crest-registry/v1/auth"
}

func (a *authConfig) doorAllowed(door string) bool {
	for _, d := range a.doors {
		if d == door {
			return true
		}
	}
	return false
}

// registerAuth wires the login surface and registers the client with eSignet
// (idempotent; a failure is logged and retried on the next boot rather than
// fatal — eSignet being down must not keep the registry down).
func registerAuth(mux *http.ServeMux, d service.Deps, a *authConfig) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		uris := make([]string, 0, len(a.doors))
		for _, door := range a.doors {
			uris = append(uris, a.callbackFor(door))
		}
		if err := a.client.Register(ctx, uris); err != nil {
			d.Log.Warn("eSignet client registration failed; logins will fail until a boot succeeds", "error", err)
			return
		}
		d.Log.Info("eSignet client registered", "clientId", a.client.ClientID, "doors", len(a.doors))
	}()

	mux.HandleFunc("GET /v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		door := strings.TrimRight(r.URL.Query().Get("door"), "/")
		if !a.doorAllowed(door) {
			httpx.WriteError(w, http.StatusBadRequest, "unknown_door", "that redirect target is not one of this deployment's doors")
			return
		}
		p, err := esignet.NewPKCE()
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "entropy", "%v", err)
			return
		}
		payload, _ := json.Marshal(map[string]any{
			"state": p.State, "verifier": p.Verifier, "door": door,
			"exp": wallClock.Now().Add(10 * time.Minute).Unix(),
		})
		http.SetCookie(w, &http.Cookie{
			Name:     "crest_auth",
			Value:    esignet.SignState(a.salt, payload),
			Path:     cookiePathFor(door),
			MaxAge:   600,
			HttpOnly: true,
			Secure:   strings.HasPrefix(door, "https://"),
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, a.client.AuthorizeURL(a.callbackFor(door), p), http.StatusFound)
	})

	mux.HandleFunc("GET /v1/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("crest_auth")
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "no_login_in_flight", "no login attempt is in flight from this browser; start again at the door")
			return
		}
		payload, ok := esignet.VerifyState(a.salt, cookie.Value)
		if !ok {
			httpx.WriteError(w, http.StatusBadRequest, "bad_state", "the login attempt does not verify; start again")
			return
		}
		var st struct {
			State, Verifier, Door string
			Exp                   int64
		}
		if json.Unmarshal(payload, &st) != nil || wallClock.Now().Unix() > st.Exp {
			httpx.WriteError(w, http.StatusBadRequest, "login_expired", "the login attempt expired; start again")
			return
		}
		if r.URL.Query().Get("state") != st.State {
			httpx.WriteError(w, http.StatusBadRequest, "bad_state", "state mismatch; start again")
			return
		}
		if e := r.URL.Query().Get("error"); e != "" {
			// The person declined, or eSignet refused. Back to the door with
			// the fact, not a dead end.
			http.Redirect(w, r, st.Door+"/#/auth?error="+url.QueryEscape(e), http.StatusFound)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			httpx.WriteError(w, http.StatusBadRequest, "no_code", "eSignet sent no code and no error")
			return
		}
		tokens, err := a.client.Exchange(r.Context(), code, a.callbackFor(st.Door), st.Verifier)
		if err != nil {
			d.Log.Warn("eSignet code exchange failed", "error", err)
			http.Redirect(w, r, st.Door+"/#/auth?error=exchange_failed", http.StatusFound)
			return
		}
		// Clear the one-shot cookie; the token itself is the session now.
		http.SetCookie(w, &http.Cookie{Name: "crest_auth", Value: "", Path: cookiePathFor(st.Door), MaxAge: -1})
		// The fragment never reaches a server log — the door's #/auth route
		// picks the token out and stores it for the session.
		http.Redirect(w, r, st.Door+"/#/auth?token="+url.QueryEscape(tokens.AccessToken), http.StatusFound)
	})

	// Who am I, as this deployment sees me: the pairwise reference and the
	// party bound to it. An authenticated stranger gets their subjectRef and
	// no partyId — the state phase B's self-registration acts on.
	mux.HandleFunc("GET /v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		caller := identity.From(r.Context())
		if !caller.Authenticated() {
			httpx.WriteError(w, http.StatusUnauthorized, "no_token", "this endpoint answers about a verified caller")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"subjectRef": caller.Subject,
			"partyId":    caller.PartyID,
			"issuer":     caller.Issuer,
		})
	})
}
