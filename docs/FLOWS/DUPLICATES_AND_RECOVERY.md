---
title: Duplicates hold, and recovery of a lost login
---

# Duplicates hold, and recovery of a lost login

## Duplicates hold, and never merge by themselves

**G-4.** A second registration on an identifier that already resolves does not merge: the service answers 409 with a hold. The worker confirms it is them from their own login, and only then does the custodian decide. A merge without both is a monitored metric that must read zero.

```mermaid
sequenceDiagram
  autonumber
  participant Ag as Agent
  participant P as core · parties
  participant W as Worker
  participant C as Custodian
  Ag->>P: register on a phone that resolves to somebody
  P-->>Ag: 409 hold {candidates}
  W->>P: POST /v1/holds/{id}/confirm (their own login)
  C->>P: POST /v1/holds/{id}/resolve
  P->>P: merge the chain, or keep them apart — existence, never content
```

A verifier resolving the survivor sees the whole chain's credentials and never a word about a merge: the residue is stated in the blueprint's §16 rather than hidden.

## Recovery of a lost login

**W-1, W-5.** A worker nominates recovery contacts in advance. When a login is lost a recovery is opened; each nominated contact answers yes or no from their own login, and a refusal is owned by whoever refused. Two authorities confirming complete it; an override by the custodian is flagged for review, never silent.

```mermaid
sequenceDiagram
  autonumber
  participant W as Worker
  participant P as core · parties
  participant N1 as Contact 1
  participant N2 as Contact 2
  participant C as Custodian
  W->>P: POST /v1/parties/{id}/recovery-contacts (in advance)
  W->>P: POST /v1/recoveries (login lost)
  N1->>P: GET /v1/recoveries?confirmerPartyId=me
  N1->>P: POST /v1/recoveries/{id}/confirmations
  N2->>P: POST /v1/recoveries/{id}/refusals (owned)
  C->>P: POST /v1/recoveries/{id}/complete · or …/override → flagged, reviewed by a date
```

## What holds it

- `merges_without_confirmation = 0` is read on the custodian's duplicates screen.
- A nominated contact can read the recovery they vouch for and nothing else; the audit list is the custodian's.
- Nominations are revocable; a revoked contact's answer no longer counts.

## Recording

- [The registry dashboards: coverage, quality, duplicates, reuse, unclear rows](../assets/clean-slate-watch/J11-j11-registry-dashboards.mp4) (31 s)

Screens: g4_4–g4_7, w1_7, w4_1–w4_3. Next: [DASHBOARDS.md](DASHBOARDS.md).
