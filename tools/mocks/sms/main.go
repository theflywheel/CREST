// Command sms is a mock SMS gateway for local development and the harness.
//
// It exists so tests can assert "the worker was notified" (W2) without a phone,
// a telco contract, or network flakiness. Messages are held in memory and
// readable over HTTP, which is what makes the assertion possible.
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

type message struct {
	To   string    `json:"to"`
	Body string    `json:"body"`
	At   time.Time `json:"at"`
}

type inbox struct {
	mu   sync.RWMutex
	msgs []message
}

func (i *inbox) add(m message) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.msgs = append(i.msgs, m)
}

func (i *inbox) all(to string) []message {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]message, 0, len(i.msgs))
	for _, m := range i.msgs {
		if to == "" || m.To == to {
			out = append(out, m)
		}
	}
	return out
}

func (i *inbox) reset() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.msgs = nil
}

func main() {
	box := &inbox{}
	mux := http.NewServeMux()

	// Send accepts what a real gateway would.
	mux.HandleFunc("POST /send", func(w http.ResponseWriter, r *http.Request) {
		var m message
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if m.To == "" || m.Body == "" {
			http.Error(w, "to and body are required", http.StatusBadRequest)
			return
		}
		m.At = time.Now().UTC()
		box.add(m)
		w.WriteHeader(http.StatusAccepted)
	})

	// Messages is the test affordance a real gateway does not give you.
	mux.HandleFunc("GET /messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(box.all(r.URL.Query().Get("to")))
	})

	// Reset keeps scenarios independent of each other.
	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, _ *http.Request) {
		box.reset()
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
