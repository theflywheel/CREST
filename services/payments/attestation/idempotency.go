package attestation

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

const maxDisputeBody = 8 << 20

// readDisputeJSON retains the exact request bytes used for the durable retry
// fingerprint while applying the same strict JSON rules as other mutations.
func readDisputeJSON(w http.ResponseWriter, r *http.Request, dst any) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxDisputeBody+1))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "cannot read the request: %v", err)
		return nil, false
	}
	if len(raw) > maxDisputeBody {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "body_too_large", "the dispute is larger than this service accepts")
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "cannot read the request: %v", err)
		return nil, false
	}
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

func requireDisputeIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpx.WriteError(w, http.StatusBadRequest, "idempotency_key_required", "this mutation requires an Idempotency-Key")
		return "", false
	}
	return key, true
}

func beginDisputeIdempotency(ctx context.Context, tx store.Querier, r *http.Request, key, actor string, body []byte) (idempotency.Reservation, error) {
	return idempotency.Begin(ctx, tx, idempotency.Request{
		Key: key, Actor: actor, Method: r.Method, Path: idempotency.CanonicalPath(r),
		BodyDigest: idempotency.BodyDigest(body),
	})
}

func writeDisputeIdempotencyError(w http.ResponseWriter, log interface{ Error(string, ...any) }, err error) {
	switch {
	case errors.Is(err, idempotency.ErrInvalidRequest):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_idempotency_request", "the Idempotency-Key and request identity are invalid")
	case errors.Is(err, idempotency.ErrFingerprint):
		httpx.WriteError(w, http.StatusConflict, "idempotency_conflict", "this Idempotency-Key was already used for a different request")
	case errors.Is(err, idempotency.ErrInProgress):
		httpx.WriteError(w, http.StatusConflict, "request_in_progress", "this Idempotency-Key is being processed; retry after the original request finishes")
	default:
		log.Error("dispute idempotency request", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "the dispute could not be recorded safely")
	}
}
