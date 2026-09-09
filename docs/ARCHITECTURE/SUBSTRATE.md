---
title: The substrate
---

# The substrate: run, not built

| Component | Role | Boundary it holds |
|---|---|---|
| **Postgres** | One instance, one schema per member of core, one for payments | The private store: everything with personal data or policy state |
| **Object store** | Consent recordings and other artefacts | Bytes the registry must never hold |
| **Vault** | The issuer's signing seed | A credential is signed by a key no service file contains |
| **DeDi node** | The public registries: instance, organisations, definitions, adapters, terms | Tamper-evident, witnessed public facts. Never a name, a number or a consent |
| **eSignet** | Login for every door | CREST sees a pairwise subject, never a credential of the person's |
| **Inji Certify · Verify · Web** | Wallet-format issuance, native verification, guest download | A credential that verifies outside CREST |

## The local stand-ins

A local stack runs with mocks where a real provider is absent. They stand in; they are never shipped as product.

| Mock | Stands in for | Retires when |
|---|---|---|
| `mock-oidc` | The identity provider; mints real ES256 tokens against a real JWKS. **Local and e2e only** since 2026-09-09 (#155 phase 4) — the deployed fleet trusts eSignet alone and runs no mock issuer | eSignet is the only door everywhere, not just on the fleet |
| `mock-rail` | The payment rail | A real rail connector lands |
| `mock-notify` | The notification inbox | A delivery channel exists |
| the payments `simulator` provider | Settlement | Refuses to run outside a local environment |

## Bringing it up

```sh
make e2e-up        # Postgres, object store, mocks, core and payments
make apps-up       # the same, plus the four doors, seeded with the fixture world and a story week
make substrate-up  # the full substrate profile: DeDi, eSignet, Inji
```

The compose file is `infra/compose/docker-compose.yml`; the production shape, and what differs about it, is ../DEPLOYMENT.md (`docs/DEPLOYMENT.md` in the repository). The run-versus-build reasoning for each component is ../COMPONENTS.md (`docs/COMPONENTS.md` in the repository).
