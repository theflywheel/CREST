// Command devnotifications is a development-only durable HTTP notification
// inbox. It accepts a notification as a provider would, stores it before
// acknowledging the request, and gives a local browser a way to open the
// acknowledgement link. Viewing a message never tells attestation that the
// worker was reached; the worker's separate acknowledgement route remains the
// source of that fact.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/notify"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

//go:embed inbox.html
var inboxPage embed.FS

const maxNotificationBody = 1 << 20

type handler struct {
	d     service.Deps
	token string
}

type storedMessage struct {
	ProviderID     string    `json:"providerId"`
	To             string    `json:"to"`
	Subject        string    `json:"subject"`
	Body           string    `json:"body"`
	Acknowledgment string    `json:"acknowledgmentUrl"`
	AcceptedAt     time.Time `json:"acceptedAt"`
}

func main() {
	token := strings.TrimSpace(config.Str("DEVNOTIFY_HTTP_TOKEN", config.Str("NOTIFY_HTTP_TOKEN", "")))
	service.Main("notify", service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		OnStart: func(_ context.Context, d service.Deps) error {
			return validateConfig(d.Config.Env, token)
		},
		Routes: func(mux *http.ServeMux, d service.Deps) {
			routes(mux, d, token)
		},
	})
}

func validateConfig(env, token string) error {
	if env != "local" {
		return fmt.Errorf("devnotifications is restricted to CREST_ENV=local")
	}
	if token == "" {
		return fmt.Errorf("DEVNOTIFY_HTTP_TOKEN or NOTIFY_HTTP_TOKEN is required")
	}
	return nil
}

//go:embed migrations/*.sql
var migrations embed.FS

func routes(mux *http.ServeMux, d service.Deps, token string) {
	h := &handler{d: d, token: token}
	mux.HandleFunc("GET /", h.inbox)
	mux.HandleFunc("GET /inbox", h.inbox)
	mux.HandleFunc("POST /", h.receive)
	mux.HandleFunc("POST /v1/notifications", h.receive)
	mux.HandleFunc("GET /v1/inbox", h.list)
}

func (h *handler) authorized(r *http.Request) bool {
	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if h.token == "" || provided == "" || len(provided) != len(h.token) ||
		subtle.ConstantTimeCompare([]byte(provided), []byte(h.token)) != 1 {
		return false
	}
	return true
}

func (h *handler) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if h.authorized(r) {
		return true
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "notification inbox authentication required", http.StatusUnauthorized)
	return false
}

func (h *handler) receive(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) {
		return
	}
	var msg notify.Message
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxNotificationBody+1))
	if err != nil || len(raw) > maxNotificationBody {
		http.Error(w, "invalid notification body", http.StatusBadRequest)
		return
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&msg); err != nil {
		http.Error(w, "invalid notification JSON", http.StatusBadRequest)
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		http.Error(w, "notification body contains more than one JSON value", http.StatusBadRequest)
		return
	}
	if err := validateMessage(msg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	digest := messageDigest(msg)
	providerID := "devnotify-" + digest[:24]
	acceptedAt := h.d.Clock.Now()
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		_, err := tx.Exec(r.Context(), `
			INSERT INTO inbox_messages
				(message_digest, provider_id, recipient, subject, body, acknowledgement_url, accepted_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (message_digest) DO NOTHING`,
			digest, providerID, msg.To, msg.Subject, msg.Body, msg.Acknowledgment, acceptedAt)
		return err
	})
	if err != nil {
		http.Error(w, "notification inbox unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "providerId": providerID})
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) {
		return
	}
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed < 1 || parsed > 500 {
			http.Error(w, "limit must be between 1 and 500", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	to := r.URL.Query().Get("to")
	rows, err := h.d.DB.Q().Query(r.Context(), `
		SELECT provider_id, recipient, subject, body, acknowledgement_url, accepted_at
		FROM inbox_messages
		WHERE ($1 = '' OR recipient = $1)
		ORDER BY accepted_at DESC, provider_id DESC LIMIT $2`, to, limit)
	if err != nil {
		http.Error(w, "notification inbox unavailable", http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()
	out := make([]storedMessage, 0)
	for rows.Next() {
		var item storedMessage
		if err := rows.Scan(&item.ProviderID, &item.To, &item.Subject, &item.Body, &item.Acknowledgment, &item.AcceptedAt); err != nil {
			http.Error(w, "notification inbox unavailable", http.StatusServiceUnavailable)
			return
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "notification inbox unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "count": len(out)})
}

func (h *handler) inbox(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page, err := inboxPage.ReadFile("inbox.html")
	if err != nil {
		http.Error(w, "inbox unavailable", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(page)
}

func validateMessage(msg notify.Message) error {
	if strings.TrimSpace(msg.To) == "" {
		return fmt.Errorf("notification recipient is required")
	}
	if strings.ContainsAny(msg.To+msg.Subject, "\r\n") {
		return fmt.Errorf("notification recipient and subject cannot contain newlines")
	}
	if strings.TrimSpace(msg.Subject) == "" || strings.TrimSpace(msg.Body) == "" {
		return fmt.Errorf("notification subject and body are required")
	}
	return nil
}

func messageDigest(msg notify.Message) string {
	sum := sha256.Sum256([]byte(msg.To + "\x00" + msg.Subject + "\x00" + msg.Body + "\x00" + msg.Acknowledgment))
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
