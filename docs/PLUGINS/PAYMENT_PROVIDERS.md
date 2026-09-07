---
title: Payment providers
---

# Payment providers

The payments application submits one transfer at a time through a provider. The mechanism's activation gate (test disbursement, reconciliation agreed, statement agreed, batching recorded, qualification verified) is the same whichever provider is behind it.

## The interface

```go
type Provider interface {
    Submit(context.Context, Request) (Response, error)
}
```

A `Request` carries an idempotency key, the instruction id, the amount in minor units, the currency and the destination. A `Response` echoes the key and the id it answers, and reports the transfer's status and settled amount. Both are validated: a request without its required fields is refused before any call, and a response that answers a different key or instruction is rejected.

A non-nil error is transport or HTTP failure. **It can never mean confirmed.** The instruction stays where it was, and the retry carries the same idempotency key.

## Shipped providers

| Name | What it does | When |
|---|---|---|
| `http` | Posts the request to `PAYMENT_PROVIDER_URL`, the rail connector a deployment runs | Production |
| `simulator` | Settles in the database on `POST /v1/providers/simulator/settle`, so a local stack can show money moving | Development only; refuses to run outside a local environment |

`mock-rail` in the local compose stack is what the `http` provider talks to when no real rail is configured.

## Adding one

1. Implement `Provider` in `services/payments/providers/`.
2. Register a factory in `providers.NewCatalogue()` under a name.
3. Name it in `PAYMENT_PROVIDER`; pass its settings through `providers.Config`.
4. Prove the request and response validation with the provider's own tests, following `provider_test.go` and `simulator_test.go`.
5. Add the manifest row.

## What the provider does not own

Pricing (the rate in force when the work happened), holds and their owners, reconciliation and statements are the application's. A provider that reprices, holds silently or reports settlement it cannot prove is a provider that must not be registered.
