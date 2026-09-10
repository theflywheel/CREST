# Upstream report (draft): Certify advertises `credential_issuer` as the bare host but serves the metadata under the servlet path

**Project:** mosip/inji-certify · **Observed on:** 0.14.0 (also 0.13.1) · **Filed from:** CREST (theflywheel/CREST#203, P0 finding C11/C12)

## Summary

Inji Certify's OpenID4VCI credential-issuer metadata sets `credential_issuer` to the deployment's bare host, but the metadata document itself is served under the servlet path `/v1/certify`. OpenID4VCI (draft 13, §11.2.2) says a client discovers the metadata by appending `/.well-known/openid-credential-issuer` to the credential issuer identifier. Doing so returns 404.

## Reproduce

```
$ curl -s https://<certify-host>/v1/certify/.well-known/openid-credential-issuer | jq .credential_issuer
"https://<certify-host>"
$ curl -o /dev/null -w '%{http_code}\n' https://<certify-host>/.well-known/openid-credential-issuer
404
```

## Effect

Any OpenID4VCI wallet other than one told the servlet path out of band fails to discover the issuer. Inji's own Mimoto only works because it accepts a `credential_issuer_host` config field carrying the servlet path — a non-standard escape hatch a third-party wallet does not have. The failure surfaces in Mimoto as `Api not accessible failure`, naming neither the URL nor the mismatch.

## Expected

Either serve `/.well-known/openid-credential-issuer` at the advertised `credential_issuer` host, or advertise `credential_issuer` including the servlet path so appending the well-known suffix resolves. The two must be consistent.

## Workaround in use

We front Certify with a reverse proxy that serves the metadata at the advertised issuer (the way MOSIP already fronts eSignet with its UI image). This should not be necessary for a spec-conformant deployment.
