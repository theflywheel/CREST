// Command dediproof independently validates a DeDi inclusion proof.
//
// P0 spike #2 asks for the proof to be "validated independently". Calling
// DeDi's own verifier would only prove DeDi agrees with itself, so this is a
// second implementation written from the wire format: recompute the leaf hash
// from the leaf fields, walk the audit path to a root, and check that root
// against the signed checkpoint the node returned.
//
// It reads the lookup response (…?proof=inclusion) on stdin and takes the
// node's public verifier key as a flag:
//
//	curl -s "$DEDI_URL/dedi/lookup/ns/reg/rec?proof=inclusion" |
//	  go run ./tools/spikes/dediproof -key "dedi.local+d8cd91b7+Aas26Ju..."
//
// Only the standard library is used, deliberately: a proof checker that needs
// a dependency tree is one nobody will run at the edge, and CREST's own
// verification path (Blueprint §7) has the same constraint.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type response struct {
	Data struct {
		Digest string `json:"digest"`
	} `json:"data"`
	Proof struct {
		LeafIndex  int64    `json:"leaf_index"`
		TreeSize   int64    `json:"tree_size"`
		Checkpoint string   `json:"checkpoint"`
		Path       []string `json:"path"`
		Leaf       struct {
			EntryType  string `json:"entry_type"`
			Namespace  string `json:"namespace"`
			Registry   string `json:"registry"`
			RecordName string `json:"record_name"`
			VersionNum int32  `json:"version_num"`
			Digest     string `json:"digest"`
			CreatedBy  string `json:"created_by"`
			CreatedAt  string `json:"created_at"`
		} `json:"leaf"`
	} `json:"proof"`
}

func main() {
	key := flag.String("key", "", "node verifier key, as printed by `dedid keygen` (required)")
	flag.Parse()
	if *key == "" {
		fatal("-key is required (the node's public verifier key)")
	}

	var r response
	if err := json.NewDecoder(os.Stdin).Decode(&r); err != nil {
		fatal("reading lookup response: %v", err)
	}
	if r.Proof.Checkpoint == "" {
		fatal("no proof in the response — was ?proof=inclusion set?")
	}

	// 1. The leaf must be the record we asked about, not merely *a* leaf.
	if r.Data.Digest != "" && r.Proof.Leaf.Digest != r.Data.Digest {
		fatal("leaf digest %s does not match the record digest %s",
			r.Proof.Leaf.Digest, r.Data.Digest)
	}

	leafBytes, err := canonicalLeaf(r)
	if err != nil {
		fatal("%v", err)
	}
	leafHash := recordHash(leafBytes)

	// 2. Walk the audit path to a root.
	root, err := rootFromPath(r.Proof.LeafIndex, r.Proof.TreeSize, leafHash, r.Proof.Path)
	if err != nil {
		fatal("%v", err)
	}

	// 3. The checkpoint must be signed by the key we were given, and must
	//    commit to that root at that size. Checking the root without checking
	//    the signature proves nothing: anyone can produce a consistent tree.
	origin, size, cpRoot, err := openNote(r.Proof.Checkpoint, *key)
	if err != nil {
		fatal("%v", err)
	}
	if size != r.Proof.TreeSize {
		fatal("checkpoint size %d does not match proof tree_size %d", size, r.Proof.TreeSize)
	}
	if root != cpRoot {
		fatal("computed root %s does not match the signed checkpoint root %s",
			base64.StdEncoding.EncodeToString(root[:]),
			base64.StdEncoding.EncodeToString(cpRoot[:]))
	}

	fmt.Printf("inclusion proof valid\n")
	fmt.Printf("  origin      %s\n", origin)
	fmt.Printf("  record      %s/%s/%s v%d\n", r.Proof.Leaf.Namespace,
		r.Proof.Leaf.Registry, r.Proof.Leaf.RecordName, r.Proof.Leaf.VersionNum)
	fmt.Printf("  leaf %d of %d, %d-step path\n", r.Proof.LeafIndex, size, len(r.Proof.Path))
	fmt.Printf("  root        %s (signature checked)\n", base64.StdEncoding.EncodeToString(root[:]))
}

// canonicalLeaf reproduces DeDi's leaf preimage: a fixed-order JSON array
// tagged "dedi/v1/leaf". Reproducing it here rather than importing it is the
// point — if the encoding drifts, this fails loudly instead of following.
func canonicalLeaf(r response) ([]byte, error) {
	l := r.Proof.Leaf
	if _, err := hex.DecodeString(l.Digest); err != nil {
		return nil, fmt.Errorf("leaf digest is not hex: %w", err)
	}
	at, err := time.Parse(time.RFC3339Nano, l.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("leaf created_at: %w", err)
	}
	return json.Marshal([]any{
		"dedi/v1/leaf",
		l.EntryType, l.Namespace, l.Registry, l.RecordName,
		l.VersionNum,
		strings.ToLower(l.Digest),
		l.CreatedBy,
		at.UTC().Format(time.RFC3339Nano),
	})
}

