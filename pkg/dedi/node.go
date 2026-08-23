package dedi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
)

// Node is a Publisher backed by a real DeDi node.
//
// It deliberately holds no CREST clock. DeDi bounds replay by comparing the
// signed timestamp against the node's own wall clock, so that timestamp is not
// domain time — it is the real time of day, and the only correct source for it
// is the system clock.
//
// This is not hypothetical tidiness. The harness runs services with a driveable
// clock set to the fixture epoch so a seven-day confirmation window is
// arithmetic rather than a wait (pkg/service.chooseClock). Signing with that
// clock produced every write failing on the deployed node with "request
// timestamp outside the accepted window" — five months of skew, from a clock
// that is doing exactly what it was built to do.
type Node struct {
	baseURL string
	key     Key
	http    *http.Client
	// now is the wall clock, injected only so the signing timestamp is
	// testable. It is never the service's domain clock.
	now func() time.Time
}

// NewNode builds a client. baseURL is the node's root, e.g.
// https://crest-dedi-production.up.railway.app.
//
// It takes no clock, unlike Fallback: nothing a Node does is domain time. See
// the type comment.
func NewNode(baseURL string, key Key) (*Node, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("dedi: node URL is required")
	}
	if key.Private == nil {
		return nil, fmt.Errorf("dedi: a node client needs a publisher key")
	}
	return &Node{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		// A registry write sits inside a request the caller is waiting on, and
		// a node that has stopped answering must fail rather than hold the
		// connection until something upstream gives up first.
		http: &http.Client{Timeout: 15 * time.Second},
		now:  clock.System{}.Now,
	}, nil
}

// Transparent reports true: this is the implementation with a log behind it.
func (n *Node) Transparent() bool { return true }

// wire is the envelope every DeDi response uses.
type wire struct {
	Message string `json:"message"`
	Error   string `json:"error"`
	Data    struct {
		Digest       string         `json:"digest"`
		State        string         `json:"state"`
		Version      string         `json:"version"`
		VersionCount int            `json:"version_count"`
		Details      map[string]any `json:"details"`
	} `json:"data"`
	Proof *struct {
		Leaf struct {
			VersionNum int32 `json:"version_num"`
		} `json:"leaf"`
	} `json:"proof"`
}

// EnsureRegistry creates the namespace and the registry if they are absent.
//
// A create against something that already exists comes back as a precondition
// failure, and that is treated as success here rather than as an error. This
// runs at every service start; a bootstrap that only works on an empty node is
// a bootstrap that fails on every redeploy.
func (n *Node) EnsureRegistry(ctx context.Context, namespace, registry, description string) error {
	if namespace == "" || registry == "" {
		return fmt.Errorf("dedi: namespace and registry are required")
	}
	nsBody := map[string]any{"payload": map[string]any{
		"name": namespace, "description": description}}
	if _, err := n.write(ctx, http.MethodPut,
		"/admin/namespaces/"+namespace, nsBody, Create()); err != nil && !isExists(err) {
		return fmt.Errorf("ensure namespace %s: %w", namespace, err)
	}
	regBody := map[string]any{"payload": map[string]any{
		"name": registry, "description": description}}
	if _, err := n.write(ctx, http.MethodPut,
		"/admin/namespaces/"+namespace+"/registries/"+registry, regBody, Create()); err != nil && !isExists(err) {
		return fmt.Errorf("ensure registry %s/%s: %w", namespace, registry, err)
	}
	return nil
}

func isExists(err error) bool {
	// A create precondition that failed means the thing is there, which is
	// what EnsureRegistry wanted. Any other precondition failure is a real
	// conflict and must not be swallowed — which is why this is only ever
	// consulted for a Create().
	return err != nil && (strings.Contains(err.Error(), ErrPrecondition.Error()) ||
		strings.Contains(err.Error(), "409"))
}

// Publish writes one version of a record.
func (n *Node) Publish(ctx context.Context, ref Ref, payload any, pre Precondition) (Receipt, error) {
	if err := ref.validate(); err != nil {
		return Receipt{}, err
	}
	if err := pre.validate(); err != nil {
		return Receipt{}, err
	}
	path := fmt.Sprintf("/admin/namespaces/%s/registries/%s/records/%s/publish",
		ref.Namespace, ref.Registry, ref.Record)
	w, err := n.write(ctx, http.MethodPost, path, map[string]any{"payload": payload}, pre)
	if err != nil {
		return Receipt{}, fmt.Errorf("publish %s: %w", ref, err)
	}
	// The publish response does not always carry the digest and state the next
	// compare-and-swap needs, so the receipt is read back rather than assumed.
	// One extra round trip, and in exchange the tag a caller stores is a tag
	// the node agrees with.
	rec, err := n.Resolve(ctx, Ref{Namespace: ref.Namespace, Registry: ref.Registry, Record: ref.Record}, false)
	if err != nil {
		return Receipt{Ref: ref, Digest: w.Data.Digest, State: w.Data.State, Transparent: true},
			fmt.Errorf("published %s but could not read back its version tag: %w", ref, err)
	}
	return rec.Receipt, nil
}

