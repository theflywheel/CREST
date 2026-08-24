package store

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
)

// An S3-compatible Blobs, written against the protocol rather than against one
// vendor's SDK.
//
// Hand-rolled for the same reason pkg/dedi's request signing is: the surface
// CREST needs is four verbs, and a signing algorithm with published test
// vectors is a smaller thing to own than a dependency tree. It is pinned by
// those vectors in s3_test.go and by a live round trip against the SeaweedFS in
// compose — one proves we implemented SigV4, the other proves a real server
// agrees.
//
// Path-style addressing throughout. Virtual-host style requires DNS the
// deployment may not control, and every S3-compatible server that is not AWS
// supports path style.

const (
	// DefaultMaxBlobBytes sizes a voice consent recording, not a video.
	// Anything larger arriving under a consent key is a mistake somewhere
	// upstream, and silently accepting it becomes a storage bill nobody
	// predicted. Override with S3_MAX_BLOB_BYTES.
	DefaultMaxBlobBytes = 5 << 20

	s3Algorithm   = "AWS4-HMAC-SHA256"
	s3TimeFormat  = "20060102T150405Z"
	s3DateFormat  = "20060102"
	unsignedQuery = ""
)

// S3Config is what a deployment supplies. Every field comes from the
// environment: a key in the repository is a key that has been published.
type S3Config struct {
	Endpoint  string // http://objectstore:8333
	Bucket    string
	Region    string // "us-east-1" is what non-AWS servers usually expect
	AccessKey string
	SecretKey string
	MaxBytes  int64
}

// S3 implements Blobs.
type S3 struct {
	cfg  S3Config
	http *http.Client
	// now is wall time, deliberately not the domain clock. The signature's
	// timestamp is replay protection checked by the server against ITS clock,
	// so a run that drives CREST's clock five months forward must not sign with
	// it. This is the same mistake pkg/dedi made once and it cost an afternoon.
	now func() time.Time
}

// NewS3 builds a client. It does not reach the network; a store that cannot be
// constructed offline cannot be constructed in a test.
func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("object store needs an endpoint and a bucket")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("object store credentials are missing; refusing to run unauthenticated")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBlobBytes
	}
	cfg.Endpoint = strings.TrimRight(cfg.Endpoint, "/")
	return &S3{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
		now:  clock.System{}.Now,
	}, nil
}

// EnsureBucket creates the bucket if it is not there.
//
// Not on the Blobs interface: creating a container is an S3 concept, and a
// deployment pointed at a national cloud will more likely have had its bucket
// provisioned with a retention and lifecycle policy attached by someone whose
// job that is. This exists so a local stack and a test can start from nothing.
func (s *S3) EnsureBucket(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.cfg.Endpoint+"/"+s.cfg.Bucket, nil)
	if err != nil {
		return fmt.Errorf("build create-bucket request: %w", err)
	}
	req.URL.Path = "/" + s.cfg.Bucket
	req.URL.RawPath = canonicalURI(req.URL.Path)
	s.sign(req, nil)

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}
	defer drain(resp)
	// Already ours is success. S3 says 409 BucketAlreadyOwnedByYou; several
	// compatible servers say 200 and mean the same thing.
	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusConflict {
		return nil
	}
	return s3Error("create bucket", s.cfg.Bucket, resp)
}

// Put stores an artefact under a minted key. See Blobs.Put — the caller names
// a kind and never a key, which is what stops an object key carrying identity.
func (s *S3) Put(ctx context.Context, kind string, r io.Reader, contentType string) (Blob, error) {
	body, digest, err := readCapped(r, s.cfg.MaxBytes)
	if err != nil {
		return Blob{}, err
	}
	key, err := mintKey(kind)
	if err != nil {
		return Blob{}, err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	req, err := s.request(ctx, http.MethodPut, key, body)
	if err != nil {
		return Blob{}, err
	}
	req.Header.Set("Content-Type", contentType)
	s.sign(req, body)

	resp, err := s.http.Do(req)
	if err != nil {
		return Blob{}, fmt.Errorf("put artefact: %w", err)
	}
	defer drain(resp)
	if resp.StatusCode/100 != 2 {
		return Blob{}, s3Error("put", key, resp)
	}
	return Blob{Key: key, Digest: digest, Size: int64(len(body)), ContentType: contentType}, nil
}

// Get opens an artefact, or returns ErrNotFound. The caller closes the body.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	req, err := s.request(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	s.sign(req, nil)

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get artefact: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		drain(resp)
		return nil, ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		defer drain(resp)
		return nil, s3Error("get", key, resp)
	}
	return resp.Body, nil
}

