---
title: The registry substrate
---

# The registry substrate

Public facts go to a DeDi node: the instance's self-description, organisation faces, published definitions, recognised adapter versions, terms sets. The node gives every record tamper-evidence, inclusion proofs, signed checkpoints and witnessing by the global node.

## The interface

```go
type Publisher interface {
    EnsureRegistry(ctx, namespace, registry, description string) error
    Publish(ctx, ref Ref, payload any, pre Precondition) (Receipt, error)
    Resolve(ctx, ref Ref, withProof bool) (Record, error)
    Transparent() bool
}
```

Small on purpose: every method a service can call is a method a fallback has to be honest about. `EnsureRegistry` is idempotent because bootstrap runs on every start. `Resolve` with a version returns that version or an error, never a different one that happens to answer. `Transparent()` reports whether a log is behind the publisher at all; a deployment logs it at boot, the only moment anyone would notice they were running without one.

## What never goes to the log

The parties service refuses to publish a fact that holds personal data, whichever publisher is configured, and logs the refusal naming the record. Names, identifiers, consents, claims, windows and grants stay in the private store. An organisation's self-declared attributes ride the private registration read, not the public face.

## Configuration

| Setting | Meaning |
|---|---|
| `DEDI_URL` | The node; empty runs the announced fallback |
| `DEDI_NAMESPACE`, `DEDI_KEY_ID` | The deployment's namespace and signing key id |
| `DEDI_PUBLISHER_KEY`, `DEDI_PUBLISHER_KEYS` | The key that writes to the log, generated per deployment with `make dedi-keys` |

The key's custody, and how a deployment proves its log independently (`make verify-deployed`), are in ../DEPLOYMENT.md (`docs/DEPLOYMENT.md` in the repository).
