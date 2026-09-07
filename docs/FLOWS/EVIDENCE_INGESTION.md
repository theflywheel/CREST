---
title: Evidence arrives and becomes claims
---

# Evidence arrives and becomes claims

**J6 · W-4.** A registered source sends a batch through its registered adapter. Each row is validated against the definition's contract, matched to a worker, gated on that worker's consent to this programme, deduplicated by the work it describes, and becomes a Unit with a Claim. What nobody could attribute waits in the unclear queue for the custodian; it never becomes somebody's claim by guess.

```mermaid
sequenceDiagram
  autonumber
  participant S as Source system / agent
  participant Ev as core · evidence
  participant Ad as Adapter (registry)
  participant P as core · parties
  participant At as core · attestation
  S->>Ev: POST /v1/batches?contextId&definitionId&definitionVersion&systemRef
  Ev->>Ev: authorise the submitter in the project; the source must be registered
  Ev->>Ad: Parse(payload, source configuration, receivedAt)
  Ad-->>Ev: rows + rejections, provenance stamped by the deployment
  loop each row
    Ev->>Ev: schema · activity · source admitted by the definition · required fields
    Ev->>P: GET /v1/resolve (joining identifier, context)
    P-->>Ev: match + enrolment consent state, or 409 hold
    Ev->>Ev: refuse unless consent is GRANTED; redact; dedupe key
    Ev->>Ev: insert the Unit (or reuse it), insert the Claim
  end
  Ev-->>At: outbox: open a window per claim
  Ev-->>S: batch receipt: accepted, unclear, rejected
```

## What holds it

- Parser selection is the source registration's, never the payload's; provenance comes from the deployment's configuration and whatever the source asserts about itself is ignored.
- The dedupe key is built over the redacted record: the work, the worker's joining identifier as stored, and the source record reference namespaced by its system. Two workers' identical rows are two units.
- A row about someone who has not consented to this programme becomes an unclear row that retains only its rejection receipt, never a claim.
- The batch receipt is what the project reads back (`/v1/batches/{id}/receipt`): what arrived, per row, and where each unclear row sits in the queue.

## Recording

- [The agent closes the roster](../assets/clean-slate-watch/J6-j6-agent-closes-the-roster.mp4) (26 s)

Screens: w4_1–w4_5, p2_20. Next: [CONFIRMATION_WINDOW.md](CONFIRMATION_WINDOW.md).