// Delete removes an artefact permanently, and is idempotent: consent
// withdrawal asked for twice is not an error.
func (s *S3) Delete(ctx context.Context, key string) error {
	req, err := s.request(ctx, http.MethodDelete, key, nil)
	if err != nil {
		return err
	}
	s.sign(req, nil)

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("delete artefact: %w", err)
	}
	defer drain(resp)
	// A delete of something already gone is a success. Withdrawal must be
	// idempotent: a worker who asks twice has not made an error.
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode/100 != 2 {
		return s3Error("delete", key, resp)
	}
	return nil
}

// Exists answers with a HEAD, so an integrity sweep does not stream every
// recording it inspects.
func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
	req, err := s.request(ctx, http.MethodHead, key, nil)
	if err != nil {
		return false, err
	}
	s.sign(req, nil)

	resp, err := s.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("head artefact: %w", err)
	}
	defer drain(resp)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode/100 == 2:
		return true, nil
	default:
		return false, s3Error("head", key, resp)
	}
}

func (s *S3) request(ctx context.Context, method, key string, body []byte) (*http.Request, error) {
	path := "/" + s.cfg.Bucket + "/" + key

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.Endpoint+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", method, err)
	}
	// The path is written in the encoding SigV4 signs, so what goes on the wire
	// and what gets signed cannot drift apart. Go's own escaping is not the
	// same: it leaves sub-delimiters like $ alone, and AWS requires every
	// character outside RFC 3986's unreserved set to be percent-encoded.
	req.URL.Path = path
	req.URL.RawPath = canonicalURI(path)
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	return req, nil
}

// sign applies SigV4. Split out and exercised directly by the vector test,
// because a signing bug that only shows up as 403 from a server is a bug
// diagnosed by guesswork.
func (s *S3) sign(req *http.Request, body []byte) {
	now := s.now().UTC()
	stamp := now.Format(s3TimeFormat)
	date := now.Format(s3DateFormat)

	sum := sha256.Sum256(body) // sha256 of empty is correct for a bodyless request
	payloadHash := hex.EncodeToString(sum[:])

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", stamp)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	signed, canonicalHeaders := canonicalHeaders(req)
	canonical := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		unsignedQuery,
		canonicalHeaders,
		signed,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{date, s.cfg.Region, "s3", "aws4_request"}, "/")
	canonicalSum := sha256.Sum256([]byte(canonical))
	toSign := strings.Join([]string{
		s3Algorithm, stamp, scope, hex.EncodeToString(canonicalSum[:]),
	}, "\n")

	key := hmacBytes(hmacBytes(hmacBytes(hmacBytes(
		[]byte("AWS4"+s.cfg.SecretKey), []byte(date)),
		[]byte(s.cfg.Region)), []byte("s3")), []byte("aws4_request"))
	signature := hex.EncodeToString(hmacBytes(key, []byte(toSign)))

	req.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s3Algorithm, s.cfg.AccessKey, scope, signed, signature))
}

// canonicalHeaders returns the signed-header list and the canonical block.
// Host and every x-amz-* header, lowercased and sorted — signing the content
// hash is what stops a proxy altering a body in flight.
func canonicalHeaders(req *http.Request) (string, string) {
	names := []string{"host"}
	values := map[string]string{"host": req.URL.Host}
	for name, vs := range req.Header {
		lower := strings.ToLower(name)
		// host, content-type, date and every x-amz-*. This set is what AWS's
		// own published examples sign, which is what lets s3_test.go check this
		// implementation against a signature we did not compute.
		if !strings.HasPrefix(lower, "x-amz-") && lower != "content-type" && lower != "date" {
			continue
		}
		names = append(names, lower)
		values[lower] = strings.TrimSpace(strings.Join(vs, ","))
	}
	sort.Strings(names)

	var block strings.Builder
	for _, n := range names {
		block.WriteString(n)
		block.WriteByte(':')
		block.WriteString(values[n])
		block.WriteByte('\n')
	}
	return strings.Join(names, ";"), block.String()
}

// canonicalURI percent-encodes a path the way SigV4 requires: every character
// outside RFC 3986's unreserved set, with "/" kept as a separator. S3 encodes
// once, unlike every other AWS service.
//
// Go's url.PathEscape is not a substitute. It leaves sub-delimiters — $ ! ' ( )
// * , ; = & + — untouched, so a key containing one signs a path AWS canonicalises
// differently, and the only symptom is a 403 that names nothing.
func canonicalURI(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for i := 0; i < len(path); i++ {
		c := path[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~', c == '/':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func hmacBytes(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
	_ = resp.Body.Close()
}

// s3Error keeps the server's own words. An object store's error body says which
// of several indistinguishable causes it was — wrong bucket, wrong signature,
// clock skew — and discarding it turns a five-minute fix into an afternoon.
func s3Error(op, key string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
	return fmt.Errorf("%s %s: %s: %s", op, key, resp.Status, strings.TrimSpace(string(body)))
}
