package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Blob storage, for the artefacts that must not live in a database.
//
// The first of them is a voice consent recording. §9 says a voice recording is
// a valid consent capture for a non-literate worker, and that is not a
// convenience: for a worker who cannot read the form, it is the only form of
// consent that is actually theirs. It has to be storable, retrievable and
// deletable, or the consent moment cannot be evidenced at all.
//
// Interface first, because #47 says a deployment may point this at SeaweedFS,
// Garage, AWS S3 or a national cloud, and which one is a deployment's decision
// rather than CREST's. Two deployments can disagree about the object store and
// both still be CREST.

// ErrBlobTooLarge is returned rather than truncating. A half-stored consent
// recording is worse than none: it is evidence of consent that stops before the
// part where the person said what they were agreeing to.
var ErrBlobTooLarge = errors.New("artefact exceeds the configured maximum size")

// Blob is what a stored artefact is afterwards.
type Blob struct {
	// Key is minted by the store, never supplied by the caller. See Put.
	Key string `json:"key"`
	// Digest is "sha256:<hex>". The Consent record holds it alongside the key,
	// so an artefact that is later swapped is detectable rather than trusted.
	Digest      string `json:"digest"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
}

// Blobs is the whole surface. Deliberately four methods: an object store with
// a rich API invites someone to encode meaning in it.
type Blobs interface {
	// Put stores an artefact under a key the STORE mints, and returns it.
	//
	// The caller names a kind — "consent", "enrolment" — and never a key. That
	// is the important part of this signature. A caller that chose its own keys
	// would eventually write consent/+15550100011.ogg or consent/<national
	// id>.wav, and object keys leak: they appear in logs, in bucket listings,
	// in a support ticket screenshot, and in whatever the storage provider
	// keeps. Making the key unguessable and meaningless is a structural
	// guarantee rather than a rule somebody has to remember.
	Put(ctx context.Context, kind string, body io.Reader, contentType string) (Blob, error)

	// Get opens an artefact. The caller closes it.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes one, permanently.
	//
	// Consent withdrawal (§9) is the reason this exists and the reason it is
	// real deletion. A withdrawal that leaves the recording in place has not
	// withdrawn anything. The Consent *record* stays — that consent was given
	// and later withdrawn is history, and history is not rewritten — but the
	// artefact itself goes.
	Delete(ctx context.Context, key string) error

	// Exists answers without transferring the body, so a health check or an
	// integrity sweep does not stream every recording it inspects.
	Exists(ctx context.Context, key string) (bool, error)
}

// mintKey produces an opaque key: <kind>/<32 hex>. No date, no party, no
// sequence — a key must not disclose who an artefact is about, when they were
// enrolled, or how many others there are.
func mintKey(kind string) (string, error) {
	if kind == "" || strings.ContainsAny(kind, "/.\\ ") {
		return "", fmt.Errorf("blob kind %q must be a single path-safe word", kind)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("mint blob key: %w", err)
	}
	return kind + "/" + hex.EncodeToString(raw[:]), nil
}

// readCapped reads at most max bytes and refuses anything longer, without
// trusting a Content-Length nobody verified.
func readCapped(r io.Reader, max int64) ([]byte, string, error) {
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, "", fmt.Errorf("read artefact: %w", err)
	}
	if int64(len(body)) > max {
		return nil, "", fmt.Errorf("%w: over %d bytes", ErrBlobTooLarge, max)
	}
	sum := sha256.Sum256(body)
	return body, "sha256:" + hex.EncodeToString(sum[:]), nil
}

// LoadS3Config reads the object store's configuration from the environment.
//
// It lives here rather than in pkg/config so that the rule about credentials is
// stated once, next to the thing that needs them: they come from the
// environment, always, in local development as much as in a pilot. A key in the
// repository is a key that has been published, and a store holding consent
// recordings is not a place to learn that lesson.
//
// An unset S3_ENDPOINT returns false rather than an error. Most services never
// touch an artefact, and a deployment that has not configured an object store
// should be able to run everything that does not need one.
func LoadS3Config() (S3Config, bool) {
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		return S3Config{}, false
	}
	cfg := S3Config{
		Endpoint:  endpoint,
		Bucket:    os.Getenv("S3_BUCKET"),
		Region:    os.Getenv("S3_REGION"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
	}
	if raw := os.Getenv("S3_MAX_BLOB_BYTES"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			cfg.MaxBytes = n
		}
	}
	return cfg, true
}

// PreparedBlobs supports an opaque key journaled before external upload.
type PreparedBlobs interface {
	PutPrepared(context.Context, string, io.Reader, string) (Blob, error)
}

// PrepareBlobKey returns a validated opaque key for an externally prepared blob.
func PrepareBlobKey(kind string) (string, error) { return mintKey(kind) }
