---
title: Identity providers
---

# Identity providers

Every door signs in through an OIDC issuer. The services verify the token against that issuer's JWKS and resolve its pairwise subject to a party. CREST never sees a credential of the person's and never keeps a raw national identifier.

## The interfaces

```go
type TokenVerifier interface {
    Verify(ctx context.Context, token string) (Caller, error)
}

type Binder interface {
    PartyForSubject(ctx context.Context, subject string) (string, error)
}
```

A `Caller` carries the issuer, the pairwise subject, and the party it resolves to, if any; the middleware attaches it to every request. `Binder` is answered by the parties member from its own table and by every other member over an internal call, and neither should know which.

## Provider classes

Four are designed for (Blueprint §4.1), integrated once per class and configured per deployment:

| Class | Where it fits | Today |
|---|---|---|
| **eSignet** | MOSIP's OIDC broker; the preferred front door wherever a MOSIP or eSignet-fronted national ID exists | Runs on the production fleet and in the substrate profile locally |
| **MOSIP IDA** | Direct authentication where eSignet is not deployed | Designed, not built |
| **Generic OIDC / eKYC** | Non-MOSIP national systems and portals | Runs; the local `mock-oidc` issuer mints real ES256 tokens against a real JWKS |
| **Mobile OTP** | The floor: proves control of a contact route, not legal identity | Designed, not built |

## Several at once

`MultiVerifier` accepts tokens from more than one issuer, each with its own JWKS, so a deployment can run eSignet for the doors while an earlier issuer's tokens still verify during a migration. Nothing about any one issuer's checking is relaxed.

## Binding

- **Late and monotonic.** A party starts unbound or at a low assurance. An anchor attached later upgrades the reading of credentials already issued, with no reissuance, because assurance is derived, not stored. Re-binding appends a new assertion record; history is never rewritten.
- **A claimed party id is never a binding credential.** A self-bind is accepted only for a never-bound party or the exact subject already bound; an invitation is claimed with the invitee's own login.
- **Setup subject.** The instance's first operator is the subject the deployment was configured with (`CREST_SETUP_SUBJECT_REF`, `CREST_SETUP_ISSUER`); nobody else can stand the instance up.

## Configuration

| Setting | Meaning |
|---|---|
| `CREST_OIDC_ISSUER`, `CREST_OIDC_JWKS_URL` | The issuer the services verify against |
| the eSignet client settings (in Vault on the fleet) | The doors' login |
| `CREST_SUBJECT_SALT` | The salt behind the pairwise derivation and the identifier hash; per deployment, never shared |

Switching providers is adding the new one beside the old, letting workers re-bind at their next login, and retiring the old issuer once nobody presents its tokens.
