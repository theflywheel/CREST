#!/usr/bin/env node
// Screenshot pairs: the reference frame beside the screen we built.
//
// The fidelity gate (tests/e2e-apps/fidelity.spec.js) can prove that every
// word the reference puts on a screen is on ours, and that nothing the
// reference withholds has crept in. It cannot see spacing, weight, colour,
// alignment, rhythm — the visual grammar. This puts the two images side by
// side and gets out of the way.
//
// The reference is rendered in the same browser as the implementation: the
// 17 Aug HTML is opened from disk and each `#scr-<id>` element is shot on its
// own, so the comparison is pixel-for-pixel of the same element the spec was
// extracted from.
//
// Output (gitignored, never committed): docs/.fidelity-sheet/
//   <id>-reference.png, <id>-built.png, index.html
//
// Usage:
//   node tools/journey-trace/contact-sheet.mjs            # everything in scope
//   node tools/journey-trace/contact-sheet.mjs p2_20 p2_21
//   BASE_URL=... node tools/journey-trace/contact-sheet.mjs
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(HERE, "..", "..");

// Playwright is a dependency of the e2e-apps suite, not of tools/ — resolve it
// from there rather than adding a second node_modules tree for one import.
const require = createRequire(path.join(ROOT, "tests", "e2e-apps", "package.json"));
const playwright = await import(pathToFileURL(require.resolve("@playwright/test")).href);
const { chromium } = playwright.default ?? playwright;  // CJS package, imported from ESM
const OUT = path.join(ROOT, "docs", ".fidelity-sheet");
const REFERENCE = path.join(ROOT, "docs", "reference", "CREST — Actor Journeys_17Aug.html");
const BASE = process.env.BASE_URL || "http://localhost:59110";

const read = (p) => JSON.parse(fs.readFileSync(p, "utf8"));
const SPEC = read(path.join(ROOT, "docs", "journey-spec.json"));
const LEDGER = read(path.join(ROOT, "docs", "journey-traceability.json"));
const MAP = read(path.join(ROOT, "tests", "e2e-apps", "fidelity-map.json"));
const STATUS = Object.fromEntries([...LEDGER.rows, ...(LEDGER.designRows || [])].map((r) => [r.id, r]));

const DOORS = { console: "/console/", field: "/enrolment/", worker: "/worker/", verify: "/verify/" };
const only = process.argv.slice(2);
const screens = (only.length ? only : Object.keys(MAP.screens)).sort();

fs.mkdirSync(OUT, { recursive: true });

async function settle(page) {
  await page.waitForLoadState("networkidle").catch(() => {});
  await page.waitForSelector(".screen", { state: "visible" }).catch(() => {});
  await page.waitForTimeout(250);
}

async function shotReference(page, sid) {
  // The walkthrough shows one frame at a time (`.role-steps .role-step` is
  // display:none until `.active`), and the flow sections above it are tabbed
  // the same way. So rather than driving the stepper 143 times, force the one
  // frame and every ancestor of it visible, then shoot the frame element.
  const shown = await page.evaluate((id) => {
    const el = document.getElementById("scr-" + id);
    if (!el) return false;
    for (let n = el; n && n !== document.documentElement; n = n.parentElement) {
      n.hidden = false;
      n.classList.add("active");
      n.style.setProperty(
        "display", n.classList.contains("role-step") ? "flex" : "block", "important");
      n.style.setProperty("visibility", "visible", "important");
      n.style.setProperty("opacity", "1", "important");
      n.style.setProperty("max-height", "none", "important");
    }
    return true;
  }, sid);
  if (!shown) return "";
  const frame = page.locator(`#scr-${sid}`).first();
  const file = path.join(OUT, `${sid}-reference.png`);
  try {
    await frame.screenshot({ path: file, timeout: 15000 });
  } catch {
    return "";  // an unshootable frame is reported as absent, never as a pass
  }
  return path.basename(file);
}

async function shotBuilt(page, sid, entry) {
  await page.goto(BASE + DOORS[entry.app]);
  await settle(page);
  if (entry.arrive.startsWith("console:")) {
    const card = page.locator(`[data-persona="${entry.arrive.slice(8)}"]`);
    if (!(await card.count())) return { file: "", note: "no such persona card" };
    await card.click();
    await settle(page);
    await page.waitForTimeout(1200);
  } else if (entry.arrive === "worker") {
    await page.click("#login-grace").catch(() => {});
    await settle(page);
  } else if (entry.arrive === "field") {
    await page.click("[data-login]").catch(() => {});
    await settle(page);
  }
  if (entry.route) {
    await page.evaluate((h) => { location.hash = h; }, entry.route);
    await settle(page);
  }
  const bar = await page.locator(".appbar").innerText().catch(() => "");
  const note = /not signed in/i.test(bar) ? "could not sign in — see the gate's quarantine" : "";
  const file = path.join(OUT, `${sid}-built.png`);
  await page.screenshot({ path: file, fullPage: true });
  return { file: path.basename(file), note };
}