// Resolve reads a record, optionally with its inclusion proof.
//
// The pin check is not defensive programming, it is the P0 spike's finding 3:
// a DeDi node ignores an unrecognised query parameter, so a client asking for
// an old version by a slightly wrong name silently receives the latest one with
// a valid proof attached. In CREST that would be a verifier resolving the wrong
// work definition with no way to notice. So the version that comes back is
// compared with the version that was asked for.
func (n *Node) Resolve(ctx context.Context, ref Ref, withProof bool) (Record, error) {
	if err := ref.validate(); err != nil {
		return Record{}, err
	}
	q := url.Values{}
	if ref.Version != "" {
		q.Set("version_id", ref.Version)
	}
	if withProof {
		q.Set("proof", "inclusion")
	}
	u := n.baseURL + ref.Path()
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Record{}, err
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return Record{}, fmt.Errorf("lookup %s: %w", ref, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Record{}, fmt.Errorf("lookup %s: %w", ref, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return Record{}, fmt.Errorf("%w: %s", ErrNotFound, ref)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Record{}, fmt.Errorf("lookup %s: %s: %s", ref, resp.Status, snippet(raw))
	}
	var w wire
	if err := json.Unmarshal(raw, &w); err != nil {
		return Record{}, fmt.Errorf("lookup %s: unreadable response: %w", ref, err)
	}

	got := Ref{Namespace: ref.Namespace, Registry: ref.Registry, Record: ref.Record, Version: w.Data.Version}
	if ref.Version != "" && w.Data.Version != ref.Version {
		return Record{}, fmt.Errorf("%w: asked for %s, got version %s", ErrPinMissed, ref, w.Data.Version)
	}

	receipt := Receipt{
		Ref: got, Digest: w.Data.Digest, State: w.Data.State,
		VersionNum: versionNum(w.Data.Version), Transparent: true,
	}
	if withProof {
		if w.Proof == nil {
			return Record{}, fmt.Errorf("%w: %s", ErrNoProof, ref)
		}
		// The bytes the node sent, not a re-serialisation of them. The checker
		// recomputes a hash over these fields, and a round trip through Go's
		// map ordering would produce a document that is equivalent JSON and a
		// different proof.
		receipt.Proof = raw
		// The leaf's version_num and the record's version_id count differently
		// — the leaf numbers a record's own versions, the version_id numbers
		// the whole registry's — so the leaf value is kept rather than
		// overwriting one with the other.
		receipt.LeafVersionNum = w.Proof.Leaf.VersionNum
	}
	return Record{Receipt: receipt, Details: w.Data.Details}, nil
}

// write signs and sends one publisher-plane request.
func (n *Node) write(ctx context.Context, method, path string, body any, pre Precondition) (wire, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return wire{}, err
	}
	req, err := http.NewRequestWithContext(ctx, method, n.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return wire{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	// The signature covers the request URI, so it must be signed over exactly
	// the path that is sent. No query string is used on the write plane today;
	// if one ever is, it belongs in this string too.
	n.key.sign(req, path, raw, pre, n.now())

	resp, err := n.http.Do(req)
	if err != nil {
		return wire{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return wire{}, err
	}
	switch {
	case resp.StatusCode == http.StatusPreconditionFailed, resp.StatusCode == http.StatusConflict:
		return wire{}, fmt.Errorf("%w: %s: %s", ErrPrecondition, resp.Status, snippet(respBody))
	case resp.StatusCode == http.StatusNotFound:
		return wire{}, fmt.Errorf("%w: %s", ErrNotFound, path)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return wire{}, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, snippet(respBody))
	}
	var w wire
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &w) // a 2xx with an unreadable body is still a success
	}
	return w, nil
}

// snippet bounds what a node's error body can put into a CREST log line.
func snippet(b []byte) string {
	const max = 400
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	if s == "" {
		return "(empty body)"
	}
	return s
}

// versionNum reads the numeric version out of a node's string version_id, and
// returns 0 for anything it cannot read rather than failing the lookup: the
// version_id string is the identity, and this is a convenience beside it.
func versionNum(s string) int32 {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return int32(n)
}
