//go:build e2e

package scenarios

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/theflywheel/crest/pkg/store"
)

// The object store, against a real S3 server rather than a fake (#47).
//
// pkg/store's SigV4 is checked against AWS's published example in a unit test.
// This is the other half: a server we did not write, applying its own idea of
// the protocol, accepting or rejecting what we send. Either check alone is
// weak — one proves we implemented the algorithm on paper, the other proves
// something real agrees — and the mistake that made both necessary is C19,
// where a signature valid against its own implementation verified nowhere else.
//
// What lives here is consent artefacts, starting with the voice recording that
// is a non-literate worker's only real way to consent (§9). That is why the
// deletion test is not an afterthought: consent withdrawal that leaves the
// recording in place has not withdrawn anything.

func blobs(t *testing.T) *store.S3 {
	t.Helper()
	cfg, ok := store.LoadS3Config()
	if !ok {
		// Default to the compose stack's published port, so this runs the same
		// way the rest of the harness does.
		cfg = store.S3Config{
			Endpoint:  "http://localhost:" + envOr("S3_PORT", "58333"),
			Bucket:    envOr("S3_BUCKET", "crest-artefacts"),
			AccessKey: envOr("S3_ACCESS_KEY", "crest-local"),
			SecretKey: envOr("S3_SECRET_KEY", "crest-local-secret"),
		}
	}
	s, err := store.NewS3(cfg)
	if err != nil {
		t.Fatalf("build object store client: %v", err)
	}
	if err := s.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("ensure bucket: %v\n\nis the stack up? `make e2e-up`", err)
	}
	return s
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// A recording goes in, comes back byte-identical, and can be withdrawn.
func TestAConsentArtefactRoundTripsAndCanBeWithdrawn(t *testing.T) {
	ctx := context.Background()
	s := blobs(t)

	// Stands in for the recording. What matters to the test is that the bytes
	// are arbitrary and come back unchanged — an artefact that is transformed
	// in flight is an artefact whose digest no longer proves anything.
	recording := bytes.Repeat([]byte{0x00, 0x01, 0xff, 0x7f, 'a'}, 400)

	blob, err := s.Put(ctx, "consent", bytes.NewReader(recording), "audio/ogg")
	if err != nil {
		t.Fatalf("store a consent artefact: %v", err)
	}
	if !strings.HasPrefix(blob.Key, "consent/") || blob.Size != int64(len(recording)) {
		t.Fatalf("unexpected blob: %+v", blob)
	}

	ok, err := s.Exists(ctx, blob.Key)
	if err != nil || !ok {
		t.Fatalf("exists = %v, %v — a HEAD must answer without transferring the body", ok, err)
	}

	body, err := s.Get(ctx, blob.Key)
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	got, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, recording) {
		t.Fatalf("the artefact came back changed: %d bytes out, %d back", len(recording), len(got))
	}

	// Withdrawal. Real deletion, and idempotent — a worker who asks twice has
	// not made an error.
	if err := s.Delete(ctx, blob.Key); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if err := s.Delete(ctx, blob.Key); err != nil {
		t.Errorf("withdrawing twice failed: %v", err)
	}
	if ok, err := s.Exists(ctx, blob.Key); err != nil || ok {
		t.Errorf("the artefact is still there after withdrawal (exists=%v, err=%v)", ok, err)
	}
	if _, err := s.Get(ctx, blob.Key); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("reading a withdrawn artefact returned %v, want ErrNotFound", err)
	}
}

// The server must actually be checking the signature. If it is not, every
// signing test above is decorative: a broken implementation would pass by being
// ignored, and a deployment copying our compose file would hold workers' consent
// recordings in a store anyone who can reach it can read.
func TestTheObjectStoreRefusesABadSignature(t *testing.T) {
	ctx := context.Background()
	_ = blobs(t) // ensures the bucket exists

	bad, err := store.NewS3(store.S3Config{
		Endpoint:  "http://localhost:" + envOr("S3_PORT", "58333"),
		Bucket:    envOr("S3_BUCKET", "crest-artefacts"),
		AccessKey: envOr("S3_ACCESS_KEY", "crest-local"),
		SecretKey: "not-the-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Put(ctx, "consent", strings.NewReader("should not be stored"), "audio/ogg"); err == nil {
		t.Fatal("the object store accepted a request signed with the wrong key; " +
			"it is not authenticating, and nothing else here proves anything")
	}
}

// Two artefacts, two keys, neither disclosing anything about who they belong
// to. The unit test proves the minting; this proves the store does not rewrite
// or collapse them.
func TestTwoArtefactsDoNotShareAKey(t *testing.T) {
	ctx := context.Background()
	s := blobs(t)

	first, err := s.Put(ctx, "consent", strings.NewReader("one"), "audio/ogg")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Put(ctx, "consent", strings.NewReader("two"), "audio/ogg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Delete(ctx, first.Key); _ = s.Delete(ctx, second.Key) }()

	if first.Key == second.Key {
		t.Fatal("two artefacts were stored under one key; one worker's consent overwrote another's")
	}
	if first.Digest == second.Digest {
		t.Error("different content produced the same digest")
	}
}