const rows = [];
const browser = await chromium.launch();
const refPage = await browser.newPage({ viewport: { width: 1400, height: 1000 } });
await refPage.goto(pathToFileURL(REFERENCE).href);
await refPage.waitForTimeout(500);
// Every frame is in the DOM; the walkthrough only hides them behind its own
// stepper, so unhiding the lot lets each be shot without driving the stepper.
await refPage.evaluate(() => {
  for (const el of document.querySelectorAll(".role-step")) {
    el.style.display = "block";
    el.style.visibility = "visible";
    el.hidden = false;
  }
});
const builtPage = await browser.newPage({ viewport: { width: 1400, height: 1000 } });

for (const sid of screens) {
  const entry = MAP.screens[sid];
  if (!entry) { console.log(`skip ${sid}: not in fidelity-map.json`); continue; }
  const led = STATUS[sid] || { status: "designed", note: "" };
  const reference = await shotReference(refPage, sid);
  const built = await shotBuilt(builtPage, sid, entry);
  rows.push({ sid, reference, built: built.file, note: built.note, status: led.status,
              gap: led.note || "", source: (SPEC.screens[sid] || {}).source || "reference",
              surface: `${entry.app} ${entry.route || "/"}` });
  console.log(`${sid}: ${reference ? "reference ✓" : "reference — (our design, no frame)"} · built ✓ ${built.note}`);
}

const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const html = `<!doctype html><meta charset="utf-8"><title>CREST fidelity contact sheet — J3</title>
<style>
 body{font:14px/1.5 ui-sans-serif,system-ui,sans-serif;margin:0;padding:24px 32px;background:#fbfaf9;color:#1a1a1a}
 h1{font-size:22px;margin:0 0 4px} p.lede{margin:0 0 28px;max-width:70ch;color:#505a5f}
 section{margin:0 0 40px;border-top:2px solid #e6e4e1;padding-top:16px}
 h2{font:600 16px/1.3 inherit;margin:0 0 2px} .meta{color:#505a5f;margin:0 0 12px;font-size:12.5px}
 .pair{display:grid;grid-template-columns:1fr 1fr;gap:16px;align-items:start}
 figure{margin:0} figcaption{font:600 11px/1.4 inherit;text-transform:uppercase;letter-spacing:.06em;color:#505a5f;margin:0 0 6px}
 img{max-width:100%;border:1px solid #d6d5d4;background:#fff}
 .none{border:1px dashed #d6d5d4;padding:24px;color:#8a8580;font-size:12.5px;background:#fff}
 .note{color:#c84c0e;font-size:12.5px;margin:6px 0 0}
 code{background:#efedea;padding:1px 4px;border-radius:3px}
</style>
<h1>Fidelity contact sheet — J3</h1>
<p class="lede">Left: the 17 Aug reference frame, shot from
<code>docs/reference/CREST — Actor Journeys_17Aug.html</code>. Right: the same screen as
built, on the local stack. This exists for what assertions cannot judge — spacing, weight,
colour, alignment, rhythm. Screens marked <em>crest-design</em> have no reference frame:
they are our design (<code>docs/design/j3-connective-tissue/README.md</code>).
Regenerate with <code>make fidelity-sheet</code>; nothing here is committed.</p>
${rows.map((r) => `<section>
 <h2>${esc(r.sid)} — ${esc((SPEC.screens[r.sid] || {}).title || "")}</h2>
 <p class="meta">status <strong>${esc(r.status)}</strong> · source ${esc(r.source)} · surface <code>${esc(r.surface)}</code>${r.gap ? ` · ${esc(r.gap)}` : ""}</p>
 <div class="pair">
  <figure><figcaption>Reference</figcaption>${r.reference ? `<img src="${esc(r.reference)}" alt="reference ${esc(r.sid)}">` : `<div class="none">No reference frame — this screen is our own design.</div>`}</figure>
  <figure><figcaption>Built</figcaption>${r.built ? `<img src="${esc(r.built)}" alt="built ${esc(r.sid)}">` : `<div class="none">Nothing rendered.</div>`}${r.note ? `<p class="note">${esc(r.note)}</p>` : ""}</figure>
 </div>
</section>`).join("\n")}
`;
fs.writeFileSync(path.join(OUT, "index.html"), html);
await browser.close();
console.log(`\n${rows.length} pairs → ${path.relative(ROOT, path.join(OUT, "index.html"))}`);
