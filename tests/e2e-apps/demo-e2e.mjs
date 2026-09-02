// "Anyone can walk in the front door" — the production demo, no seeding on
// any path shown. Chapters: signup, org onboarding, field enrolment+evidence,
// worker confirm → wallet → Inji, verifier.
import { chromium } from "playwright";
import { mkdirSync } from "fs";

const SC = process.env.SC;
const VID = `${SC}/vid2`;
mkdirSync(VID, { recursive: true });

const W = "https://crest-worker-production.up.railway.app";
const F = "https://crest-field-production.up.railway.app";
const C = "https://crest-console-production.up.railway.app";
const V = "https://crest-verifier-production.up.railway.app";
const MOCK = "https://crest-mock-identity-production.up.railway.app";

const STAMP = Date.now().toString().slice(-6);
const NAT_ID = process.env.NAT_ID || "93" + STAMP + "55"; // fresh synthetic national id
const PIN = "424242";
const PHONE = "+2547" + STAMP + "9";
const NAME = "Zawadi Demo";

const CAPTION_CSS = `
#crest-cap{position:fixed;left:0;right:0;bottom:0;z-index:99999;
background:rgba(11,75,102,.94);color:#fff;font:600 17px/1.45 system-ui;
padding:14px 26px;letter-spacing:.01em;border-top:3px solid #C84C0E;
transition:opacity .3s}
#crest-cap small{display:block;font-weight:400;opacity:.85;font-size:13px}`;

async function chapter(browser, name, fn) {
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    recordVideo: { dir: VID, size: { width: 1440, height: 900 } },
  });
  const p = await ctx.newPage();
  p.setDefaultTimeout(25000);
  const cap = async (title, sub = "") => {
    await p.evaluate(([css, t, s]) => {
      if (!document.getElementById("crest-cap-style")) {
        const st = document.createElement("style");
        st.id = "crest-cap-style"; st.textContent = css;
        document.head.appendChild(st);
      }
      let el = document.getElementById("crest-cap");
      if (!el) { el = document.createElement("div"); el.id = "crest-cap"; document.body.appendChild(el); }
      el.innerHTML = t + (s ? `<small>${s}</small>` : "");
    }, [CAPTION_CSS, title, sub]).catch(() => {});
  };
  let ok = true;
  try { await fn(p, cap); console.log(`chapter ok: ${name}`); }
  catch (e) { ok = false; console.log(`chapter FAILED: ${name}: ${e.message.slice(0, 300)}`); await p.screenshot({ path: `${SC}/fail2-${name}.png` }).catch(() => {}); }
  await ctx.close();
  const v = await p.video().path().catch(() => null);
  console.log(`video: ${name} -> ${v}`);
  return ok ? v : null;
}
const pause = (p, ms) => p.waitForTimeout(ms);

async function esignetLogin(p, cap, natId, pin) {
  await p.getByText("Login with PIN").first().click();
  await pause(p, 1200);
  await p.locator("input:visible").nth(0).fill(natId);
  await pause(p, 500);
  await p.locator("input:visible").nth(1).fill(pin);
  await pause(p, 800);
  await p.getByText(/^Login$/i).first().click();
  await pause(p, 3000);
  const allow = p.getByText(/Allow/i).first();
  if (await allow.isVisible().catch(() => false)) await allow.click();
}

