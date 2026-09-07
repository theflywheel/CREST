---
title: The confirmation window and its four exits
---

# The confirmation window and its four exits

**J7 · W-1, J8 · W-4.** A window opens on every claim and the worker is told. It closes one of four ways: the worker confirms, the worker disputes, the clock auto-confirms at the window's end, or a supervisor confirms for a worker who could not be reached. Every one of the four releases the payment obligation; a dispute contests the record, it never withholds the money. The window's length is programme policy, not infrastructure.

```mermaid
sequenceDiagram
  autonumber
  participant At as core · attestation
  participant W as Worker
  participant Sup as Supervisor
  participant Clk as Clock
  participant Vf as core · verification
  participant Pay as crest-payments
  At->>W: window opened (acknowledgement token)
  W->>At: POST /v1/windows/{id}/ack
  alt confirm
    W->>At: POST /v1/claims/{id}/confirm
  else dispute
    W->>At: POST /v1/claims/{id}/dispute
    At->>At: open a contest — the record is challenged, not erased
  else auto-confirm
    Clk->>At: POST /v1/sweep (windows past their end)
  else assisted
    Sup->>At: POST /v1/claims/{id}/assist (acting for the worker, by grant)
  end
  At->>At: record the exit route and the authoritative time
  At->>Vf: issue the credential for the accepted claim
  At->>Pay: release the obligation (outbox, at-least-once)
  Pay->>Pay: price by the rate in force when the work happened
  Pay->>Pay: HELD with an owned reason, or RELEASED to the rail
```

## What holds it

- All four exits are proven to release, the dispute hardest of all: `TestADisputeStillReleasesPayment`.
- A worker who was never reached is not auto-confirmed against; the unreached list is the supervisor's assisted route.
- The exit's time, not the receipt's, is what the credential signs.
- A release that reaches payments while the mechanism is not live is held as `mechanism_not_live`, owned by the mechanism owner, and released by activation at the rate in force.

## Recordings

- [The worker confirms her record](../assets/clean-slate-watch/J7-j7-worker-confirms-her-record.mp4) (39 s)
- [Assisted confirmation and the handoff](../assets/clean-slate-watch/J8-j8-assisted-confirmation-and-handoff.mp4) (17 s)

Screens: w1_7–w1_12, w4_2–w4_4. Next: [CREDENTIALS_AND_VERIFICATION.md](CREDENTIALS_AND_VERIFICATION.md).

