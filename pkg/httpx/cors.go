package httpx

// CORS, as deployment configuration (#102's companion: a browser frontend
// cannot exist without it, and a wildcard nobody chose must not exist at all).
//
// Off by default. A deployment that serves a web product names the origins it
// serves it from; everything else keeps the browser's same-origin refusal,
// which is the correct default for an API whose tokens arrive in headers.

import (
	"net/http"
	"strings"
)

// CORSFromOrigins builds the middleware for a comma-separated origin list.
// Empty means no CORS headers at all — not a wildcard.
func CORSFromOrigins(origins string) Middleware {
	allowed := map[string]bool{}
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(strings.TrimRight(o, "/")); o != "" {
			allowed[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		if len(allowed) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && allowed[origin] {
				h := w.Header()
				// The specific origin, never *: credentials ride on these
				// requests, and echoing arbitrary origins would hand every
				// site a worker's token scope.
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CREST-On-Behalf-Of")
				// PUT is here because the J3 configuration endpoints are PUTs
				// by design (one record per key, idempotent): composition
				// choices, activation gates, the finance link, the support
				// owner. Without it the preflight fails and a door sees only
				// "Failed to fetch" — an endpoint with no browser path to
				// call it, which the service-level tests could not notice
				// because they call the service directly.
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				h.Set("Access-Control-Max-Age", "600")
				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
