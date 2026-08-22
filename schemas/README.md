# Schemas — the source of truth

The eleven primitives (Blueprint §2), the Trusted Payments profile (§2.1) and the
`WorkEventCredential` (§5) live here as versioned JSON Schema.

**Go structs and TypeScript types are generated from these**, never hand-written
alongside them. Two hand-maintained definitions of a primitive is two systems that
agree right up until they don't.

## Layout

```
primitives/                  Party, Terms, Authorization, Context, Definition,
                             Unit, Claim, Credential, Consent, Contest, LinkedRecord
profiles/trusted-payments/   Role bindings and LinkedRecord types for the first use case
credentials/                 WorkEventCredential and its JSON-LD context
```

## Rules

- **Versioned from day one.** A schema change that is not a new version is a
  change that breaks a credential someone already holds.
- **Primitives stay generic.** No payments, health or training vocabulary. If
  expressing a use case needs a field in a primitive, that is a design finding
  (§2), not a quick edit.
- **Releases publish to the DeDi node**, so a verifier can resolve the exact
  schema version a credential was issued against.

Authored in #12 (primitives), #13 (profile), #16 (credential).
