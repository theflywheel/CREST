package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// The JSON conventions every CREST service shares.
//
// Errors carry a code and a human sentence, and the sentence is written for
// whoever will read it — including, eventually, a worker being told why a
// payment is held. "500 internal error" is a message that makes someone else's
// day worse, so the codes here are specific enough to act on.

// Error is the error body every service returns.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`

	// Problems is the per-field detail from schema validation, when there is
	// any. A form that reports its problems one at a time is a form nobody
	// finishes.
	Problems []string `json:"problems,omitempty"`
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, code int, v any) { writeJSON(w, code, v) }

// WriteError writes a structured error.
func WriteError(w http.ResponseWriter, status int, code, format string, args ...any) {
	writeJSON(w, status, Error{Code: code, Message: fmt.Sprintf(format, args...)})
}

// WriteProblems writes a validation failure with its field-level detail.
func WriteProblems(w http.ResponseWriter, code, message string, problems []string) {
	writeJSON(w, http.StatusUnprocessableEntity, Error{Code: code, Message: message, Problems: problems})
}

// ReadJSON decodes a request body into v, rejecting unknown fields.
//
// Rejecting unknown fields is deliberate: a client that misspells a field name
// should be told, not silently ignored. On records that decide whether someone
// gets paid, a dropped field is a wrong number nobody notices.
func ReadJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_body", "cannot read the request: %v", err)
		return false
	}
	return true
}

// Fail logs an unexpected error and answers 500 without leaking its detail.
// The log has the cause; the response has something a caller can quote.
func Fail(w http.ResponseWriter, log *slog.Logger, what string, err error) {
	log.Error(what, "error", err)
	WriteError(w, http.StatusInternalServerError, "internal_error",
		"%s failed; the service logged the cause", what)
}

// NotFoundOr writes 404 for a missing row and 500 for anything else, which is
// the branch nearly every read handler needs.
func NotFoundOr(w http.ResponseWriter, log *slog.Logger, what string, err, notFound error) {
	if errors.Is(err, notFound) {
		WriteError(w, http.StatusNotFound, "not_found", "no such %s", what)
		return
	}
	Fail(w, log, "read "+what, err)
}
