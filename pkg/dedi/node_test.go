package dedi

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeNode is a DeDi node good enough to exercise the client's decisions: it
// verifies the signature the way a real node does, enforces the precondition,
// and can be told to misbehave in the specific way the P0 spike found real
// nodes misbehaving.
type fakeNode struct {
	t         *testing.T
	pub       ed25519.PublicKey
	versions  map[string]string // version_id -> digest
	latest    string
	ignorePin bool // finding 3: answer with the latest whatever version was pinned
	withProof bool
	writes    []string
	stamps    []time.Time
	conflict  bool
}

func (f *fakeNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		f.lookup(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	pre := Precondition{IfMatch: r.Header.Get("If-Match"), Create: r.Header.Get("If-None-Match") == "*"}
	ts, err := time.Parse(time.RFC3339, r.Header.Get(headerTimestamp))
	if err != nil {
		http.Error(w, "bad timestamp", http.StatusBadRequest)
		return
	}
	sig, err := base64.StdEncoding.DecodeString(r.Header.Get(headerSignature))
	if err != nil || !ed25519.Verify(f.pub, Preimage(r.Method, r.URL.Path, body, pre, ts), sig) {
		http.Error(w, "signature does not verify", http.StatusUnauthorized)
		return
	}
	f.writes = append(f.writes, r.Method+" "+r.URL.Path)
	f.stamps = append(f.stamps, ts)
	if f.conflict {
		http.Error(w, "already exists", http.StatusPreconditionFailed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message":"ok"}`))
}

func (f *fakeNode) lookup(w http.ResponseWriter, r *http.Request) {
	want := r.URL.Query().Get("version_id")
	if want == "" || f.ignorePin {
		want = f.latest
	}
	digest, ok := f.versions[want]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	out := map[string]any{"data": map[string]any{
		"digest": digest, "state": "live", "version": want,
		"details": map[string]any{"definitionId": "WD-4471"},
	}}
	if f.withProof && r.URL.Query().Get("proof") == "inclusion" {
		out["proof"] = map[string]any{"leaf": map[string]any{"version_num": 2}}
	}
	_ = json.NewEncoder(w).Encode(out)
}

func newFake(t *testing.T) (*fakeNode, *Node) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeNode{
		t: t, pub: pub, latest: "3",
		versions: map[string]string{"2": "aaaa", "3": "bbbb"},
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	n, err := NewNode(srv.URL, Key{ID: "crest", Private: priv})
	if err != nil {
		t.Fatal(err)
	}
	return f, n
}

// The finding with teeth. A node that ignores an unrecognised pin answers with
// the latest version and a valid proof, and a verifier resolving a credential's
// definition would follow it without noticing. CREST refuses instead.
func TestResolveRefusesWhenThePinIsIgnored(t *testing.T) {
	f, n := newFake(t)
	f.ignorePin = true

	_, err := n.Resolve(context.Background(),
		Ref{Namespace: "crest", Registry: "work-definitions", Record: "WD-4471", Version: "2"}, false)
	if !errors.Is(err, ErrPinMissed) {
		t.Fatalf("a missed pin was accepted: err = %v", err)
	}
}

func TestResolvePinnedVersion(t *testing.T) {
	_, n := newFake(t)
	rec, err := n.Resolve(context.Background(),
		Ref{Namespace: "crest", Registry: "work-definitions", Record: "WD-4471", Version: "2"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Receipt.Ref.Version != "2" || rec.Receipt.Digest != "aaaa" {
		t.Fatalf("resolved %+v, want version 2 with digest aaaa", rec.Receipt)
	}
	if rec.Receipt.Tag() != "aaaa-live" {
		t.Errorf("Tag() = %q, want the <digest>-<state> form If-Match needs", rec.Receipt.Tag())
	}
	if !rec.Receipt.Transparent {
		t.Error("a node receipt claims no transparency log")
	}
}

// Asking for a proof and getting a document without one must be an error. A
// caller that wanted something checkable and received a silent nil would hand a
// verifier nothing and believe it had.
func TestResolveRefusesAMissingProof(t *testing.T) {
	f, n := newFake(t)
	f.withProof = false
	_, err := n.Resolve(context.Background(),
		Ref{Namespace: "crest", Registry: "work-definitions", Record: "WD-4471"}, true)
	if !errors.Is(err, ErrNoProof) {
		t.Fatalf("err = %v, want ErrNoProof", err)
	}
}

func TestResolveKeepsTheBytesTheNodeSigned(t *testing.T) {
	f, n := newFake(t)
	f.withProof = true
	rec, err := n.Resolve(context.Background(),
		Ref{Namespace: "crest", Registry: "work-definitions", Record: "WD-4471"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Receipt.Proof) == 0 {
		t.Fatal("no proof bytes were kept; a re-serialised proof is not the one the node signed")
	}
	if rec.Receipt.LeafVersionNum != 2 {
		t.Errorf("LeafVersionNum = %d, want the leaf's own count", rec.Receipt.LeafVersionNum)
	}
}

// Bootstrap runs at every start. A registry that already exists is success.
func TestEnsureRegistryIsIdempotent(t *testing.T) {
	f, n := newFake(t)
	f.conflict = true
	if err := n.EnsureRegistry(context.Background(), "crest", "work-definitions", "public faces"); err != nil {
		t.Fatalf("an existing registry was reported as a failure: %v", err)
	}
	if len(f.writes) != 2 {
		t.Errorf("wrote %v, want a namespace and a registry", f.writes)
	}
}

// A DeDi node rejects a signed request whose timestamp is outside its window,
// and CREST services run under a driveable clock set to the fixture epoch so a
// seven-day window is arithmetic rather than a wait. Signing with that clock
// made every write to the deployed node fail with "request timestamp outside
// the accepted window" — five months of skew from a clock doing its job.
//
// The signing timestamp is wall time, not domain time, and this is the test
// that keeps it that way.
func TestSigningTimestampIsWallTimeNotTheDomainClock(t *testing.T) {
	f, n := newFake(t)
	if err := n.EnsureRegistry(context.Background(), "crest", "work-definitions", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.stamps) == 0 {
		t.Fatal("no signed request reached the node")
	}
	for _, ts := range f.stamps {
		if d := time.Since(ts); d > time.Minute || d < -time.Minute {
			t.Fatalf("signed timestamp is %s away from now; a node would reject it as replay", d)
		}
	}
}

func TestRefRejectsPathInjection(t *testing.T) {
	_, n := newFake(t)
	// The signature covers the path. A record name with a slash in it signs one
	// route and reaches another, which is a signed write landing somewhere the
	// publisher did not authorise.
	_, err := n.Publish(context.Background(),
		Ref{Namespace: "crest", Registry: "work-definitions", Record: "../../admin"},
		map[string]any{"x": 1}, Create())
	if err == nil {
		t.Fatal("a record name containing a path separator was accepted")
	}
}

func TestPreconditionMustBeExactlyOne(t *testing.T) {
	_, n := newFake(t)
	ref := Ref{Namespace: "crest", Registry: "r", Record: "x"}
	for name, pre := range map[string]Precondition{
		"neither": {},
		"both":    {Create: true, IfMatch: "a-live"},
	} {
		if _, err := n.Publish(context.Background(), ref, map[string]any{}, pre); err == nil {
			t.Errorf("%s precondition was accepted", name)
		}
	}
}
