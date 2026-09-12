package dedi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/sumdb/tlog"
)

// fakeLog is an in-memory transparency log that behaves like a DeDi node's log:
// leaves are appended, a signed checkpoint is served, and a consistency proof is
// computed between any two sizes. It can also rewrite history — recompute the
// root over altered leaves at the same size — which is the tamper the client is
// supposed to catch.
type fakeLog struct {
	t       *testing.T
	origin  string
	signer  note.Signer
	stored  []tlog.Hash // tlog stored-hash tree
	records [][]byte    // leaf data, so history can be rewritten
}

func newFakeLog(t *testing.T, origin string) (*fakeLog, note.Verifier) {
	t.Helper()
	skey, vkey, err := note.GenerateKey(rand.Reader, origin)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := note.NewSigner(skey)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	verifier, err := note.NewVerifier(vkey)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	return &fakeLog{t: t, origin: origin, signer: signer}, verifier
}

func (f *fakeLog) reader() tlog.HashReaderFunc {
	return func(indexes []int64) ([]tlog.Hash, error) {
		out := make([]tlog.Hash, len(indexes))
		for i, x := range indexes {
			if x < 0 || x >= int64(len(f.stored)) {
				return nil, fmt.Errorf("stored hash %d out of range (have %d)", x, len(f.stored))
			}
			out[i] = f.stored[x]
		}
		return out, nil
	}
}

func (f *fakeLog) append(data string) {
	f.t.Helper()
	n := int64(len(f.records))
	hashes, err := tlog.StoredHashesForRecordHash(n, tlog.RecordHash([]byte(data)), f.reader())
	if err != nil {
		f.t.Fatalf("append %q: %v", data, err)
	}
	f.stored = append(f.stored, hashes...)
	f.records = append(f.records, []byte(data))
}

// rewrite alters the leaf at index and rebuilds the stored tree at the same
// size — a history rewrite in place, producing a different root for an
// unchanged tree size.
func (f *fakeLog) rewrite(index int, data string) {
	f.t.Helper()
	f.records[index] = []byte(data)
	f.stored = f.stored[:0]
	recs := f.records
	f.records = nil
	for _, r := range recs {
		f.append(string(r))
	}
}

func (f *fakeLog) size() int64 { return int64(len(f.records)) }

func (f *fakeLog) root() tlog.Hash {
	f.t.Helper()
	h, err := tlog.TreeHash(f.size(), f.reader())
	if err != nil {
		f.t.Fatalf("tree hash: %v", err)
	}
	return h
}

func (f *fakeLog) checkpoint() []byte {
	f.t.Helper()
	root := f.root()
	body := fmt.Sprintf("%s\n%d\n%s\n", f.origin, f.size(),
		base64.StdEncoding.EncodeToString(root[:]))
	msg, err := note.Sign(&note.Note{Text: body}, f.signer)
	if err != nil {
		f.t.Fatalf("sign checkpoint: %v", err)
	}
	return msg
}

func (f *fakeLog) serve() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/dedi/log/checkpoint", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(f.checkpoint())
	})
	mux.HandleFunc("/dedi/log/proof/consistency", func(w http.ResponseWriter, r *http.Request) {
		old, _ := strconv.ParseInt(r.URL.Query().Get("old"), 10, 64)
		newer, _ := strconv.ParseInt(r.URL.Query().Get("new"), 10, 64)
		proof, err := tlog.ProveTree(newer, old, f.reader())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		path := make([]string, len(proof))
		for i, h := range proof {
			path[i] = base64.StdEncoding.EncodeToString(h[:])
		}
		writeData(w, map[string]any{"old_size": old, "new_size": newer, "proof": path})
	})
	return httptest.NewServer(mux)
}

func writeData(w http.ResponseWriter, data map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"message": "ok", "data": data})
}

// witnessData is a stand-in witness node's verdict about one target, for the
// monitor tests.
type witnessData struct {
	witnessed     bool
	consistencyOK bool
	size          int64
	root          string
	detail        string
}

// serveWitness stands in for an independent witness node's read plane
// (GET /dedi/witness/{target}), keyed by target origin.
func serveWitness(t *testing.T, verdicts map[string]witnessData) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/dedi/witness/", func(w http.ResponseWriter, r *http.Request) {
		origin, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/dedi/witness/"))
		d, ok := verdicts[origin]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeData(w, map[string]any{
			"origin": origin, "witnessed": d.witnessed, "consistency_ok": d.consistencyOK,
			"size": d.size, "root": d.root, "detail": d.detail,
		})
	})
	return httptest.NewServer(mux)
}

func nodeFor(t *testing.T, url string, v note.Verifier) *Node {
	t.Helper()
	key, err := ParseKey("test", base64.StdEncoding.EncodeToString(make([]byte, 64)))
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	n, err := NewNode(url, key)
	if err != nil {
		t.Fatalf("new node: %v", err)
	}
	if v != nil {
		n.SetCheckpointVerifier(v)
	}
	return n
}

