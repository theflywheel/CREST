# Mimoto PDF rendering patch

This directory contains a reviewed source patch against the exact Mimoto source
used by the local wallet image:

- repository: `https://github.com/mosip/mimoto`
- tag: `v0.20.0`
- commit: `f426da315047f55daca085ae92285551667707c8`
- image: `mosipid/mimoto:0.20.0`

The patch changes only `CredentialPDFGeneratorService.formatValue`. Mimoto's
original mapper treated every map as an outcome wrapper and read only its
`value` member, which discarded structured `period` and `provenance` claims.
The patched mapper keeps that wrapper behavior, appends an outcome `unit`, and
renders other maps/lists as labeled human-readable text. Rendering happens
before the existing HTML template, which remains responsible for escaping.
The QR path and the object passed to PixelPass are unchanged.

Rebased from 0.19.2 to 0.20.0 on 2026-09-10 (#230): `formatValue` is unchanged
between the two, so the hunks moved by line offset only. 0.20.0 is the Mimoto
Inji Web 0.15.0 ships with upstream, and the first with the wallet-side
OpenID4VP surface (`/wallets/{id}/presentations`, `/wallets/{id}/verifiers`).
