# Upstream report (draft): `display[0].logo` is required by the wallet but not documented as required

**Project:** mosip/inji-certify (issuer well-known) and mosip/mimoto (wallet) · **Observed on:** Certify 0.14.0 / Mimoto 0.20.0 · **Filed from:** CREST (theflywheel/CREST#203, P0 finding C15)

## Summary

A credential-issuer well-known whose `credential_configurations_supported[*].display[0]` (or the issuer-level `display[0]`) omits `logo` is rejected by the wallet outright:

```
Invalid Wellknown from Issuer … display[0].logo: must not be null
```

Nothing in Certify's configuration schema or the OpenID4VCI metadata documentation marks `display[].logo` as required — the spec lists `logo` as OPTIONAL. A logo-less issuer is therefore silently unusable, with the failure appearing only at the wallet, not at issuance or metadata publication.

## Expected

Either treat `display[].logo` as optional in the wallet (per the spec), or document it as required in Certify's metadata schema so an issuer configures it deliberately rather than discovering the requirement from a wallet error.

## Note

CREST's issuer config carries a logo, so we are not blocked; this is filed so a third-party issuer is not surprised.
