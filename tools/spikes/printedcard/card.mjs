// Renders a printed card from a real issued credential, and decodes it back.
//
// #66's second leg. PixelPass is the format Blueprint §5 names for the printed
// path: the QR carries the *full signed VC*, so a bare scan verifies signature
// and schema with no network at all. That is what makes a worker's record
// provable to a stranger without CREST, and it is the reason the payload is not
// a URL or a lookup key — either of those would put a server between a worker
// and their own history.
//
// Usage: node card.mjs <credential.json> <outdir>
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import pkg from '@mosip/pixelpass'

const { generateQRData, generateQRCode, decode } = pkg

const [, , credPath, outDir] = process.argv
if (!credPath || !outDir) {
  console.error('usage: node card.mjs <credential.json> <outdir>')
  process.exit(2)
}

const credential = JSON.parse(readFileSync(credPath, 'utf8'))
const flat = JSON.stringify(credential)

// PixelPass compresses (zlib) and base45-encodes. No header is passed: an
// empty header is what Inji's own wallet writes, and a card only this repo can
// read would prove nothing about interoperability.
const payload = generateQRData(flat)
writeFileSync(join(outDir, 'card.qr.txt'), payload)

const png = await generateQRCode(flat)
writeFileSync(join(outDir, 'card.png'), Buffer.from(png.split(',')[1], 'base64'))

// The round trip is the claim worth checking here: a card that renders but
// decodes to something else is a card that fails in a worker's hands, in a
// place where nobody can debug it.
const back = decode(payload)
if (back !== flat) {
  console.error('[FAIL] the card does not decode back to the credential that was printed')
  process.exit(1)
}
writeFileSync(join(outDir, 'decoded.json'), back)

const subject = credential.credentialSubject ?? {}
const html = `<!doctype html><meta charset="utf-8"><title>CREST work record</title>
<style>
 body{font:14px/1.5 system-ui,sans-serif;margin:0;display:flex;justify-content:center;padding:24px}
 .card{width:340px;border:1px solid #222;border-radius:10px;padding:18px}
 h1{font-size:15px;margin:0 0 2px} .sub{color:#555;font-size:12px;margin:0 0 14px}
 img{width:100%;image-rendering:pixelated;border:1px solid #eee}
 dl{display:grid;grid-template-columns:auto 1fr;gap:2px 10px;font-size:12px;margin:12px 0 0}
 dt{color:#555} dd{margin:0}
 .foot{margin-top:12px;font-size:10px;color:#666;border-top:1px solid #eee;padding-top:8px}
 @media print{body{padding:0}}
</style>
<div class="card">
  <h1>${escapeHtml(String(subject.activity ?? 'work record'))}</h1>
  <p class="sub">${escapeHtml(String(subject.issuerOrg ?? ''))}</p>
  <img alt="PixelPass QR carrying the full signed credential" src="data:image/png;base64,${png.split(',')[1]}">
  <dl>
    <dt>outcome</dt><dd>${escapeHtml(String(subject.outcome?.value ?? ''))} ${escapeHtml(String(subject.outcome?.unit ?? ''))}</dd>
    <dt>period</dt><dd>${escapeHtml(String(subject.period?.start ?? ''))}</dd>
    <dt>definition</dt><dd>${escapeHtml(String(subject.definition?.ref ?? ''))} v${escapeHtml(String(subject.definition?.version ?? ''))}</dd>
  </dl>
  <p class="foot">Scanning this code reads the whole signed record. It carries no trust tier and no
  identity number: strength is worked out by whoever checks it, from the facts inside.</p>
</div>`
writeFileSync(join(outDir, 'card.html'), html)

function escapeHtml (s) {
  return s.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]))
}

console.log(`[PASS] card rendered and decodes back byte-identical (${payload.length} chars of QR payload)`)
