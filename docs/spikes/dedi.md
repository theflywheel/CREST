# P0 spike: DeDi as CREST's registry substrate (#2)

**Status: passing.** Publish → inclusion proof → independent validation → v2 → both versions resolve, all green against a live node on 2026-08-22.

Blueprint §3 assumes a registry that can hold a work definition, hand back a proof that the definition really is in the log, and keep old versions resolvable forever. This spike establishes that DeDi does all three, and records the places where it does them differently than assumed.

## Reproduce it

```bash
git clone git@github.com:theflywheel/DeDi-node.git ../DeDi-node

# The node image is published (flywheelai/dedi-node) and compose pins it, so
# `make dedi-image` is only for running an unreleased build from a checkout.
make dedi-keys DEDI_SRC=../DeDi-node     # node key + CREST publisher key
# put the printed DEDI_PUBLISHER_KEYS line into infra/compose/.env

make substrate-up
DEDI_VERIFIER_KEY="<the verifier key dedi-keys printed>" make spike-dedi
```

The script is `tools/spikes/dedi-spike.sh`. It is a spike, not a test: it lives outside the harness because it proves something about somebody else's software, and it will be retired once #20 wraps DeDi behind CREST's own registry interface.

## What the spike establishes

**A record carries a real Merkle inclusion proof.** Leaf, audit path, and a signed checkpoint in the Go checksum-database note format:

```
crest.spike.local/log
3
jd7jg5SRE+SECaRUa3Li8Zo8qK7gLf+B6vkaQjbgbVg=

— dedi.local 2M2Rtyxr/h7xsVGZ…
```

**The proof validates independently.** `tools/spikes/dediproof` is a second implementation, written from the wire format rather than by importing DeDi's code — asking DeDi to check its own proof would only establish that DeDi agrees with itself. It recomputes the leaf hash from the leaf fields, walks the path under RFC 6962 domain separation, and verifies the checkpoint's Ed25519 signature. Standard library only, because CREST's own verification path (§7) has to run where there is no dependency tree.

It was tested against tampering, not just against the happy path. All four of these are rejected:

| Tampered | Caught by |
|---|---|
| Leaf digest altered | digest no longer matches the record |
| `created_by` rewritten | recomputed root ≠ signed root |
| One audit-path step flipped | recomputed root ≠ signed root |
| Correct proof, wrong verifier key | no signature verifies |

That last row is the one that matters. A checker that validates the path but not the signature proves nothing, because anyone can build a consistent tree.

**Versions are immutable and both resolve.** Publishing v1.1 does not disturb v1.0: `version_id` pins the old version, and — the property that actually counts — **the inclusion proof issued against v1 still validates after v2 exists**. A credential issued under a definition has to stay verifiable after the definition moves on, or W6 breaks silently.

**Writes are authenticated per namespace.** Ed25519 publisher signatures over method + path + body + precondition, with the key scoped to a namespace. Compare-and-swap via `If-Match: <digest>-<state>` means two concurrent editors cannot both win.

## Findings for the memo (#4)

**1. ~~There is no published DeDi image.~~ Withdrawn — I was wrong.** It is published as `flywheelai/dedi-node`; I checked one registry, found nothing, and generalised. Compose now pins the published image.

**2. DeDi wants its own Postgres.** Its compose runs a separate database, and it should stay that way: the node owns a transparency log whose checkpoints must never roll back, and sharing a database with CREST would put registry history within reach of a CREST migration. Corrected in `infra/compose/docker-compose.yml`. *Reinforces §3, does not contradict it.*

**3. An unrecognised query parameter is ignored, not rejected.** `?versionId=` (camel case) returns the **latest** version with a perfectly valid proof attached; the parameter is `version_id`. A verifier that misspells the pin resolves the wrong definition and has no way to notice. **This is the finding with teeth** — CREST's own resolution path must reject unknown parameters rather than silently answer a different question, and #20 should treat that as a requirement on the registry interface.

**4. The version tag has to be reassembled by the client.** `If-Match` wants `<digest>-<state>`, but the lookup response returns `digest` and `state` as separate fields with no combined tag. Every client re-derives a format only the server should own. Minor, but exactly the kind of thing that drifts between implementations.

**5. DeDi-node requires Go 1.25; the CREST module is on 1.24.** Harmless today — separate modules, and CREST consumes DeDi over HTTP — but it forecloses vendoring, which is the correct outcome anyway.

Nothing here contradicts Blueprint §3. Finding 3 adds a requirement to it rather than correcting it.

## Not covered by this spike

- **Witness rings and consistency proofs.** The node exposes `/dedi/log/proof/consistency` and a witness surface; this spike checked neither. Inclusion proves a record is in *a* log — consistency proves the log was never rewritten, which is the property an independent verifier actually depends on. It belongs with #20.
- **The private half of §3.** Only public work-definition faces were published. The rule that no PII reaches the DeDi node is asserted in #20, not here.
- **Anything at scale.** Four leaves.
