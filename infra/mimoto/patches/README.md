# Mimoto PDF rendering patch

This directory contains a reviewed source patch against the exact Mimoto source
used by the local wallet image:

- repository: `https://github.com/mosip/mimoto`
- tag: `v0.19.2`
- commit: `e1fedbb9dce6a8c3dd9b5b8e75423d636a0704cf`
- image: `mosipid/mimoto:0.19.2`

The patch changes only `CredentialPDFGeneratorService.formatValue`. Mimoto's
original mapper treated every map as an outcome wrapper and read only its
`value` member, which discarded structured `period` and `provenance` claims.
The patched mapper keeps that wrapper behavior, appends an outcome `unit`, and
renders other maps/lists as labeled human-readable text. Rendering happens
before the existing HTML template, which remains responsible for escaping.
The QR path and the object passed to PixelPass are unchanged.