// A fresh identity in eSignet's test registry — the API anyone would use.
if (!process.env.DEMO_PARTY) {
const res = await fetch(`${MOCK}/v1/mock-identity-system/identity`, {
  method: "POST", headers: { "Content-Type": "application/json" },
  body: JSON.stringify({
    requestTime: new Date().toISOString().replace(/\.\d+Z/, ".000Z"),
    request: {
      individualId: NAT_ID, pin: PIN,
      fullName: [{ language: "eng", value: NAME }],
      givenName: [{ language: "eng", value: "Zawadi" }],
      familyName: [{ language: "eng", value: "Demo" }],
      middleName: [{ language: "eng", value: "M" }],
      nickName: [{ language: "eng", value: "Zee" }],
      preferredUsername: [{ language: "eng", value: "zawadi" }],
      preferredLang: "eng",
      gender: [{ language: "eng", value: "Female" }],
      dateOfBirth: "1992/02/02",
      streetAddress: [{ language: "eng", value: "1 Demo Road" }],
      locality: [{ language: "eng", value: "Demoville" }],
      region: [{ language: "eng", value: "Demo Region" }],
      postalCode: "560001",
      country: [{ language: "eng", value: "IND" }],
      phone: "+919999999998", email: "zawadi@example.invalid",
      encodedPhoto: "data:image/jpeg;base64,/9j/4AAQSkZJRg==",
    },
  }),
});
console.log("identity create:", res.status, NAT_ID, "phone", PHONE);
}

const b = await chromium.launch();
const videos = [];
let demoParty = process.env.DEMO_PARTY || "";
const RESUME = !!process.env.DEMO_PARTY;

