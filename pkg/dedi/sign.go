package dedi

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Publisher authentication, ported from DeDi-node's internal/publisher.
//
// The P0 spike said CREST "deliberately does not re-implement DeDi's publisher
// signature", and for a bash script shelling out to `dedid sign` that was
// right. It stops being right here: a Go service cannot shell out to a binary
// that is not in its image, and shipping DeDi's CLI into every CREST service
// image to sign a request is a larger liability than forty lines of Ed25519.
// Vendoring the package is foreclosed separately — DeDi-node needs Go 1.25 and
// CREST is on 1.24 (docs/spikes/dedi.md, finding 5).
//
// So this is a second implementation of somebody else's wire contract, which
// means it can drift. The mitigation is TestPreimageWireContract, which pins
// the exact preimage bytes against a vector taken from DeDi's own test suite:
// if either side changes the scheme, the field order or the separator, that
// test fails rather than production failing with "signature does not verify".
const scheme = "dedi/v1/publish"

const (
	headerKeyID     = "DeDi-Key-Id"
	headerTimestamp = "DeDi-Timestamp"
	headerSignature = "DeDi-Signature"
)

// Key is a CREST publisher credential.
//
// The private key comes from the environment and never from the repository. A
// signing key in a file next to the code is a signing key on every laptop that
// ever cloned it, and this one authorises writes to the log a verifier trusts.
type Key struct {
	ID      string
	Private ed25519.PrivateKey
}

// ParseKey reads the base64 form `dedid pubkeygen` writes to its key file.
func ParseKey(kid, b64 string) (Key, error) {
	if kid == "" {
		return Key{}, fmt.Errorf("dedi: key id is required")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		// Deliberately does not echo the value: an unparseable secret is still
		// a secret, and half of one in a log is enough to identify it.
		return Key{}, fmt.Errorf("dedi: publisher key is not standard base64: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return Key{}, fmt.Errorf("dedi: publisher key is %d bytes, want %d", len(raw), ed25519.PrivateKeySize)
	}
	return Key{ID: kid, Private: ed25519.PrivateKey(raw)}, nil
}

// Preimage is the exact byte string a publisher signs.
//
// Field order and separator are the wire contract; changing either invalidates
// every signature. The precondition is in here rather than only in the headers
// for a reason worth restating: with an unsigned precondition, a captured
// request could have its If-Match stripped and be replayed unconditionally,
// reverting a record to an earlier payload. Signed, a replay can only land on
// the exact version it was replacing — so it is a no-op or a conflict.
func Preimage(method, requestURI string, body []byte, pre Precondition, ts time.Time) []byte {
	sum := sha256.Sum256(body)
	ifNoneMatch := ""
	if pre.Create {
		ifNoneMatch = "*"
	}
	return []byte(strings.Join([]string{
		scheme,
		strings.ToUpper(method),
		requestURI,
		base64.StdEncoding.EncodeToString(sum[:]),
		pre.IfMatch,
		ifNoneMatch,
		ts.UTC().Format(time.RFC3339),
	}, "\n"))
}

// sign sets the authentication and precondition headers on a request.
//
// The precondition is applied here, in the same place as the signature, so the
// headers that are sent and the bytes that are signed cannot disagree — which
// is the only failure mode of this scheme that produces a confusing error
// rather than an obvious one.
func (k Key) sign(req *http.Request, requestURI string, body []byte, pre Precondition, now time.Time) {
	req.Header.Set(headerKeyID, k.ID)
	req.Header.Set(headerTimestamp, now.UTC().Format(time.RFC3339))
	req.Header.Set(headerSignature,
		base64.StdEncoding.EncodeToString(ed25519.Sign(k.Private, Preimage(req.Method, requestURI, body, pre, now))))
	if pre.Create {
		req.Header.Set("If-None-Match", "*")
	}
	if pre.IfMatch != "" {
		req.Header.Set("If-Match", pre.IfMatch)
	}
}
