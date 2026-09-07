package identity

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// ServiceTokenHeader carries the shared token for internal service routes.
const ServiceTokenHeader = "X-CREST-Service-Token"

// ServiceBoundary authenticates service-only routes independently of user login.
// An absent configuration fails closed, including on local deployments.
func ServiceBoundary(token string) func(http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/internal/") {
				next.ServeHTTP(w, r)
				return
			}
			if len(token) < 32 {
				deny(w, http.StatusServiceUnavailable, "service_identity_unconfigured", "service authentication is not configured")
				return
			}
			got := sha256.Sum256([]byte(r.Header.Get(ServiceTokenHeader)))
			if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
				deny(w, http.StatusUnauthorized, "service_identity_required", "this operation requires service authentication")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
