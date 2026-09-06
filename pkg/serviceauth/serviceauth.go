package serviceauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
)

// ReplayStore atomically claims a service nonce until expiresAt. Implementations
// must make the (serviceID, nonce) pair unique across replicas.
type ReplayStore interface {
	ClaimServiceNonce(ctx context.Context, serviceID, nonce string, expiresAt time.Time) (bool, error)
}

// Peer describes one authenticated service and the operations it may call.
type Peer struct {
	PublicKey    string        `json:"publicKey"`
	Allow        []string      `json:"allow"`
	PreviousKeys []PreviousKey `json:"previousKeys,omitempty"`
}

// PreviousKey is a bounded rotation grace key. NotAfter is mandatory: an old
// service key must never become an unbounded historical trust anchor.
type PreviousKey struct {
	PublicKey string    `json:"publicKey"`
	NotAfter  time.Time `json:"notAfter"`
}
type peerKey struct {
	key      ed25519.PublicKey
	allow    []string
	previous []previousKey
}
type previousKey struct {
	key      ed25519.PublicKey
	notAfter time.Time
}

// Verifier authenticates signed service-to-service requests.
type Verifier struct {
	peers  map[string]peerKey
	mu     sync.Mutex
	seen   map[string]time.Time
	replay ReplayStore
}

// NewVerifier parses the configured service peers and their bounded key grace.
func NewVerifier(raw string) (*Verifier, error) {
	var peers map[string]Peer
	if err := json.Unmarshal([]byte(raw), &peers); err != nil {
		return nil, fmt.Errorf("service peers: %w", err)
	}
	v := &Verifier{peers: map[string]peerKey{}, seen: map[string]time.Time{}}
	for id, p := range peers {
		key, err := base64.StdEncoding.DecodeString(p.PublicKey)
		if err != nil || len(key) != ed25519.PublicKeySize || id == "" || len(p.Allow) == 0 {
			return nil, fmt.Errorf("invalid service peer %q", id)
		}
		previous := make([]previousKey, 0, len(p.PreviousKeys))
		for i, candidate := range p.PreviousKeys {
			if candidate.NotAfter.IsZero() {
				return nil, fmt.Errorf("invalid service peer %q previous key %d: notAfter is required", id, i)
			}
			oldKey, err := base64.StdEncoding.DecodeString(candidate.PublicKey)
			if err != nil || len(oldKey) != ed25519.PublicKeySize {
				return nil, fmt.Errorf("invalid service peer %q previous key %d", id, i)
			}
			previous = append(previous, previousKey{key: ed25519.PublicKey(oldKey), notAfter: candidate.NotAfter})
		}
		v.peers[id] = peerKey{key: ed25519.PublicKey(key), allow: p.Allow, previous: previous}
	}
	if len(v.peers) == 0 {
		return nil, fmt.Errorf("no service peers configured")
	}
	return v, nil
}

// WithReplayStore enables durable replay protection. Without one, the
// verifier keeps the existing process-local fallback, which is useful for
// isolated unit tests but must not be used by a composed database-backed
// deployment.
func (v *Verifier) WithReplayStore(store ReplayStore) *Verifier {
	v.replay = store
	return v
}

func canonical(r *http.Request, body []byte) []byte {
	digest := sha256.Sum256(body)
	return []byte(strings.Join([]string{r.Header.Get("X-CREST-Service-ID"), r.Header.Get("X-CREST-Service-Time"), r.Header.Get("X-CREST-Service-Nonce"), r.Method, r.Host, r.URL.RequestURI(), hex.EncodeToString(digest[:])}, "\n"))
}

// Sign adds the authenticated service identity and signature headers to a request.
func Sign(r *http.Request, id, encodedSeed string) error {
	seed, err := base64.StdEncoding.DecodeString(encodedSeed)
	if err != nil || len(seed) != ed25519.SeedSize || id == "" {
		return fmt.Errorf("service signing identity is invalid")
	}
	var body []byte
	if r.Body != nil && r.Body != http.NoBody {
		if r.GetBody == nil {
			return fmt.Errorf("service request body must be replayable")
		}
		reader, err := r.GetBody()
		if err != nil {
			return err
		}
		// GetBody readers are in-memory request copies; close errors cannot alter
		// the signed bytes and are therefore deliberately ignored.
		defer func() { _ = reader.Close() }()
		body, err = io.ReadAll(io.LimitReader(reader, 8<<20+1))
		if err != nil {
			return err
		}
		if len(body) > 8<<20 {
			return fmt.Errorf("service request body exceeds limit")
		}
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	r.Header.Set("X-CREST-Service-ID", id)
	r.Header.Set("X-CREST-Service-Time", strconv.FormatInt(clock.System{}.Now().Unix(), 10))
	r.Header.Set("X-CREST-Service-Nonce", base64.RawURLEncoding.EncodeToString(nonce))
	r.Header.Set("X-CREST-Service-Signature", base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), canonical(r, body))))
	return nil
}