type hash [sha256.Size]byte

// RFC 6962 domain separation: leaves and interior nodes hash under different
// prefixes, so a leaf can never be presented as a subtree.
func recordHash(b []byte) hash {
	return sha256.Sum256(append([]byte{0x00}, b...))
}

func nodeHash(l, r hash) hash {
	b := make([]byte, 0, 1+2*sha256.Size)
	b = append(b, 0x01)
	b = append(b, l[:]...)
	b = append(b, r[:]...)
	return sha256.Sum256(b)
}

func rootFromPath(index, size int64, leaf hash, path []string) (hash, error) {
	if index < 0 || size <= 0 || index >= size {
		return hash{}, fmt.Errorf("leaf index %d out of range for tree of %d", index, size)
	}
	r := leaf
	fn, sn := index, size-1
	for i, enc := range path {
		b, err := base64.StdEncoding.DecodeString(enc)
		if err != nil || len(b) != sha256.Size {
			return hash{}, fmt.Errorf("path step %d is not a base64 SHA-256 hash", i)
		}
		var sib hash
		copy(sib[:], b)
		if fn == sn || fn&1 == 1 {
			r = nodeHash(sib, r)
			for fn != 0 && fn&1 == 0 {
				fn >>= 1
				sn >>= 1
			}
		} else {
			r = nodeHash(r, sib)
		}
		fn >>= 1
		sn >>= 1
	}
	if fn != 0 {
		return hash{}, fmt.Errorf("audit path ended before reaching the root (%d steps)", len(path))
	}
	return r, nil
}

// openNote verifies a signed note (the checkpoint format from the Go
// checksum-database work) and returns what it commits to. The key is
// "<name>+<hash8>+<base64 32-byte ed25519 public key>"; the signature line is
// "— <name> <base64(keyhash || sig)>".
func openNote(text, verifierKey string) (origin string, size int64, root hash, err error) {
	parts := strings.Split(verifierKey, "+")
	if len(parts) != 3 {
		return "", 0, root, fmt.Errorf("verifier key must be name+hash+base64key")
	}
	name := parts[0]
	pub, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil || len(pub) != 1+ed25519.PublicKeySize {
		return "", 0, root, fmt.Errorf("verifier key payload is not an algorithm byte plus 32 bytes")
	}
	pub = pub[1:] // leading byte is the algorithm identifier

	// A note is "<text>\n<blank line>\n<signature lines>". The signature covers
	// the text including its trailing newline, and nothing after the blank line.
	i := strings.Index(text, "\n\n")
	if i < 0 {
		return "", 0, root, fmt.Errorf("checkpoint has no signature block")
	}
	body, sigBlock := text[:i+1], text[i+2:]

	verified := false
	for _, line := range strings.Split(strings.TrimRight(sigBlock, "\n"), "\n") {
		f := strings.Fields(line)
		// "—" (em dash) name base64
		if len(f) != 3 || f[1] != name {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(f[2])
		if err != nil || len(raw) != 4+ed25519.SignatureSize {
			continue
		}
		if ed25519.Verify(pub, []byte(body), raw[4:]) {
			verified = true
			break
		}
	}
	if !verified {
		return "", 0, root, fmt.Errorf("no signature on the checkpoint verifies under %q", name)
	}

	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(lines) < 3 {
		return "", 0, root, fmt.Errorf("checkpoint body has %d lines, want >=3", len(lines))
	}
	origin = lines[0]
	if size, err = strconv.ParseInt(lines[1], 10, 64); err != nil {
		return "", 0, root, fmt.Errorf("checkpoint size: %w", err)
	}
	rb, err := base64.StdEncoding.DecodeString(lines[2])
	if err != nil || len(rb) != sha256.Size {
		return "", 0, root, fmt.Errorf("checkpoint root is not a base64 SHA-256 hash")
	}
	copy(root[:], rb)
	return origin, size, root, nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "dediproof: "+format+"\n", args...)
	os.Exit(1)
}

// Sentinel errors so tests can assert on the reason rather than on a string.
var (
	errSize = fmt.Errorf("checkpoint size does not match the proof")
	errRoot = fmt.Errorf("computed root does not match the signed checkpoint")
)
