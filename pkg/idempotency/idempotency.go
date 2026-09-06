// Package idempotency provides the transaction-bound request ledger used by
// mutation handlers. A handler reserves a key in the same PostgreSQL
// transaction as its mutation and marks it complete before that transaction
// commits. A crash therefore rolls back both the mutation and the reservation;
// a retry can safely perform the work again.
//
// The ledger stores only a request fingerprint and a small resource reference.
// It deliberately does not store a response body: callers must reconstruct a
// replay from the referenced resource, so private response documents are not
// copied into a generic infrastructure table.
package idempotency

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/theflywheel/crest/pkg/store"
)

// MigrationSQL is the additive schema required by this package. Service owners
// should apply it in their own schema migration stream; CREST services do not
// share database schemas.
//
//go:embed migration.sql
var MigrationSQL string

var (
	// ErrInvalidRequest means the request identity is incomplete or malformed.
	ErrInvalidRequest = errors.New("idempotency: invalid request identity")
	// ErrFingerprint means a key was reused for a different request.
	ErrFingerprint = errors.New("idempotency: request key was reused with a different request")
	// ErrInProgress means another transaction currently owns the request key.
	ErrInProgress = errors.New("idempotency: request is already in progress")
	// ErrNotReserved means completion was attempted without the active reservation.
	ErrNotReserved = errors.New("idempotency: request is not reserved by this transaction")
)

// Request is the authenticated request fingerprint. Key is supplied by the
// caller (normally the operationId carried in an Idempotency-Key header).
// Actor is the party proven by the bearer token, never a body field.
type Request struct {
	Key    string
	Actor  string
	Method string
	// Path is the canonical request target. Include any query parameters that
	// affect the mutation (for example consent contextId), in a stable order.
	Path       string
	BodyDigest string
}

// Result contains only replay metadata. ResourceID is suitable for a
// subsequent endpoint-specific read, and Status is the status originally
// returned. A handler that has no resource to reread may leave both references
// empty and replay its own empty response for that status.
type Result struct {
	Status       int
	ResourceType string
	ResourceID   string
}

// Reservation is returned for a newly reserved request or a completed replay.
// New mutations must call Complete on the reservation in the same transaction.
type Reservation struct {
	request Request
	result  Result
	replay  bool
}

// Replay reports whether this reservation represents a completed prior request.
func (r Reservation) Replay() bool { return r.replay }

// Result returns the replay metadata recorded for a completed request.
func (r Reservation) Result() Result { return r.result }

// BodyDigest returns the canonical hexadecimal SHA-256 digest of the exact
// bytes received by the HTTP handler. The digest must be calculated before JSON
// decoding so retries are compared against the bytes that were actually sent.
func BodyDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// CanonicalPath returns a stable request target for the fingerprint. Query
// parameters are included because some mutations (such as scoped consent)
// carry their authority in the query string, and Encode sorts their keys and
// escapes values consistently.
func CanonicalPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	u := *r.URL
	u.RawQuery = u.Query().Encode()
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		return path + "?" + u.RawQuery
	}
	return path
}

func normalize(req Request) (Request, error) {
	for _, value := range []string{req.Key, req.Actor, req.Method, req.Path, req.BodyDigest} {
		for _, r := range value {
			if unicode.IsControl(r) {
				return Request{}, ErrInvalidRequest
			}
		}
	}
	req.Key = strings.TrimSpace(req.Key)
	req.Actor = strings.TrimSpace(req.Actor)
	req.Method = strings.ToUpper(strings.TrimSpace(req.Method))
	if req.Key == "" || len(req.Key) > 200 || req.Actor == "" || len(req.Actor) > 500 ||
		req.Method == "" || len(req.Method) > 16 || req.Path == "" || len(req.Path) > 4096 ||
		len(req.BodyDigest) != sha256.Size*2 {
		return Request{}, ErrInvalidRequest
	}
	if !strings.HasPrefix(req.Path, "/") {
		return Request{}, ErrInvalidRequest
	}
	if _, err := hex.DecodeString(req.BodyDigest); err != nil {
		return Request{}, ErrInvalidRequest
	}
	return req, nil
}

// Begin reserves req in q. q must be the transaction that also performs the
// mutation. PostgreSQL's unique constraint and row lock make concurrent calls
// safe: a waiting retry sees the committed completed row, while a transaction
// that crashed rolls its reservation back and allows the retry to reserve it.
func Begin(ctx context.Context, q store.Querier, req Request) (Reservation, error) {
	normalized, err := normalize(req)
	if err != nil {
		return Reservation{}, err
	}
	affected, err := q.Exec(ctx, `
		INSERT INTO idempotency_requests
			(request_key, actor_id, method, path, body_digest, state)
		VALUES ($1, $2, $3, $4, $5, 'in_progress')
		ON CONFLICT (actor_id, request_key) DO NOTHING`,
		normalized.Key, normalized.Actor, normalized.Method, normalized.Path, normalized.BodyDigest)
	if err != nil {
		return Reservation{}, fmt.Errorf("reserve idempotency request: %w", err)
	}
	if affected == 1 {
		return Reservation{request: normalized}, nil
	}

	var existing Request
	var state string
	var status *int
	var resourceType, resourceID *string
	err = q.QueryRow(ctx, `
		SELECT request_key, actor_id, method, path, body_digest, state,
		       status, resource_type, resource_id
		FROM idempotency_requests
		WHERE actor_id = $1 AND request_key = $2
		FOR UPDATE`, normalized.Actor, normalized.Key).Scan(
		&existing.Key, &existing.Actor, &existing.Method, &existing.Path, &existing.BodyDigest,
		&state, &status, &resourceType, &resourceID)
	if err != nil {
		return Reservation{}, fmt.Errorf("read idempotency request: %w", err)
	}
	if existing.Method != normalized.Method || existing.Path != normalized.Path || existing.BodyDigest != normalized.BodyDigest {
		return Reservation{}, ErrFingerprint
	}
	if state != "completed" || status == nil {
		return Reservation{}, ErrInProgress
	}
	result := Result{Status: *status}
	if resourceType != nil {
		result.ResourceType = *resourceType
	}
	if resourceID != nil {
		result.ResourceID = *resourceID
	}
	return Reservation{request: normalized, result: result, replay: true}, nil
}

// Complete records replay metadata. It must be called for a non-replay
// reservation before the handler's transaction commits. Only status and
// resource identifiers are persisted; a handler reconstructs private JSON from
// its own tables when serving a replay.
func (r Reservation) Complete(ctx context.Context, q store.Querier, result Result) error {
	if r.replay {
		return ErrNotReserved
	}
	if result.Status < 100 || result.Status > 599 ||
		(result.ResourceType == "" && result.ResourceID != "") ||
		(result.ResourceType != "" && result.ResourceID == "") {
		return fmt.Errorf("%w: invalid completion result", ErrInvalidRequest)
	}
	affected, err := q.Exec(ctx, `
		UPDATE idempotency_requests
		SET state = 'completed', status = $3, resource_type = NULLIF($4, ''),
		    resource_id = NULLIF($5, ''), completed_at = now()
		WHERE actor_id = $1 AND request_key = $2 AND method = $6 AND path = $7
		  AND body_digest = $8 AND state = 'in_progress'`,
		r.request.Actor, r.request.Key, result.Status, result.ResourceType, result.ResourceID,
		r.request.Method, r.request.Path, r.request.BodyDigest)
	if err != nil {
		return fmt.Errorf("complete idempotency request: %w", err)
	}
	if affected != 1 {
		return ErrNotReserved
	}
	return nil
}