// A checkpoint an authorised node signed verifies, and its size and root parse
// out of the signed bytes.
func TestCheckpointVerifiesAndParses(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	log.append("b")
	srv := log.serve()
	defer srv.Close()

	n := nodeFor(t, srv.URL, verifier)
	cp, err := n.Checkpoint(context.Background())
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if !cp.Verified {
		t.Fatal("checkpoint should be Verified with the node key configured")
	}
	if cp.Size != 2 {
		t.Fatalf("size = %d, want 2", cp.Size)
	}
	if cp.Root != log.root() {
		t.Fatal("parsed root does not match the log's root")
	}
}

// Without the node's verifier key a checkpoint is readable but not Verified —
// the weaker statement, never dressed up as the stronger one.
func TestCheckpointUnauthenticatedWithoutKey(t *testing.T) {
	log, _ := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	srv := log.serve()
	defer srv.Close()

	n := nodeFor(t, srv.URL, nil)
	cp, err := n.Checkpoint(context.Background())
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if cp.Verified {
		t.Fatal("checkpoint must not be Verified with no key configured")
	}
	if cp.Size != 1 {
		t.Fatalf("size = %d, want 1", cp.Size)
	}
}

// A checkpoint signed by the wrong key is rejected outright.
func TestCheckpointWrongKeyRejected(t *testing.T) {
	log, _ := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	srv := log.serve()
	defer srv.Close()

	_, otherVerifier := newFakeLog(t, "crest.test/dedi")
	n := nodeFor(t, srv.URL, otherVerifier)
	_, err := n.Checkpoint(context.Background())
	if err == nil || !strings.Contains(err.Error(), ErrCheckpointUnverified.Error()) {
		t.Fatalf("want ErrCheckpointUnverified, got %v", err)
	}
}

// The honest case: the log grew, and the growth is a forward-only extension the
// node can prove. ConfirmExtends accepts it and returns the new checkpoint.
func TestConfirmExtendsAcceptsAppend(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	log.append("b")
	srv := log.serve()
	defer srv.Close()

	n := nodeFor(t, srv.URL, verifier)
	pinned, err := n.Checkpoint(context.Background())
	if err != nil {
		t.Fatalf("pin: %v", err)
	}

	log.append("c")
	log.append("d")
	cur, err := n.ConfirmExtends(context.Background(), pinned)
	if err != nil {
		t.Fatalf("ConfirmExtends should accept an append: %v", err)
	}
	if cur.Size != 4 {
		t.Fatalf("current size = %d, want 4", cur.Size)
	}
}

// The finding this feature exists for: the operator rewrites a leaf in place.
// Size is unchanged, root differs, and there is no consistency proof from the
// pinned checkpoint to the rewritten one. CREST refuses.
func TestConfirmExtendsCatchesRewriteInPlace(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	log.append("b")
	log.append("c")
	srv := log.serve()
	defer srv.Close()

	n := nodeFor(t, srv.URL, verifier)
	pinned, err := n.Checkpoint(context.Background())
	if err != nil {
		t.Fatalf("pin: %v", err)
	}

	log.rewrite(1, "b-tampered") // same size, different history
	_, err = n.ConfirmExtends(context.Background(), pinned)
	if err == nil || !strings.Contains(err.Error(), ErrHistoryRewritten.Error()) {
		t.Fatalf("want ErrHistoryRewritten for a same-size rewrite, got %v", err)
	}
}

// A rewrite that also grows the log cannot escape detection: the consistency
// proof the node produces is between the tampered histories, so CheckTree
// against the pinned root fails.
func TestConfirmExtendsCatchesRewriteThenGrow(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	log.append("b")
	srv := log.serve()
	defer srv.Close()

	n := nodeFor(t, srv.URL, verifier)
	pinned, err := n.Checkpoint(context.Background())
	if err != nil {
		t.Fatalf("pin: %v", err)
	}

	log.rewrite(0, "a-tampered")
	log.append("c") // now size 3, but history 0..1 was rewritten
	_, err = n.ConfirmExtends(context.Background(), pinned)
	if err == nil || !strings.Contains(err.Error(), ErrHistoryRewritten.Error()) {
		t.Fatalf("want ErrHistoryRewritten for a rewrite-then-grow, got %v", err)
	}
}

// A log that shrank cannot be append-only, whatever its root.
func TestConfirmExtendsCatchesShrink(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	log.append("b")
	log.append("c")
	srv := log.serve()
	defer srv.Close()

	n := nodeFor(t, srv.URL, verifier)
	pinned, err := n.Checkpoint(context.Background())
	if err != nil {
		t.Fatalf("pin: %v", err)
	}

	log.records = log.records[:1] // pretend the log shrank to size 1
	log.rewrite(0, "a")           // rebuild stored tree at the smaller size
	_, err = n.ConfirmExtends(context.Background(), pinned)
	if err == nil || !strings.Contains(err.Error(), ErrHistoryRewritten.Error()) {
		t.Fatalf("want ErrHistoryRewritten for a shrunk log, got %v", err)
	}
}

// The first observation (pinned Size 0) has nothing earlier to contradict and
// always passes, returning the current checkpoint to pin.
func TestConfirmExtendsFirstObservationPasses(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	srv := log.serve()
	defer srv.Close()

	n := nodeFor(t, srv.URL, verifier)
	cur, err := n.ConfirmExtends(context.Background(), Checkpoint{})
	if err != nil {
		t.Fatalf("first observation should pass: %v", err)
	}
	if cur.Size != 1 {
		t.Fatalf("current size = %d, want 1", cur.Size)
	}
}
