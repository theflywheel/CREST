// Command notify is an authenticated development notification transport.
//
// It accepts a notification as a provider would, keeps the accepted inbox
// available to the local harness, and never calls the worker or marks reach
// itself. The harness extracts the acknowledgement link and submits it through
// the public window endpoint with the worker's real bearer token.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type message struct {
	ProviderID     string    `json:"providerId"`
	To             string    `json:"to"`
	Subject        string    `json:"subject"`
	Body           string    `json:"body"`
	Acknowledgment string    `json:"acknowledgmentUrl"`
	AcceptedAt     time.Time `json:"acceptedAt"`
	ClaimID        string    `json:"claimId,omitempty"`
}

type inbox struct {
	mu       sync.Mutex
	token    string
	messages map[string]message
}

func main() {
	i := &inbox{
		token:    env("NOTIFY_HTTP_TOKEN", "dev-notify-token"),
		messages: make(map[string]message),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /notify", i.accept)
	mux.HandleFunc("GET /messages", i.list)
	mux.HandleFunc("POST /reset", i.reset)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	addr := env("ADDR", ":8080")
	_ = (&http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe()
}

func (i *inbox) authorized(r *http.Request) bool {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == i.token && i.token != ""
}

func (i *inbox) accept(w http.ResponseWriter, r *http.Request) {
	if !i.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid notification provider credentials"})
		return
	}
	var in message
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil ||
		strings.TrimSpace(in.To) == "" || strings.TrimSpace(in.Acknowledgment) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to and acknowledgmentUrl are required"})
		return
	}
	claimID := claimID(in.Acknowledgment)
	if claimID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "acknowledgmentUrl is not a review link"})
		return
	}
	in.ClaimID = claimID
	in.AcceptedAt = time.Now().UTC()
	hash := sha256.Sum256([]byte(in.Acknowledgment))
	in.ProviderID = "dev-notify-" + hex.EncodeToString(hash[:8])
	i.mu.Lock()
	if previous, ok := i.messages[in.Acknowledgment]; ok {
		in = previous
	}
	i.messages[in.Acknowledgment] = in
	i.mu.Unlock()
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "providerId": in.ProviderID})
}

func (i *inbox) list(w http.ResponseWriter, r *http.Request) {
	if !i.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid notification provider credentials"})
		return
	}
	claim := r.URL.Query().Get("claimId")
	i.mu.Lock()
	items := make([]message, 0, len(i.messages))
	for _, m := range i.messages {
		if claim == "" || claim == m.ClaimID {
			items = append(items, m)
		}
	}
	i.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"messages": items})
}

func (i *inbox) reset(w http.ResponseWriter, r *http.Request) {
	if !i.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid notification provider credentials"})
		return
	}
	i.mu.Lock()
	i.messages = make(map[string]message)
	i.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func claimID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	fragment := strings.TrimPrefix(u.Fragment, "/")
	fragmentURL, err := url.Parse("http://notify/" + fragment)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(fragmentURL.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "review" {
		return ""
	}
	return parts[1]
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
