package dedi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/sumdb/tlog"
)

// Consuming DeDi's tamper-evidence, not just its inclusion proofs (#241).
//
// A DeDi node serves three things a verifier needs and CREST, until this file,
// asked for exactly none of: a signed tree head (the checkpoint), a consistency
// proof between two tree sizes, and the verdicts of independent witnesses. An
// inclusion proof answers "is this record in the log rooted at R?" — and a
// malicious or compromised operator can rewrite history and mint a fresh R that
// every inclusion proof still checks out against. What that operator cannot do
// is produce a consistency proof from a checkpoint CREST already pinned to the
// rewritten one, or forge an independent witness's signature over the old root.
//
// So this is the other half of §3's promise. CREST pins the newest checkpoint
// it has authenticated, and thereafter refuses to accept any checkpoint that is
// not a forward-only extension of it. History that was appended to verifies;
// history that was rewritten does not, and says so.
//
// The verification primitives are DeDi-node's own — golang.org/x/mod/sumdb's
// tlog and note — so CREST checks the exact bytes the node signed with the same
// code that produced them, rather than a second implementation that can drift.

var (
	// ErrCheckpointUnverified is a checkpoint whose signature did not check out
	// against the configured node key. Distinct from a parse error: the bytes
	// were a well-formed checkpoint, but nothing proves the node authored them.
	ErrCheckpointUnverified = errors.New("dedi: checkpoint signature did not verify against the configured node key")

	// ErrHistoryRewritten is the finding this whole file exists to surface: the
	// node served a checkpoint that is not a forward-only extension of the one
	// CREST pinned. On an append-only log this cannot happen honestly, so it is
	// reported as tamper, never retried into silence.
	ErrHistoryRewritten = errors.New("dedi: the node served a checkpoint that is not an append-only extension of the pinned one; history may have been rewritten")
)

// Checkpoint is a signed tree head: the log's size and Merkle root at a point in
// time, carried in the exact C2SP note bytes the node signed.
type Checkpoint struct {
	Origin string
	Size   int64
	Root   tlog.Hash

	// Note is the raw note, exactly as served. Kept verbatim because the
	// signature covers these bytes and a re-serialisation is no longer signed.
	Note []byte

	// Verified reports that Note's signature checked out against the configured
	// node verifier key. False when no key is configured — the checkpoint was
	// parsed but not authenticated, which is a materially weaker statement and
	// is never allowed to pass for the stronger one.
	Verified bool
}

// SetCheckpointVerifier installs the node's checkpoint verifier key (the note
// verifier string a DeDi node publishes: "<name>+<hash>+<base64>").
func (n *Node) SetCheckpointVerifier(v note.Verifier) { n.cpVerifier = v }

// NewReadNode builds a read-only client: it fetches checkpoints, consistency
// proofs and witness verdicts, all of which are unsigned reads, and cannot
// publish. The consistency monitor (#241) uses this rather than NewNode because
// monitoring the log for tamper is a read and must not require the publisher's
// signing key — a service that only verifies should not hold write credentials.
func NewReadNode(baseURL string) (*Node, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("dedi: node URL is required")
	}
	return &Node{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
		now:     func() time.Time { return time.Now().UTC() },
	}, nil
}

// ParseCheckpointNote reads a stored checkpoint note back into its contents,
// without authenticating it. It is for reloading a pin CREST already verified
// when it saved it: the note is the source of truth for size and root, and the
// caller restores Verified from its own record. A fresh checkpoint off the wire
// always goes through Checkpoint, which authenticates.
func ParseCheckpointNote(raw []byte) (Checkpoint, error) {
	origin, size, root, err := splitCheckpoint(raw)
	if err != nil {
		return Checkpoint{}, err
	}
	return Checkpoint{Origin: origin, Size: size, Root: root, Note: append([]byte(nil), raw...)}, nil
}

// Checkpoint fetches and authenticates the node's current signed tree head.
func (n *Node) Checkpoint(ctx context.Context) (Checkpoint, error) {
	raw, err := n.get(ctx, "/dedi/log/checkpoint")
	if err != nil {
		return Checkpoint{}, fmt.Errorf("fetch checkpoint: %w", err)
	}
	return n.parseCheckpoint(raw)
}

