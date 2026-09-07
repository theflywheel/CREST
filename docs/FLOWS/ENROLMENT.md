---
title: A worker is enrolled
---

# A worker is enrolled, by an agent or by herself

**J6 · W-2, J7 · W-1.** Two pathways, neither a fallback: an agent registers the worker in the field and records consent, or the worker registers herself from the project's join link. Both issue the same party and the same enrolment consent; a verifier can never tell which was used. Consent is captured at enrolment, per programme, before any evidence about the worker may be recorded.

```mermaid
sequenceDiagram
  autonumber
  participant Ag as Agent
  participant F as Enrolment door
  participant W as Worker
  participant Wd as Worker door
  participant P as core · parties
  participant Obj as Object store
  alt assisted (J6)
    Ag->>F: register the worker (offline queue if no signal)
    F->>P: POST /v1/enrolments (Idempotency-Key)
    Ag->>F: record consent: voice, or assisted
    F->>P: POST /v1/parties/{id}/consents?moment=enrolment&contextId=…
    P->>Obj: store the recording
  else self (J7)
    W->>Wd: opens #/join/<context>, signs in
    Wd->>P: consent first, then POST /v1/enrolments
  end
  P->>P: pairwise subject + salted hash; never the raw identifier
  P-->>F: the party, with the enrolment method recorded
```

## What holds it

- The enrolment record carries the method (assisted, self, confidence check), never a tier.
- A voice consent needs a real recording; the bytes go to the object store, the row keeps the key.
- A second registration on an identifier that already resolves does not merge; it holds ([DUPLICATES_AND_RECOVERY.md](DUPLICATES_AND_RECOVERY.md)).
- The field door's offline queue is encrypted and survives an upgrade; nothing registered offline is lost.

## Recordings

- [The agent registers a worker](../assets/clean-slate-watch/J6-j6-agent-registers-a-worker.mp4) (24 s)
- [The worker registers herself](../assets/clean-slate-watch/J7-j7-worker-registers-herself.mp4) (34 s)

Screens: w1_1–w1_20, w2_1–w2_5. Next: [EVIDENCE_INGESTION.md](EVIDENCE_INGESTION.md).
