// Command healthcheck probes a service's own /healthz endpoint.
//
// The service images are distroless — no shell, no curl — so the container
// healthcheck has to be a binary. Compose uses this to decide readiness, which
// is what lets the harness poll instead of sleeping.
package main

import (
	"context"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := os.Getenv("HEALTHCHECK_URL")
	if addr == "" {
		addr = "http://127.0.0.1:8080/healthz"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		os.Exit(1)
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
