// Command rail is a mock mobile-money rail for local development and the harness.
//
// Two reasons it exists. Sandbox access takes weeks (#19) and P2/P3 cannot wait
// for it. More importantly, a sandbox will not produce the failures that matter —
// a timeout after the money moved, a duplicate settlement, a payment that is accepted
// but is never confirmed back. This mock injects them on demand, which is how
// W10 ("every held payment has a reason with an owner") gets tested at all.
//
// Never deployed anywhere real.
package main

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"
)

type instruction struct {
	IdempotencyKey string    `json:"idempotency_key"`
	Reference      string    `json:"reference"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	Destination    string    `json:"destination"`
	Status         string    `json:"status"`
	At             time.Time `json:"at"`
}

type rail struct {
	mu sync.Mutex
	// Keyed by idempotency key: a retried instruction must never pay twice, and
	// the mock enforces that so the test can prove the service relies on it.
	seen     map[string]instruction
	failMode string
}

func main() {
	r := &rail{seen: map[string]instruction{}}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /instructions", func(w http.ResponseWriter, req *http.Request) {
		var in instruction
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if in.IdempotencyKey == "" {
			http.Error(w, "idempotency_key is required", http.StatusBadRequest)
			return
		}

		r.mu.Lock()
		defer r.mu.Unlock()

		if prev, ok := r.seen[in.IdempotencyKey]; ok {
			// Replay: same answer, no second payment.
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Idempotent-Replay", "true")
			_ = json.NewEncoder(w).Encode(prev)
			return
		}

		switch r.failMode {
		case "timeout":
			// The nastiest real failure: the money moved, the caller never heard.
			in.Status = "confirmed"
			r.seen[in.IdempotencyKey] = in
			time.Sleep(50 * time.Millisecond)
			http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
			return
		case "reject":
			in.Status = "failed"
			r.seen[in.IdempotencyKey] = in
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(in)
			return
		}

		in.Status = "confirmed"
		in.At = time.Now().UTC()
		r.seen[in.IdempotencyKey] = in
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(in)
	})

	mux.HandleFunc("GET /instructions", func(w http.ResponseWriter, _ *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		out := make([]instruction, 0, len(r.seen))
		for _, v := range r.seen {
			out = append(out, v)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	// Fail-mode injection: "", "timeout", "reject".
	mux.HandleFunc("POST /failmode", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Mode string `json:"mode"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		r.mu.Lock()
		r.failMode = body.Mode
		r.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, _ *http.Request) {
		r.mu.Lock()
		r.seen = map[string]instruction{}
		r.failMode = ""
		r.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		os.Exit(1)
	}
}
