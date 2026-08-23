// Package dedi is CREST's registry substrate: the public half of Blueprint §3.
//
// The placement rule §3 states is not a preference. Public facts — which
// organisations exist, which terms they hold, which authorizations they were
// granted, and what a work definition says counts as done — go to a
// transparency log a verifier can check without asking CREST. Personal data
// never does. A verifier who has to trust CREST's own database about which
// terms an organisation held at issuance time is not verifying anything; they
// are being told.
//
// Everything here goes through Publisher rather than through an HTTP client,
// for the reason #20 gives: DeDi-node is at M1, and a deployment that cannot
// reach a node still has to keep working. Two implementations satisfy it — a
// real node (Node) and a Postgres-backed store (Fallback) — and they are
// deliberately NOT interchangeable in the property that matters. A node receipt
// carries a Merkle inclusion proof; a fallback receipt carries none and says so
// in Transparent. Code that needs a proof must check that field, because a
// fallback that silently passes for a transparency log is how an unverifiable
// record reaches a pilot wearing a green test.
package dedi

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Errors callers distinguish. Everything else is transport trouble.
var (
	// ErrNotFound is a record, registry or namespace that is not there.
	ErrNotFound = errors.New("dedi: no such record")

	// ErrPrecondition is a compare-and-swap that lost: something else wrote
	// the record between the read and the write. It is never retried blindly —
	// the caller re-reads and decides, because a blind retry is how one
	// publisher's edit silently overwrites another's.
	ErrPrecondition = errors.New("dedi: precondition failed")

	// ErrPinMissed is the finding with teeth from the P0 spike (docs/spikes/dedi.md,
	// finding 3). A DeDi node ignores an unrecognised query parameter rather
	// than rejecting it, so a client that asks for version 1 and misspells the
	// parameter gets the LATEST version with a perfectly valid proof attached.
	// Resolve therefore checks that the version it got back is the version it
	// pinned, and refuses rather than answering a different question.
	ErrPinMissed = errors.New("dedi: the node returned a different version than the one pinned")

	// ErrNoProof is a proof asked for and not supplied. Distinct from a
	// transport error so a caller cannot mistake "no proof" for "no answer".
	ErrNoProof = errors.New("dedi: response carried no inclusion proof")
)

// Ref names one record. Version is the node's version_id; empty means "the
// current one", which is correct for governance reads and wrong for anything a
// credential pinned.
type Ref struct {
	Namespace string
	Registry  string
	Record    string
	Version   string
}

// Path is the lookup path for this ref, without the query string.
func (r Ref) Path() string {
	return fmt.Sprintf("/dedi/lookup/%s/%s/%s", r.Namespace, r.Registry, r.Record)
}

// String is what appears in a log line or an error.
func (r Ref) String() string {
	s := r.Namespace + "/" + r.Registry + "/" + r.Record
	if r.Version != "" {
		s += "@" + r.Version
	}
	return s
}

func (r Ref) validate() error {
	for name, v := range map[string]string{
		"namespace": r.Namespace, "registry": r.Registry, "record": r.Record,
	} {
		if v == "" {
			return fmt.Errorf("dedi: %s is required", name)
		}
		// The node builds paths from these and the signature covers the path,
		// so a value containing a slash signs one route and hits another.
		if strings.ContainsAny(v, "/?#% ") {
			return fmt.Errorf("dedi: %s %q contains a character that cannot appear in a path", name, v)
		}
	}
	return nil
}

// Receipt is what a write returns and what a read returns alongside the record.
//
// Tag exists because of finding 4 in docs/spikes/dedi.md: If-Match wants
// "<digest>-<state>", but a lookup returns digest and state as separate fields
// with no combined tag, so every client re-derives a format only the server
// should own. CREST re-derives it exactly once, here.
type Receipt struct {
	Ref        Ref
	Digest     string
	State      string
	VersionNum int32

	// LeafVersionNum is the version number the transparency log's leaf carries,
	// set only when a proof was requested. It counts a record's own versions,
	// where VersionNum counts the registry's, and the two are not the same
	// number — a pin checked against the wrong one passes for the wrong reason.
	LeafVersionNum int32

	// Transparent reports whether this receipt is backed by a Merkle inclusion
	// proof from a transparency log. False for the Postgres fallback. A caller
	// that needs to hand a verifier something checkable must not treat the two
	// as equivalent, and the field is named so that ignoring it is visible in
	// review rather than invisible in behaviour.
	Transparent bool

	// Proof is the raw lookup response, exactly as the node returned it, when
	// the read asked for one. It is kept as raw bytes and not parsed here: the
	// checker is a separate implementation written from the wire format, and a
	// proof re-serialised through this package's structs is no longer the bytes
	// the node signed.
	Proof []byte
}

// Tag is the If-Match value for the version this receipt describes.
func (r Receipt) Tag() string {
	if r.Digest == "" || r.State == "" {
		return ""
	}
	return r.Digest + "-" + r.State
}

// Record is a resolved record: its payload and the receipt that locates it.
type Record struct {
	Receipt Receipt
	Details map[string]any
}

// Precondition is the conditional part of a write, and it is signed rather than
// merely sent — see the note on Sign. Exactly one of the two is set.
type Precondition struct {
	// IfNoneMatch means "this must not exist yet".
	Create bool
	// IfMatch is the Tag of the version being replaced.
	IfMatch string
}

// Create is the precondition for a first publication.
func Create() Precondition { return Precondition{Create: true} }

// Replace is the precondition for superseding a known version.
func Replace(tag string) Precondition { return Precondition{IfMatch: tag} }

func (p Precondition) validate() error {
	if p.Create == (p.IfMatch != "") {
		return errors.New("dedi: give exactly one of Create() or Replace(tag)")
	}
	return nil
}

// Publisher is the whole surface CREST has on its registry substrate.
//
// It is small on purpose. Every method a service can call is a method a
// fallback has to be honest about, and the four below are the ones §3 actually
// needs: make a registry exist, write a public fact, read one back, and list
// what a record's history is.
type Publisher interface {
	// EnsureRegistry makes the namespace and registry exist. Idempotent: an
	// already-present registry is success, not a conflict, because bootstrap
	// runs on every start and a service that will not start twice is a service
	// that cannot be redeployed.
	EnsureRegistry(ctx context.Context, namespace, registry, description string) error

	// Publish writes one version of a record.
	Publish(ctx context.Context, ref Ref, payload any, pre Precondition) (Receipt, error)

	// Resolve reads a record. When ref.Version is set the returned record is
	// that version or an error — never a different version that happens to
	// answer.
	Resolve(ctx context.Context, ref Ref, withProof bool) (Record, error)

	// Transparent reports whether this publisher is backed by a transparency
	// log at all. A deployment can log this at boot, which is the only moment
	// anyone would notice they are running without one.
	Transparent() bool
}