// ── 1. A stranger signs up ─────────────────────────────────────────────
if (!RESUME) videos.push(await chapter(b, "1-signup", async (p, cap) => {
  await p.goto(W + "/");
  await cap("1 · A stranger arrives at the worker door",
    `A brand-new identity (${NAT_ID}) created moments ago in eSignet's test registry — no seeding, no fixtures.`);
  await pause(p, 3000);
  await p.click("#login-esignet");
  await p.waitForLoadState("domcontentloaded");
  await pause(p, 2500);
  await cap("Authentication is eSignet's, not CREST's", "PKCE + private_key_jwt. CREST never sees the national ID.");
  await esignetLogin(p, cap, NAT_ID, PIN);
  await p.waitForSelector("#signupform", { timeout: 30000 });
  await cap("Signed in — and invited to create their own record",
    "Phase B: the record belongs to the worker from minute one. What is stored: a display name, a phone, a pairwise reference. Nothing else.");
  await pause(p, 3000);
  await p.fill("#signupform input >> nth=0", NAME);
  await pause(p, 600);
  await p.fill("#signupform input >> nth=1", PHONE);
  await pause(p, 1000);
  await p.click("#signupform button.btn");
  await p.waitForURL(/#\/home/, { timeout: 20000 });
  demoParty = await p.evaluate(() => JSON.parse(sessionStorage.getItem("crest.worker.session") || "{}").me || "");
  console.log("demo party:", demoParty);
  await cap("Enrolled — by themselves", "A real party in the registry, bound to the identity they hold. No work yet; that comes from a programme.");
  await pause(p, 4000);
}));

// ── 2. An organisation onboards itself ──────────────────────────────────
if (!RESUME) videos.push(await chapter(b, "2-onboard", async (p, cap) => {
  await p.goto(C + "/#/onboard");
  await cap("2 · An organisation walks in — the console's onboarding",
    "Apply, read the published terms, accept a specific version. No seeded party anywhere on this path.");
  await pause(p, 3000);
  await p.fill("#orgapplyform input >> nth=0", "Lakeside Health Trust");
  await pause(p, 600);
  await p.fill("#orgapplyform input >> nth=1", "programmes@lakeside.example");
  await pause(p, 900);
  await p.click("#orgapplyform button.btn");
  await p.waitForSelector("#acceptterms", { timeout: 20000 });
  await cap("The terms, by exact version",
    "Acceptance names a published version — the fact verifiers walk back to. Approval policy is deployment configuration.");
  await pause(p, 3500);
  await p.click("#acceptterms");
  await p.waitForURL(/#\/onboard\/status/, { timeout: 20000 });
  await pause(p, 1500);
  await cap("APPROVED — with the decider on record",
    "Approved by policy, in the same transaction as the acceptance. The organisation is now published to the registry log.");
  await pause(p, 4500);
}));

// ── 3. The programme enrols the worker and submits evidence ────────────
if (!RESUME) videos.push(await chapter(b, "3-field", async (p, cap) => {
  await p.goto(F + "/");
  await cap("3 · The field door — the programme's side",
    "A supervisor registers workers and submits the evidence of work done.");
  await pause(p, 2500);
  await p.click("[data-login]");
  await p.waitForURL(/registrations/, { timeout: 20000 });
  await pause(p, 2000);
  await p.goto(F + "/#/roster");
  await cap("A day's roster becomes evidence",
    `One CSV row names the new worker by the phone they signed up with (${PHONE}) — the batch becomes a unit and a claim.`);
  await pause(p, 3000);
  const csv = `activity,outcome_value,outcome_unit,worker_id_kind,worker_id,period_start,period_end,geography,household_id,beneficiary_count,source_record_ref\nbednet-distribution,7,bednets-distributed,phone,${PHONE},2026-09-01,2026-09-01,Riverside,HH-${STAMP},7,demo-${STAMP}`;
  await p.setInputFiles('input[type="file"]', { name: "roster.csv", mimeType: "text/csv", buffer: Buffer.from(csv) });
  await pause(p, 1200);
  await p.locator("form button.btn, form .btn").first().click();
  await pause(p, 4000);
  await cap("Submitted — a claim now awaits the worker's word",
    "The confirmation window opens; nothing pays until it exits, and every exit releases payment.");
  await pause(p, 3500);
}));

// ── 4. The worker confirms, and the wallet fills ───────────────────────
videos.push(await chapter(b, "4-confirm-wallet", async (p, cap) => {
  await p.goto(W + "/");
  await p.click("#login-esignet");
  await p.waitForLoadState("domcontentloaded");
  await esignetLogin(p, cap, NAT_ID, PIN);
  await p.waitForURL(/#\/(home|auth)/, { timeout: 30000 });
  await pause(p, 2000);
  await cap("4 · The worker returns — same identity, their own record now",
    "The work appears; confirming is having your say, and it is what releases the money.");
  await p.goto(W + "/#/work");
  await pause(p, 3000);
  await p.locator("[data-confirm]").first().click({ timeout: 20000 });
  await pause(p, 2500);
  await cap("Confirmed — a credential is signed at the exit", "The record and the payment move together.");
  // fail loudly if the credential did not issue
  for (let i = 0; ; i++) {
    const r = await fetch(`${V}/api/crest-verification/v1/parties/${encodeURIComponent(demoParty)}/credentials`);
    const j = await r.json().catch(() => ({}));
    if ((j.count || 0) > 0) break;
    if (i > 20) throw new Error("no credential issued after confirm");
    await new Promise((res) => setTimeout(res, 3000));
  }
  await pause(p, 2000);
  await p.goto(W + "/#/wallet");
  await pause(p, 3000);
  await cap("The wallet view — and custody in the worker's own Inji wallet",
    "The newest confirmed work event is issuable straight into Inji Web via OpenID4VCI, signed by this deployment's Certify.");
  await pause(p, 4500);
}));

// ── 5. The verifier ─────────────────────────────────────────────────────
videos.push(await chapter(b, "5-verify", async (p, cap) => {
  await p.goto(V + "/#/v1_2");
  await cap("5 · A stranger checks the record",
    "No account, no permission from CREST — the answer rides the signature.");
  await pause(p, 2500);
  // borrow the worker's newest credential by party id? The verifier's
  // load-sample needs a party id; use the demo worker's via the trail…
  // keep the chapter to the sample flow on the seeded worker for reliability.
  await p.fill("#verifyform input >> nth=0", demoParty);
  await p.click("#loadsample");
  await pause(p, 2500);
  await p.locator("#verifyform button.btn").last().click();
  await p.waitForURL(/v1_3/, { timeout: 20000 });
  await cap("Verified — facts and provenance, nothing identifying",
    "Tier derived at this moment, never stored. The same credential also verifies in Inji Verify — a different stack's verifier.");
  await pause(p, 5000);
}));

console.log(JSON.stringify(videos));
await b.close();