func (n *Node) parseCheckpoint(raw []byte) (Checkpoint, error) {
	origin, size, root, err := splitCheckpoint(raw)
	if err != nil {
		return Checkpoint{}, err
	}
	cp := Checkpoint{Origin: origin, Size: size, Root: root, Note: append([]byte(nil), raw...)}
	if n.cpVerifier == nil {
		// Parsed, not authenticated. Reported honestly rather than silently
		// trusted — an unauthenticated checkpoint is exactly what an attacker in
		// the path would hand us.
		return cp, nil
	}
	if _, err := note.Open(raw, note.VerifierList(n.cpVerifier)); err != nil {
		return cp, fmt.Errorf("%w: %w", ErrCheckpointUnverified, err)
	}
	cp.Verified = true
	return cp, nil
}

// splitCheckpoint reads the C2SP checkpoint body: origin, decimal size, base64
// root, one per line, before the blank line that separates the signatures.
func splitCheckpoint(raw []byte) (origin string, size int64, root tlog.Hash, err error) {
	body := string(raw)
	if i := strings.Index(body, "\n\n"); i >= 0 {
		body = body[:i]
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) < 3 {
		return "", 0, tlog.Hash{}, fmt.Errorf("dedi: checkpoint has %d body lines, want at least 3 (origin, size, root)", len(lines))
	}
	origin = lines[0]
	size, err = strconv.ParseInt(lines[1], 10, 64)
	if err != nil || size < 0 {
		return "", 0, tlog.Hash{}, fmt.Errorf("dedi: checkpoint size %q is not a non-negative integer", lines[1])
	}
	rb, err := base64.StdEncoding.DecodeString(lines[2])
	if err != nil || len(rb) != tlog.HashSize {
		return "", 0, tlog.Hash{}, fmt.Errorf("dedi: checkpoint root %q is not a %d-byte base64 hash", lines[2], tlog.HashSize)
	}
	copy(root[:], rb)
	return origin, size, root, nil
}

// ConfirmExtends verifies that the node's current checkpoint is a forward-only
// extension of pinned — the log was only appended to, never rewritten — and
// returns the current checkpoint. A pinned checkpoint with Size 0 is the
// first-ever observation and always passes: there is no earlier history to
// contradict.
func (n *Node) ConfirmExtends(ctx context.Context, pinned Checkpoint) (Checkpoint, error) {
	cur, err := n.Checkpoint(ctx)
	if err != nil {
		return Checkpoint{}, err
	}
	if pinned.Size == 0 {
		return cur, nil
	}
	if err := n.checkExtension(ctx, pinned, cur); err != nil {
		return cur, err
	}
	return cur, nil
}

func (n *Node) checkExtension(ctx context.Context, old, cur Checkpoint) error {
	switch {
	case cur.Size < old.Size:
		// An append-only log cannot shrink. The plainest rewrite there is.
		return fmt.Errorf("%w: pinned size %d, node now serves size %d", ErrHistoryRewritten, old.Size, cur.Size)
	case cur.Size == old.Size:
		// Same size must mean the same root; a different root at the same size is
		// a rewrite in place.
		if cur.Root != old.Root {
			return fmt.Errorf("%w: at size %d the root changed from %x to %x", ErrHistoryRewritten, old.Size, old.Root, cur.Root)
		}
		return nil
	default:
		// Grown: the node must prove the old tree is a prefix of the new one.
		proof, err := n.consistencyProof(ctx, old.Size, cur.Size)
		if err != nil {
			return err
		}
		if err := tlog.CheckTree(proof, cur.Size, cur.Root, old.Size, old.Root); err != nil {
			return fmt.Errorf("%w: consistency proof %d→%d did not verify: %w", ErrHistoryRewritten, old.Size, cur.Size, err)
		}
		return nil
	}
}

