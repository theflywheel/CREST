package main

import (
	"encoding/json"
	"os"
	"testing"
)

// A real lookup response captured from a live DeDi node during the P0 spike
// (#2), together with that node's public verifier key. Nothing here is
// sensitive: a verifier key is meant to be published, and the record is a
// work-definition public face carrying no personal data.
const spikeVerifierKey = "dedi.local+d8cd91b7+Aas26JuKhnhmKLFsrt1bq6QQGA7MeVGBTbMyvqWE6Z5h"

func load(t *testing.T) response {
	t.Helper()
	raw, err := os.ReadFile("testdata/proof.json")
	if err != nil {
		t.Fatal(err)
	}
	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// check runs the same three steps main() does, so a test that passes says
// something about the command rather than about the helpers.
func check(r response, key string) error {
	leaf, err := canonicalLeaf(r)
	if err != nil {
		return err
	}
	root, err := rootFromPath(r.Proof.LeafIndex, r.Proof.TreeSize, recordHash(leaf), r.Proof.Path)
	if err != nil {
		return err
	}
	_, size, cpRoot, err := openNote(r.Proof.Checkpoint, key)
	if err != nil {
		return err
	}
	if size != r.Proof.TreeSize {
		return errSize
	}
	if root != cpRoot {
		return errRoot
	}
	return nil
}

func TestGoldenProofValidates(t *testing.T) {
	if err := check(load(t), spikeVerifierKey); err != nil {
		t.Fatalf("a proof captured from a real node must validate: %v", err)
	}
}

// The point of a proof checker is what it REJECTS. A checker that only ever
// sees good input is indistinguishable from `return nil`, so every field the
// proof commits to is tampered with here and each must be caught.
func TestTamperedProofsAreRejected(t *testing.T) {
	cases := map[string]func(*response){
		"leaf digest altered": func(r *response) {
			r.Proof.Leaf.Digest = "00" + r.Proof.Leaf.Digest[2:]
		},
		"author rewritten": func(r *response) {
			r.Proof.Leaf.CreatedBy = "publisher:someone-else"
		},
		"version number changed": func(r *response) {
			r.Proof.Leaf.VersionNum++
		},
		"record renamed": func(r *response) {
			r.Proof.Leaf.RecordName = "WD-9999"
		},
		"namespace changed": func(r *response) {
			r.Proof.Leaf.Namespace = "someone.else"
		},
		"timestamp moved": func(r *response) {
			r.Proof.Leaf.CreatedAt = "2020-01-01T00:00:00Z"
		},
		"audit path step flipped": func(r *response) {
			if len(r.Proof.Path) == 0 {
				return
			}
			b := []byte(r.Proof.Path[0])
			if b[0] == 'A' {
				b[0] = 'B'
			} else {
				b[0] = 'A'
			}
			r.Proof.Path[0] = string(b)
		},
		"tree size inflated": func(r *response) {
			r.Proof.TreeSize += 8
		},
		"leaf index moved": func(r *response) {
			r.Proof.LeafIndex = 0
		},
	}

	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			r := load(t)
			tamper(&r)
			if err := check(r, spikeVerifierKey); err == nil {
				t.Fatal("tampered proof was accepted")
			}
		})
	}
}

// Checking the audit path without checking who signed the checkpoint proves
// nothing: anyone can build a self-consistent tree.
func TestUnsignedByTheExpectedKeyIsRejected(t *testing.T) {
	// Same key with one byte of the public key flipped.
	wrong := "dedi.local+d8cd91b7+AVQ26JuKhnhmKLFsrt1bq6QQGA7MeVGBTbMyvqWE6Z5h"
	if err := check(load(t), wrong); err == nil {
		t.Fatal("a checkpoint signed by a different key was accepted")
	}
}

func TestRecordAndInteriorNodesHashDifferently(t *testing.T) {
	// RFC 6962 domain separation. Without it a leaf could be presented as a
	// subtree, which is how a forged inclusion proof gets built.
	leaf := recordHash([]byte{})
	node := nodeHash(hash{}, hash{})
	if leaf == node {
		t.Fatal("leaf and node hashing are not domain-separated")
	}
}
