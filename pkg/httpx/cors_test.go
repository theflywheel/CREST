package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The default is refusal by silence: no configured origins, no CORS headers,
// and never a wildcard — tokens ride these requests.
func TestNoOriginsMeansNoCORSHeadersNotAWildcard(t *testing.T) {
	h := CORSFromOrigins("")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/x", nil)
	req.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unconfigured CORS answered %q", got)
	}
}

func TestOnlyNamedOriginsAreEchoedAndPreflightIsAnswered(t *testing.T) {
	h := CORSFromOrigins("http://localhost:59100, https://app.example")(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(418) }))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/v1/x", nil)
	req.Header.Set("Origin", "http://localhost:59100")
	req.Header.Set("Access-Control-Request-Method", "POST")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:59100" {
		t.Fatalf("preflight from a named origin: %d %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/x", nil)
	req.Header.Set("Origin", "https://elsewhere.example")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("an unnamed origin was echoed: %q", got)
	}
}
