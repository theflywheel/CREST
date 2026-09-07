package identity

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceBoundary(t *testing.T) {
	key := strings.Repeat("k", 32)
	for _, tc := range []struct {
		name, configured, supplied, path string
		want                             int
	}{
		{"no configuration", "", "", "/internal/credentials/issue", 503},
		{"no service credential", key, "", "/internal/credentials/issue", 401},
		{"wrong credential", key, strings.Repeat("x", 32), "/internal/credentials/issue", 401},
		{"service credential", key, key, "/internal/credentials/issue", 204},
		{"health remains public", "", "", "/healthz", 204},
		{"user route delegated", key, "", "/v1/verify", 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := ServiceBoundary(tc.configured)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			r := httptest.NewRequest("POST", tc.path, nil)
			r.Header.Set(ServiceTokenHeader, tc.supplied)
			r.Header.Set("Authorization", "Bearer ordinary-user-token")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
		})
	}
}
