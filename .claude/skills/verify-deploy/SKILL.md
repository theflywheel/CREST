---
name: verify-deploy
description: Verify a CREST deployment after it lands — service health, one issuance, one verification, one payment instruction. Use after deploying to any environment, or when asked whether an environment is healthy.
---

# Verifying a deployment

A deploy is not done when it completes. It is done when it is verified.

```sh
make verify-deploy ENV=staging
make verify-deploy ENV=production   # read-only checks only
```

## What runs where

**Staging — the full smoke path.** Health of every service; one credential issued end to end; one verification against it, including the offline path; one payment instruction against the sandbox rail; the DeDi node returning a valid inclusion proof; the status list resolving.

**Production — read-only only.** Service health, a status-list fetch, a DeDi checkpoint fetch, and the trust chain walked for an *existing* credential.

Nothing in the production path creates a credential, a claim, or a payment. A smoke test that issues a real credential to a real worker's record is not a smoke test — it is a data-quality incident with a green tick next to it.

## Reading the result

Report per check, not as a single pass/fail: which service, what was asserted, what came back. When something fails, say what still works — "issuance is down, verification and status resolution are healthy" is actionable in a way that "deploy verification failed" is not.

## Before deploying at all

- Migrations reviewed for reversibility. A migration that cannot be rolled back needs to be a deliberate decision, not a discovery.
- Secrets from the environment, never from the repo. If a config value had to be committed to make the deploy work, stop.
- The status list and issuance keys are the two pieces of state whose loss is unrecoverable — a worker's credential history hangs off them. Confirm backup before any change touching key material or the status list.

## When verification fails

1. Do not immediately re-deploy. Find out what broke.
2. `make harness-logs SERVICE=<name>` against the environment.
3. If issuance is affected, check whether anything was **partially** issued — a credential issued without its status list entry is worse than no credential, because it verifies today and cannot be revoked tomorrow.
4. Roll back if the failure touches issuance, payments or identity binding. Everything else can usually be fixed forward.