func (n *Node) consistencyProof(ctx context.Context, old, newer int64) (tlog.TreeProof, error) {
	raw, err := n.get(ctx, fmt.Sprintf("/dedi/log/proof/consistency?old=%d&new=%d", old, newer))
	if err != nil {
		return nil, fmt.Errorf("fetch consistency proof %d→%d: %w", old, newer, err)
	}
	var w struct {
		Data struct {
			Proof []string `json:"proof"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("consistency proof %d→%d: unreadable response: %w", old, newer, err)
	}
	proof := make(tlog.TreeProof, len(w.Data.Proof))
	for i, s := range w.Data.Proof {
		hb, err := base64.StdEncoding.DecodeString(s)
		if err != nil || len(hb) != tlog.HashSize {
			return nil, fmt.Errorf("consistency proof %d→%d: hash %d is not a %d-byte base64 hash", old, newer, i, tlog.HashSize)
		}
		copy(proof[i][:], hb)
	}
	return proof, nil
}

// WitnessVerdict is one node's independently checkable conclusion about a
// target log it witnesses: whether the target's history has stayed consistent,
// at what size and root.
type WitnessVerdict struct {
	// Origin is the target log's checkpoint origin line.
	Origin string
	// Witnessed is false when the witness holds a registration for the target
	// but has never successfully verified it — "not yet checked" is not "checked
	// and sound", and the two must not read alike.
	Witnessed bool
	// ConsistencyOK is the verdict: the target's log extended append-only across
	// every check this witness ran.
	ConsistencyOK bool
	Size          int64
	Root          string
	// Detail is set only when the witness caught something (today, a root that
	// changed under an unchanged size). It is the reason the endpoint exists, so
	// it is never summarised away.
	Detail string
}

// WitnessVerdictFor asks an independent witness node what it has concluded about
// a target log. witnessBaseURL is the witness node's root; targetOrigin is the
// target's checkpoint origin. This is how CREST confirms its own node's log is
// vouched for by a party other than the node itself — the point of §3's witness
// ring. Until a second node exists (#76) there is no independent witness to ask,
// and callers treat an unconfigured witness as an honest "not established",
// never as "sound".
func (n *Node) WitnessVerdictFor(ctx context.Context, witnessBaseURL, targetOrigin string) (WitnessVerdict, error) {
	base := strings.TrimRight(witnessBaseURL, "/")
	raw, err := n.getFrom(ctx, base+"/dedi/witness/"+pathEscapeOrigin(targetOrigin))
	if err != nil {
		return WitnessVerdict{}, fmt.Errorf("fetch witness verdict for %q: %w", targetOrigin, err)
	}
	var w struct {
		Data struct {
			Origin        string `json:"origin"`
			Witnessed     bool   `json:"witnessed"`
			ConsistencyOK bool   `json:"consistency_ok"`
			Size          int64  `json:"size"`
			Root          string `json:"root"`
			Detail        string `json:"detail"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return WitnessVerdict{}, fmt.Errorf("witness verdict for %q: unreadable response: %w", targetOrigin, err)
	}
	return WitnessVerdict{
		Origin: w.Data.Origin, Witnessed: w.Data.Witnessed, ConsistencyOK: w.Data.ConsistencyOK,
		Size: w.Data.Size, Root: w.Data.Root, Detail: w.Data.Detail,
	}, nil
}

// pathEscapeOrigin percent-encodes a checkpoint origin for a path segment. An
// origin is a log identifier that can contain slashes, so it is escaped whole
// (url.PathEscape encodes "/" as "%2F"), matching how DeDi-node's witness view
// reaches the same record.
func pathEscapeOrigin(origin string) string {
	return url.PathEscape(origin)
}

// get performs an unsigned read-plane GET against this node.
func (n *Node) get(ctx context.Context, path string) ([]byte, error) {
	return n.getFrom(ctx, n.baseURL+path)
}

// getFrom performs an unsigned GET against an absolute URL — used for the read
// plane on this node and on a separate witness node.
func (n *Node) getFrom(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, u)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, snippet(raw))
	}
	return raw, nil
}
