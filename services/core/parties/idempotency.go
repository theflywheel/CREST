package parties

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/idempotency"
	"github.com/theflywheel/crest/pkg/store"
)

const maxIdempotentJSONBody = 8 << 20
const maxConsentBody = 64 << 20

// readIdempotentJSON keeps the exact bytes used by the caller's fingerprint
// while retaining the normal strict JSON rules used by the other party routes.
// The digest must be over the wire representation, not over a re-encoded Go
// value: two requests that only happen to decode to the same value are still
// two different signed requests.
func readIdempotentJSON(w http.ResponseWriter, r *http.Request, dst any) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxIdempotentJSONBody+1))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "cannot read the request: %v", err)
		return nil, false
	}
	if len(raw) > maxIdempotentJSONBody {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "body_too_large", "the request body is larger than this service accepts")
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "cannot read the request: %v", err)
		return nil, false
	}
	// ReadJSON historically accepts one JSON value followed by whitespace. Keep
	// that compatibility while ensuring a second value cannot be hidden in the
	// fingerprinted body.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "the request contains more than one JSON value")
		} else {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "cannot read the request: %v", err)
		}
		return nil, false
	}
	return raw, true
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if strings.TrimSpace(key) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "idempotency_key_required", "this mutation requires an Idempotency-Key")
		return "", false
	}
	return key, true
}

func readConsentBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxConsentBody+1))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "cannot read the consent recording: %v", err)
		return nil, false
	}
	if len(raw) > maxConsentBody {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "artefact_too_large", "the consent recording is larger than this service accepts")
		return nil, false
	}
	return raw, true
}

// validVoiceRecording checks both declarations carried by an upload. A MIME
// label alone is caller supplied and cannot turn text into a voice record.
func validVoiceRecording(contentType string, body []byte) bool {
	mime := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch mime {
	case "audio/webm":
		return len(body) >= 4 && bytes.Equal(body[:4], []byte{0x1a, 0x45, 0xdf, 0xa3})
	case "audio/mp4":
		return len(body) >= 12 && string(body[4:8]) == "ftyp"
	case "audio/ogg":
		return len(body) >= 4 && string(body[:4]) == "OggS"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return len(body) >= 12 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WAVE"
	default:
		return false
	}
}

func beginIdempotency(ctx context.Context, q store.Querier, r *http.Request, key, actor string, body []byte) (idempotency.Reservation, error) {
	return idempotency.Begin(ctx, q, idempotency.Request{
		Key: key, Actor: actor, Method: r.Method, Path: idempotency.CanonicalPath(r),
		BodyDigest: idempotency.BodyDigest(body),
	})
}

func writeIdempotencyError(w http.ResponseWriter, log interface{ Error(string, ...any) }, err error) {
	switch {
	case errors.Is(err, idempotency.ErrInvalidRequest):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_idempotency_request", "the Idempotency-Key and request identity are invalid")
	case errors.Is(err, idempotency.ErrFingerprint):
		httpx.WriteError(w, http.StatusConflict, "idempotency_conflict", "this Idempotency-Key was already used for a different request")
	case errors.Is(err, idempotency.ErrInProgress):
		httpx.WriteError(w, http.StatusConflict, "request_in_progress", "this Idempotency-Key is being processed; retry after the original request finishes")
	default:
		// This is a database/schema failure in normal operation. Keep its detail
		// out of the response while preserving the existing service logging path.
		log.Error("idempotency request", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "idempotency could not be recorded")
	}
}