// SignConfigured signs a request with the deployment's configured identity.
func SignConfigured(r *http.Request) error {
	return Sign(r, os.Getenv("CREST_SERVICE_ID"), os.Getenv("CREST_SERVICE_PRIVATE_KEY"))
}

// Middleware authenticates and authorizes internal service routes.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/internal/") {
			next.ServeHTTP(w, r)
			return
		}
		peer, ok := v.peers[r.Header.Get("X-CREST-Service-ID")]
		stamp, err := strconv.ParseInt(r.Header.Get("X-CREST-Service-Time"), 10, 64)
		now := clock.System{}.Now()
		if !ok || err != nil || now.Sub(time.Unix(stamp, 0)) > 30*time.Second || time.Unix(stamp, 0).Sub(now) > 30*time.Second {
			http.Error(w, "invalid service identity or timestamp", http.StatusUnauthorized)
			return
		}
		nonce := r.Header.Get("X-CREST-Service-Nonce")
		rawNonce, nonceErr := base64.RawURLEncoding.DecodeString(nonce)
		if nonceErr != nil || len(rawNonce) != 16 {
			http.Error(w, "invalid service nonce", http.StatusUnauthorized)
			return
		}
		allowed := false
		for _, rule := range peer.allow {
			method, path, ok := strings.Cut(rule, " ")
			if ok && (method == r.Method || method == "*") && ((strings.HasSuffix(path, "/") && strings.HasPrefix(r.URL.Path, path)) || r.URL.Path == path) {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, "service is not authorized for this operation", http.StatusForbidden)
			return
		}
		var body []byte
		if r.Body != nil && r.Body != http.NoBody {
			body, err = io.ReadAll(io.LimitReader(r.Body, 8<<20+1))
			closeErr := r.Body.Close()
			if err != nil || closeErr != nil || len(body) > 8<<20 {
				http.Error(w, "invalid service body", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		signature, err := base64.StdEncoding.DecodeString(r.Header.Get("X-CREST-Service-Signature"))
		validSignature := err == nil && ed25519.Verify(peer.key, canonical(r, body), signature)
		if !validSignature {
			for _, previous := range peer.previous {
				if !now.After(previous.notAfter) && ed25519.Verify(previous.key, canonical(r, body), signature) {
					validSignature = true
					break
				}
			}
		}
		if err != nil || len(nonce) < 16 || !validSignature {
			http.Error(w, "invalid service signature", http.StatusUnauthorized)
			return
		}
		serviceID := r.Header.Get("X-CREST-Service-ID")
		if v.replay != nil {
			claimed, err := v.replay.ClaimServiceNonce(r.Context(), serviceID, nonce, now.Add(time.Minute))
			if err != nil {
				http.Error(w, "service replay protection unavailable", http.StatusServiceUnavailable)
				return
			}
			if !claimed {
				http.Error(w, "service request already used", http.StatusConflict)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		replayKey := serviceID + ":" + nonce
		v.mu.Lock()
		for key, expires := range v.seen {
			if expires.Before(now) {
				delete(v.seen, key)
			}
		}
		_, replayed := v.seen[replayKey]
		if !replayed {
			v.seen[replayKey] = now.Add(time.Minute)
		}
		v.mu.Unlock()
		if replayed {
			http.Error(w, "service request already used", http.StatusConflict)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ValidateIdentity checks that a configured private key belongs to the service
// identity or to an unexpired rotation-grace key.
func ValidateIdentity(id, seedText, peersJSON string) error {
	v, err := NewVerifier(peersJSON)
	if err != nil {
		return err
	}
	seed, err := base64.StdEncoding.DecodeString(seedText)
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("invalid service private key")
	}
	peer, ok := v.peers[id]
	if !ok {
		return fmt.Errorf("service identity is absent from configured peers")
	}
	public := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if bytes.Equal(public, peer.key) {
		return nil
	}
	now := clock.System{}.Now()
	for _, previous := range peer.previous {
		if !now.After(previous.notAfter) && bytes.Equal(public, previous.key) {
			return nil
		}
	}
	return fmt.Errorf("service private key does not match a current or unexpired trusted public key")
}
