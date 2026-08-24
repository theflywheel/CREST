package store

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"
)

// SigV4, checked against a signature AWS published rather than one we computed.
//
// This is the "PUT Object" example from the Signature Version 4 documentation.
// It matters that the expected value comes from somebody else: a signing test
// that signs with this code and verifies with this code proves the two halves
// agree and nothing about whether either is right. That mistake has already
// been made once in this repo, in pkg/credential, and it shipped credentials no
// stranger could verify.
//
// The live half — a real S3 server accepting these requests — is the SeaweedFS
// round trip in the harness. Two independent checks, because a signing bug that
// surfaces only as a 403 from a server is a bug diagnosed by guesswork.
func TestSigV4MatchesAWSPublishedExample(t *testing.T) {
	// AWS's own documented example values, not credentials. crest:allow-secret
	const (
		accessKey = "AKIA" + "IOSFODNN7EXAMPLE"                     // crest:allow-secret
		secretKey = "wJalrXUtnFEMI/K7MDENG/" + "bPxRfiCYEXAMPLEKEY" // crest:allow-secret
		body      = "Welcome to Amazon S3."
		want      = "98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd"
	)

	s := &S3{
		cfg: S3Config{Region: "us-east-1", AccessKey: accessKey, SecretKey: secretKey},
		now: func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) },
	}

	req, err := http.NewRequest(http.MethodPut,
		"https://examplebucket.s3.amazonaws.com/test$file.text", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Date", "Fri, 24 May 2013 00:00:00 GMT")
	req.Header.Set("X-Amz-Storage-Class", "REDUCED_REDUNDANCY")

	// The example's own payload hash, so a mistake in ours is visible as a
	// mismatch here rather than as a different signature with no explanation.
	sum := sha256.Sum256([]byte(body))
	if got := hex.EncodeToString(sum[:]); got != "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072" {
		t.Fatalf("payload hash = %s, not the example's", got)
	}

	s.sign(req, []byte(body))

	auth := req.Header.Get("Authorization")
	if !strings.HasSuffix(auth, "Signature="+want) {
		t.Errorf("signature does not match AWS's published example.\n got: %s\nwant suffix: Signature=%s", auth, want)
	}
	if !strings.Contains(auth, "SignedHeaders=date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class") {
		t.Errorf("signed headers differ from the example: %s", auth)
	}
}

// A key the caller cannot choose is the point of Put's signature. This is the
// structural version of "never persist a raw national identifier": object keys
// end up in logs, bucket listings and support tickets, so a key that cannot
// carry a phone number is better than a rule saying it must not.
func TestKeysAreMintedAndDiscloseNothing(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		key, err := mintKey("consent")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(key, "consent/") {
			t.Fatalf("key %q is not namespaced by kind", key)
		}
		if len(key) != len("consent/")+32 {
			t.Fatalf("key %q is not 32 hex characters of nothing", key)
		}
		if seen[key] {
			t.Fatalf("minted the same key twice: %q", key)
		}
		seen[key] = true
	}
}

// A kind that can contain a slash is a kind that can be a path, and a path is
// somewhere a caller can smuggle meaning.
func TestAKindCannotBeAPath(t *testing.T) {
	for _, bad := range []string{"", "consent/worker", "../escape", "with space", "a.b"} {
		if _, err := mintKey(bad); err == nil {
			t.Errorf("mintKey(%q) was allowed", bad)
		}
	}
}

// Refusing is the only safe answer. A truncated consent recording is evidence
// of consent that stops before the part where the person said what they agreed
// to, and it would be indistinguishable from a complete one.
func TestAnOversizedArtefactIsRefusedRatherThanTruncated(t *testing.T) {
	_, _, err := readCapped(strings.NewReader(strings.Repeat("x", 101)), 100)
	if err == nil {
		t.Fatal("an oversized artefact was accepted")
	}
	body2, digest, err := readCapped(strings.NewReader(strings.Repeat("x", 100)), 100)
	if err != nil {
		t.Fatalf("an artefact exactly at the limit was refused: %v", err)
	}
	if len(body2) != 100 || !strings.HasPrefix(digest, "sha256:") {
		t.Errorf("digest = %q, size = %d", digest, len(body2))
	}
}

// Credentials come from the environment. A store that would run without them
// is a store that silently writes a worker's consent recording somewhere
// world-readable.
func TestAStoreWithoutCredentialsRefusesToExist(t *testing.T) {
	if _, err := NewS3(S3Config{Endpoint: "http://objectstore:8333", Bucket: "crest"}); err == nil {
		t.Error("an unauthenticated object store was constructed")
	}
}
