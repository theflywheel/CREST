// Does MOSIP's own reader read the card CREST prints?
//
// This is the interop direction that matters and the only one a Go test cannot
// check. A printed card is read by somebody else's scanner, so what has to hold
// is that THEIR library gets our credential back intact — not that our encoder
// reproduces their bytes, which it deliberately does not: their library
// CBOR-encodes JSON before compressing, and Go's deflate and pako's make
// different, equally legal choices.
//
// Asserting byte equality was tried first and is the wrong test. This is the
// right one, and it is why `make printed-card` runs node at all.
//
// Usage: node reads-ours.mjs <payload-file> <expected-credential.json>
import { readFileSync } from 'node:fs'
import pkg from '@mosip/pixelpass'

const [, , payloadPath, credentialPath] = process.argv
const payload = readFileSync(payloadPath, 'utf8').trim()
const expected = JSON.parse(readFileSync(credentialPath, 'utf8'))

let decoded
try {
  decoded = JSON.parse(pkg.decode(payload))
} catch (e) {
  console.error(`[FAIL] MOSIP's reader could not read the card CREST printed: ${e.message}`)
  process.exit(1)
}

for (const field of ['issuer', 'proof', 'credentialSubject', 'type']) {
  if (decoded[field] === undefined) {
    console.error(`[FAIL] their reader got the card back without ${field}`)
    process.exit(1)
  }
}
if (JSON.stringify(decoded) !== JSON.stringify(expected)) {
  console.error('[FAIL] the credential came back changed. A signature survives no edit at all,')
  console.error('       so a card that decodes to something subtly different fails in front of a verifier.')
  process.exit(1)
}

console.log(`[PASS] MOSIP's own reader read the card CREST printed (${payload.length} chars)`)
