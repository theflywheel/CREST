package dedi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/store"
)

// Fallback is a Publisher backed by the service's own Postgres schema.
//
// #20 asks for this explicitly, because DeDi-node is at M1 and a write-path
// feature that lags must not stop the core loop. What it gives up is exactly
// the property DeDi exists for: there is no transparency log, so there is no
// inclusion proof, so a verifier has nothing to check that does not amount to
// trusting CREST's database. Transparent() therefore returns false and every
// receipt says so — a fallback that impersonated a log would turn a temporary
// gap into a permanently unverifiable record.
//
// It keeps the properties it can keep honestly: versions are append-only, an
// old version stays resolvable after a new one supersedes it, and the same
// compare-and-swap refuses two concurrent editors. Those are the ones that,
// once broken, cannot be repaired by moving to a real node later.
type Fallback struct {
	db  *store.DB
	clk clock.Clock
}

// NewFallback builds the Postgres-backed publisher. The table it uses lives in
// the calling service's own schema and is created by that service's migrations
// — pkg/store is the only place that opens a database, and a package that
// created its own tables in someone else's schema would break the rule that
// makes "what could this service have written" answerable.
func NewFallback(db *store.DB, clk clock.Clock) *Fallback {
	return &Fallback{db: db, clk: clk}
}

// Transparent reports false. See the type comment; callers are expected to
// branch on this rather than to discover it.
func (f *Fallback) Transparent() bool { return false }

// EnsureRegistry is a no-op with a real meaning: there is no namespace to
// create, because the rows carry their namespace and registry as columns.
// Returning nil rather than an error keeps the bootstrap path identical in both
// deployments, which is what makes swapping the implementation a config change.
func (f *Fallback) EnsureRegistry(ctx context.Context, namespace, registry, description string) error {
	if namespace == "" || registry == "" {
		return fmt.Errorf("dedi: namespace and registry are required")
	}
	return nil
}

// Publish appends a version, honouring the precondition.
//
// The read and the write are in one transaction with the existing rows locked,
// so two publishers cannot both see the same latest version and both append.
func (f *Fallback) Publish(ctx context.Context, ref Ref, payload any, pre Precondition) (Receipt, error) {
	if err := ref.validate(); err != nil {
		return Receipt{}, err
	}
	if err := pre.validate(); err != nil {
		return Receipt{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Receipt{}, fmt.Errorf("dedi: marshal payload: %w", err)
	}
	// Canonicalised before hashing, so the digest is a property of the content
	// and not of Go's map iteration order. Two publishes of the same fact have
	// to produce the same digest or the compare-and-swap tag is noise.
	canon, err := credential.Canonicalise(raw)
	if err != nil {
		return Receipt{}, fmt.Errorf("dedi: canonicalise payload: %w", err)
	}
	sum := sha256.Sum256(canon)
	digest := hex.EncodeToString(sum[:])

	var receipt Receipt
	err = f.db.InTx(ctx, func(tx store.Querier) error {
		var latest int32
		var latestDigest, latestState string
		err := tx.QueryRow(ctx, `
			SELECT version, digest, state FROM dedi_records
			WHERE namespace = $1 AND registry = $2 AND record = $3
			ORDER BY version DESC LIMIT 1
			FOR UPDATE`, ref.Namespace, ref.Registry, ref.Record).
			Scan(&latest, &latestDigest, &latestState)
		switch {
		case errors.Is(err, store.ErrNotFound):
			if !pre.Create {
				return fmt.Errorf("%w: nothing to replace at %s", ErrPrecondition, ref)
			}
		case err != nil:
			return err
		default:
			if pre.Create {
				return fmt.Errorf("%w: %s already exists", ErrPrecondition, ref)
			}
			if got := latestDigest + "-" + latestState; got != pre.IfMatch {
				return fmt.Errorf("%w: %s is at %s, not %s", ErrPrecondition, ref, got, pre.IfMatch)
			}
		}

		next := latest + 1
		if _, err := tx.Exec(ctx, `
			INSERT INTO dedi_records (namespace, registry, record, version, digest, state, details, created_at)
			VALUES ($1, $2, $3, $4, $5, 'live', $6, $7)`,
			ref.Namespace, ref.Registry, ref.Record, next, digest, canon, f.clk.Now()); err != nil {
			return err
		}
		receipt = Receipt{
			Ref: Ref{Namespace: ref.Namespace, Registry: ref.Registry,
				Record: ref.Record, Version: fmt.Sprint(next)},
			Digest: digest, State: "live", VersionNum: next, Transparent: false,
		}
		return nil
	})
	if err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

// Resolve reads a version, defaulting to the newest.
//
// withProof is answered with ErrNoProof rather than with a receipt that has an
// empty Proof field. A caller that asked for something checkable and got a
// silent nil would carry on as though it had one.
func (f *Fallback) Resolve(ctx context.Context, ref Ref, withProof bool) (Record, error) {
	if err := ref.validate(); err != nil {
		return Record{}, err
	}
	if withProof {
		return Record{}, fmt.Errorf("%w: this deployment has no transparency log behind its registry", ErrNoProof)
	}
	var version int32
	var digest, state string
	var details []byte
	// $4 = '' means "the newest"; the pin is applied in SQL rather than by
	// fetching the newest and comparing, so there is no path where a missed
	// pin quietly answers with a different version.
	err := f.db.Q().QueryRow(ctx, `
		SELECT version, digest, state, details FROM dedi_records
		WHERE namespace = $1 AND registry = $2 AND record = $3
		  AND ($4 = '' OR version::text = $4)
		ORDER BY version DESC LIMIT 1`,
		ref.Namespace, ref.Registry, ref.Record, ref.Version).
		Scan(&version, &digest, &state, &details)
	if errors.Is(err, store.ErrNotFound) {
		return Record{}, fmt.Errorf("%w: %s", ErrNotFound, ref)
	}
	if err != nil {
		return Record{}, err
	}
	var out map[string]any
	if err := json.Unmarshal(details, &out); err != nil {
		return Record{}, fmt.Errorf("dedi: stored payload for %s is unreadable: %w", ref, err)
	}
	return Record{
		Receipt: Receipt{
			Ref: Ref{Namespace: ref.Namespace, Registry: ref.Registry,
				Record: ref.Record, Version: fmt.Sprint(version)},
			Digest: digest, State: state, VersionNum: version, Transparent: false,
		},
		Details: out,
	}, nil
}

// Digest is the content address of a payload, computed the same way a
// publication computes it.
//
// Exported so a caller can ask "has this changed since I last published it?"
// without publishing to find out. Canonicalised before hashing, so the answer
// is a property of the content and not of Go's map iteration order — two
// marshals of the same fact must produce the same digest or the question is
// meaningless.
func Digest(payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("dedi: marshal payload: %w", err)
	}
	canon, err := credential.Canonicalise(raw)
	if err != nil {
		return "", fmt.Errorf("dedi: canonicalise payload: %w", err)
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}
