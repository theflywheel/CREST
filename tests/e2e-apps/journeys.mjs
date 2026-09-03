// Twelve journey recorders — one video per reference journey, walked against
// the LOCAL story-seeded stack (make apps-up). Each journey follows its
// reference screen sequence from docs/reference/"CREST — Actor
// Journeys_17Aug.html" one-to-one where a surface exists; where the manifest
// (docs/journey-traceability.md) says a screen is missing or illustrative,
// the recording shows the on-screen honesty note instead of faking the
// operation.
//
//   OUT=/path node journeys.mjs J7 J6 …      (defaults: all twelve, ./journeys)
//   BASE=http://localhost:59110              (the compose door)
import { chromium } from "playwright";
import { mkdirSync, renameSync } from "fs";

const BASE = process.env.BASE || "http://localhost:59110";
const OUT = process.env.OUT || "journeys";
mkdirSync(OUT, { recursive: true });

const W = BASE + "/worker/";
const F = BASE + "/enrolment/";
const C = BASE + "/console/";
const V = BASE + "/verify/";
const FIXWORKER = "did:crest:party:01JCREST00000000000000WRKA";
const FIXORG = "did:crest:party:01JCREST000000000000000RGN";
const FIXSPVR = "did:crest:party:01JCREST00000000000000SPVR";
const FIXCSTD = "did:crest:party:01JCREST00000000000000CSTD";
const STAMP = Date.now().toString().slice(-6);

// Bearer-authenticated service calls for the acts that are not the recorded
// actor's own — a verifier asking, a custodian opening a recovery. The same
// doors apps.spec.js's walks and the seeder use.
const LOCAL = new URL(BASE).port === "59110";
const SVCBASE = LOCAL
  ? `http://${new URL(BASE).hostname}:59000`
  : BASE.replace(/\/$/, "") + "/api/crest-registry";
const OIDCBASE = LOCAL
  ? `http://${new URL(BASE).hostname}:59103`
  : BASE.replace(/\/$/, "") + "/api/crest-mock-oidc";

async function mintToken(partyId) {
  const r = await fetch(OIDCBASE + "/token", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      sub: "story|" + partyId.replace("did:crest:party:", ""),
      aud: "crest", expiresIn: "1h",
    }),
  });
  const d = await r.json();
  return d.accessToken || d.access_token || d.token;
}

const VERIFBASE = LOCAL
  ? `http://${new URL(BASE).hostname}:59000`
  : BASE.replace(/\/$/, "") + "/api/crest-verification";

async function asParty(partyId, method, path, body, base = SVCBASE) {
  const token = await mintToken(partyId);
  return fetch(base + path, {
    method,
    headers: { Authorization: "Bearer " + token, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

const CAPTION_CSS = `
#crest-cap{position:fixed;left:0;right:0;bottom:0;z-index:99999;
background:rgba(11,75,102,.94);color:#fff;font:600 17px/1.45 system-ui;
padding:14px 26px;letter-spacing:.01em;border-top:3px solid #C84C0E;
transition:opacity .3s}
#crest-cap small{display:block;font-weight:400;opacity:.85;font-size:13px}`;

async function journey(browser, name, fn) {
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    recordVideo: { dir: OUT, size: { width: 1440, height: 900 } },
  });
  const p = await ctx.newPage();
  p.setDefaultTimeout(25000);
  const cap = async (title, sub = "") => {
    await p
      .evaluate(
        ([css, t, s]) => {
          if (!document.getElementById("crest-cap-style")) {
            const st = document.createElement("style");
            st.id = "crest-cap-style";
            st.textContent = css;
            document.head.appendChild(st);
          }
          let el = document.getElementById("crest-cap");
          if (!el) {
            el = document.createElement("div");
            el.id = "crest-cap";
            document.body.appendChild(el);
          }
          el.innerHTML = t + (s ? `<small>${s}</small>` : "");
        },
        [CAPTION_CSS, title, sub],
      )
      .catch(() => {});
  };
  let ok = true;
  try {
    await fn(p, cap);
    console.log(`journey ok: ${name}`);
  } catch (e) {
    ok = false;
    console.log(`journey FAILED: ${name}: ${String(e.message || e).slice(0, 300)}`);
    await p.screenshot({ path: `${OUT}/fail-${name}.png` }).catch(() => {});
  }
  await ctx.close();
  const v = await p.video().path().catch(() => null);
  if (v) {
    renameSync(v, `${OUT}/${name}.webm`);
    console.log(`video: ${OUT}/${name}.webm`);
  }
  return ok;
}

const pause = (p, ms) => p.waitForTimeout(ms);
const go = async (p, url, ms = 1800) => {
  await p.goto(url);
  await p.waitForLoadState("networkidle").catch(() => {});
  await pause(p, ms);
};
const hash = async (p, h, ms = 1800) => {
  await p.evaluate((x) => (location.hash = x), h);
  await pause(p, ms);
};

async function consoleLogin(p, persona) {
  await go(p, C);
  const out = p.locator("#logout");
  if (await out.isVisible().catch(() => false)) {
    await out.click();
    await pause(p, 800);
  }
  await p.click(`[data-persona="${persona}"]`);
  await pause(p, 2000);
}

async function workerLogin(p) {
  await go(p, W);
  await p.click("#login-grace");
  await pause(p, 2200);
}

async function fieldLogin(p) {
  await go(p, F);
  await p.click("[data-login]");
  await pause(p, 2000);
}

/* ── J7 · A worker end to end (W-1, 22 screens) ───────────────────────── */
const J7 = async (p, cap) => {
  await go(p, W, 2500);
  await cap("J7 · A worker, end to end — two enrollment pathways (w1_1)",
    "Self-enroll or be enrolled with help. Neither is a fallback; both end in the same CREST ID and the same consent record.");
  await pause(p, 4500);
  await cap("The self path is the identity system's, not CREST's (w1_2–w1_3)",
    "eSignet proves the national identity; CREST only ever sees a pairwise reference. Consent comes BEFORE any record is created (w1_5).");
  await pause(p, 4500);
  await workerLogin(p);
  await cap("Home — the wallet, not a dashboard (w1_8)", "Real counts: credentials held, payments explained. Grace is the story's community health worker.");
  await pause(p, 4000);
  await hash(p, "#/work", 2200);
  await cap("What counts as evidence, and the seven days to check it (w1_9–w1_10)",
    "Open confirmation windows on real claims. Confirming is having your say — and every exit releases payment.");
  await pause(p, 4000);
  const btn = p.locator("[data-confirm]").first();
  if (await btn.isVisible().catch(() => false)) {
    await btn.click();
    await pause(p, 2500);
    await cap("Confirmed — the window exits, a credential is signed (w1_10 → w1_12)",
      "The record and the payment move together; nothing here can withhold the money.");
    await pause(p, 4000);
  }
  await hash(p, "#/work/declined", 2000);
  await cap("Declining work — the honest gap (w1_14)", "No decline endpoint exists; the screen says so instead of faking a state.");
  await pause(p, 3500);
  await hash(p, "#/wallet", 2200);
  await cap("The wallet — tier derived when checked, never stored (w1_12)", "Signed documents Grace holds; they do not need CREST to be believed.");
  await pause(p, 4000);
  await hash(p, "#/wallet/0", 2000);
  await pause(p, 2500);
  await hash(p, "#/wallet/0/show", 2500);
  await cap("Show to someone — the QR is the offline presentation (w1_18)",
    "Generated on this device; a verifier scans and checks the signature with no signal. The JSON sits behind a toggle. What a scan gives away is listed first.");
  await pause(p, 5000);
  // The consent loop is real now: a verifier asks through the verification
  // service, and Grace decides line by line.
  const shareR = await asParty(FIXORG, "POST", "/v1/presentation-requests", {
    subjectPartyId: FIXWORKER, requestedByPartyId: FIXORG,
    purpose: "Hiring for a private clinic " + STAMP,
  }, VERIFBASE);
  const shareId = (await shareR.json()).request.id;
  await hash(p, "#/shares", 2200);
  await cap("Someone is asking — who, and why, before any list (w1_19)",
    "The inbox names the requester and the purpose only. Nobody sees more than the public record without asking, and no disclosure list renders until Grace opens the request.");
  await pause(p, 5000);
  await hash(p, `#/shares/${encodeURIComponent(shareId)}`, 2500);
  await cap("Consent, per share, every time (w1_15)",
    "The resolved disclosure list, ticked line by line. Nothing moves before approval — a collect before the decision is the service's 409. The request expires on the service's real 72h clock.");
  await pause(p, 5000);
  const boxes = p.locator("[data-dis] input[type=checkbox]");
  if ((await boxes.count()) > 1) {
    await boxes.first().uncheck();
    await pause(p, 1500);
    await cap("Unticking a line is a per-line refusal — shown as refused, never as a gap", "");
    await pause(p, 3000);
    await p.click("#share-approve");
    await pause(p, 2500);
    await cap("Sent — what they got, what you kept (w1_20)",
      "Both faces read one record and one resolved list: this is the very list the verifier sees. The history callout carries Grace's real counts; the reference's twelve-asked-nine-agreed is named as its worked example.");
    await pause(p, 5500);
  }
  await hash(p, "#/wallet/deferred", 1800);
  await cap("Deferred qualification — the honest gap (w1_16)", "No endpoint serves it; the promise the screen will keep is stated.");
  await pause(p, 3000);
  await hash(p, "#/added", 2200);
  await cap("A qualification arrives, and earned strength re-derives (w1_17)",
    "Derived assurance with its because, and a live /v1/verify re-check — the tier is derived at this check, stored nowhere. The story's flip (weakest-assurance caveat gone when the anchor binds, credential untouched) is proven in the e2e walk.");
  await pause(p, 4500);
  const recheck = p.locator("#recheck");
  if (await recheck.isVisible().catch(() => false)) {
    await recheck.click();
    await pause(p, 3000);
  }
  await hash(p, "#/pay", 2200);
  await cap("My money — every held payment has a reason with an owner (w1_13, w1_24)",
    "The story seeds one held instruction; its reason and owner are on the face of it.");
  await pause(p, 4000);
  await hash(p, "#/pay/0", 2000);
  await pause(p, 3000);
  await hash(p, "#/profile/consents", 2000);
  await cap("Consents — granted, scoped, withdrawable (w1_5's later life)", "Withdrawal stops new records; it never deletes the work already done.");
  await pause(p, 3500);
  await hash(p, "#/profile/checks", 2000);
  await cap("Who checked me — every scan leaves a line (w1_18's other half)", "");
  await pause(p, 3000);
  await hash(p, "#/profile/recovery", 2000);
  await cap("Who can confirm it is you? Nomination is real, and party-linked (w1_7)",
    "POST /v1/parties/{id}/recovery-contacts — a person on the registry, never a phone number. Revoking keeps the row. Nomination routes the request; the quorum stays two voices from distinct authorities.");
  await pause(p, 4500);
};

/* ── J6 · Registering a worker who cannot self-register (W-2) ─────────── */
const J6 = async (p, cap) => {
  await fieldLogin(p);
  await cap("J6 · The Registering Agent's day starts with a list (w2_1)",
    "Naomi's device holds an offline queue; registrations sync when there is signal.");
  await pause(p, 4000);
  await hash(p, "#/register", 2000);
  await cap("Register a worker who cannot self-register (w2_2)",
    "Phone or roster id — never a raw national ID. Identity assertion is a reference, and the screen says what is illustrative.");
  await pause(p, 3500);
  await p.fill('#regform input[name="name"]', "Wafula Demo " + STAMP);
  await pause(p, 600);
  await p.fill('#regform input[name="phone"]', "+2547" + STAMP + "1");
  await pause(p, 1000);
  await p.locator("#regform button.btn, #regform .btn").first().click();
  await pause(p, 2500);
  await cap("Consent, read aloud, recorded (w2_3)",
    "The script is read in the worker's language. Honest gap: the 'voice' artefact is a typed sentence today, not captured audio.");
  await pause(p, 4000);
  const rec = p.locator("#recordbtn");
  if (await rec.isVisible().catch(() => false)) {
    await rec.click();
    await pause(p, 2500);
  }
  await cap("Registered — with the enroller's name on it (w2_5)",
    "Assurance is NOT raised by assistance: provenance is recorded, proof stays derived. Card printing is labelled illustrative.");
  await pause(p, 4500);
  await hash(p, "#/registrations", 2000);
  await cap("Back on the list — the day's work, synced", "A duplicate registration holds for the custodian and never auto-merges (w2_4) — the held one is in the console's queue (J11).");
  await pause(p, 4000);
};

/* ── J9 · Checking a credential (V-1 + V-2) ───────────────────────────── */
const J9 = async (p, cap) => {
  await go(p, V, 2200);
  await hash(p, "#/v1_1", 1800);
  await cap("J9 · A verifier arrives — identified, not onboarded (v1_1)",
    "Honest gap: pass issuance has no endpoint yet; the check itself needs no account at all.");
  await pause(p, 4000);
  await hash(p, "#/v1_2", 1800);
  await cap("The check itself (v1_2)", "Load a real credential and verify its signature — no permission from CREST needed. The camera/QR scan UX is the named gap; the verification is real.");
  await pause(p, 3000);
  await p.fill("#verifyform input >> nth=0", FIXWORKER);
  await p.click("#loadsample");
  await pause(p, 2200);
  await p.locator("#verifyform button.btn").last().click();
  await pause(p, 2800);
  await cap("Yes, plus four facts — nothing that identifies anyone (v1_3)",
    "Tier derived at this moment, never stored. A pairwise reference, not a name.");
  await pause(p, 5000);
  await hash(p, "#/v2_1", 2000);
  await cap("The institutional verifier — the same credential, a wider window (v2_1)",
    "Honest gap: the accreditation ceiling cannot be read from anywhere yet.");
  await pause(p, 4000);
  await hash(p, "#/v2_2", 2000);
  await cap("What the worker let through (v2_2)",
    "Honest gap: selective disclosure does not exist, so presence/absence is all that can be shown — refusal cannot be represented yet.");
  await pause(p, 4000);
  await hash(p, "#/v2_3", 2000);
  await cap("Batch — where volume becomes a different question (v2_3)", "Bounded batch verification against the real endpoint.");
  await pause(p, 3500);
  await hash(p, "#/person", 1800);
  await p.locator("#personform input").first().fill(FIXWORKER);
  await p.locator("#personform button.btn").last().click();
  await pause(p, 2500);
  await cap("A person's whole chain, resolved", "Everything the registry will say about one pairwise worker, in one read.");
  await pause(p, 4000);
};

/* ── J1 · Onboarding an organisation (G-2) ────────────────────────────── */
const J1 = async (p, cap) => {
  await go(p, C + "#/onboard", 2200);
  await cap("J1 · Six fields, and one of them decides everything after (g2_1)",
    "Legal name, country, work email, contact person, kind, sector — the desktop console frame, step 1 of 4. Registration documents are deliberately NOT asked here, exactly as the reference's callout says. Since #168 every field persists — country, kind, sector and contact ride the Party's attributes and read back from the registry.");
  await pause(p, 5500);
  await p.fill('[name="orgname"]', "Lakeside Health Trust " + STAMP);
  await pause(p, 500);
  await p.selectOption('[name="country"]', "KE");
  await pause(p, 400);
  await p.fill('[name="workemail"]', `programmes+${STAMP}@lakeside.example`);
  await pause(p, 500);
  await p.fill('[name="contactname"]', "Hon. Peter Demo");
  await pause(p, 400);
  await p.selectOption('[name="orgkind"]', "delivery");
  await pause(p, 500);
  await p.selectOption('[name="orgsector"]', "health");
  await pause(p, 900);
  await p.click("#orgapplyform button.dominant");
  await p.waitForSelector("#acceptterms", { timeout: 20000 });
  await cap("Published, named, and never edited underneath you (g2_11)",
    "Acceptance names an exact published version — the fact verifiers walk back to.");
  await pause(p, 4500);
  await p.click("#acceptterms");
  await p.waitForURL(/#\/onboard\/status/, { timeout: 20000 });
  await pause(p, 2000);
  await cap("The terminal state, honestly (g2_4, g2_13)",
    "The exact terms version accepted is on record. This deployment's approval model is manual, so the state waits on the operator's decision — where policy-on-acceptance is configured, approval lands in the acceptance's own transaction.");
  await pause(p, 5500);
  await hash(p, "#/onboard/standalone", 2200);
  await cap("This registration stands alone (g2_5)",
    "The real invitation inbox, read from the registry. An empty inbox is a true answer — either ordering works, and an invitation can arrive before or after this registration.");
  await pause(p, 4500);
  await hash(p, "#/onboard/wider", 2200);
  await cap("Wider terms, asked for only when needed (g2_6, g2_11)",
    "What these terms allow today against the other published sets. Requesting wider terms opens a reviewed request — it never edits the terms you hold.");
  await pause(p, 4500);
  await hash(p, "#/onboard/documents", 2200);
  await cap("What we need to see (g2_7)",
    "Declared references — kind, ref, hash — never the documents themselves. No file input exists on this screen, and that is asserted, not just intended.");
  await pause(p, 4500);
  await hash(p, "#/onboard/checks", 2200);
  await cap("What is checked before this is live (g2_12)",
    "Checks are recorded verdicts, each with a named owner — a person or a policy. Nothing here pretends to be automation that does not exist.");
  await pause(p, 4500);
};

/* ── J2 · Setting up the instance (G-1) ───────────────────────────────── */
const J2 = async (p, cap) => {
  // The admission queue at the end of this walk needs a real pending
  // application. Register one through the same open door the g2_1 form
  // posts to — a unique organisation per run, so J2 re-records cleanly.
  const PARTIES = new URL(BASE).port === "59110"
    ? `http://${new URL(BASE).hostname}:59000`
    : BASE.replace(/\/$/, "") + "/api/crest-registry";
  const regOut = await fetch(PARTIES + "/v1/organisations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      displayName: "Ministry of Health (demo) " + STAMP,
      kind: "organisation",
      contactRoutes: [{ kind: "email", value: `g.wanjiru+${STAMP}@health.example` }],
      attributes: { kind: "government", sector: "health", country: "KE", contactPerson: "Dr. Grace Wanjiru" },
    }),
  }).then((r) => r.json());
  const regId = regOut.party.id;

  await consoleLogin(p, "instance");
  await hash(p, "#/instance/setup", 2200);
  await cap("J2 · There is no wizard, and the screen says so (g1_1)",
    "A CREST instance is stood up by its deployment — compose or Railway — and this front door reads what that stand-up already decided, live from GET /v1/instance. No stand-up write is faked.");
  await pause(p, 5500);
  await hash(p, "#/instance/covers", 2200);
  await cap("What this instance covers — read-only by design (g1_2)",
    "The published self-description, live. The reference's four other fields — jurisdiction, identity anchor, residency, languages — are named as deploy-time configuration, not invented values.");
  await pause(p, 5000);
  await hash(p, "#/instance/consent", 2200);
  await cap("Consent rules, before the first worker (g1_3)",
    "The floor is enforced infrastructure: consent before any record, an artefact the worker can hear back, withdrawal that never unwinds a payment. Scripts and templates are deployment configuration (#59).");
  await pause(p, 5000);
  await hash(p, "#/instance/invite", 2200);
  await cap("Inviting the first organisation — no Send button, on purpose (g1_5)",
    "An instance-level invitation has no primitive: #182's invitation is a project's offer to an existing party, and g2_5 records the entry decision as still open. The gap is design finding #185, not a faked send.");
  await pause(p, 5500);
  await hash(p, "#/instance/services", 2200);
  await cap("The services behind all of it (g1_6)",
    "Six service doors, each answering its own /healthz live — a real sweep, not a status page. Services, not roles: who may do what is granted in the registry.");
  await pause(p, 4500);
  await hash(p, "#/admissions", 2200);
  await cap("Requests a person has to look at — the queue is real (g4_1)",
    "Registrations from GET /v1/registrations, terms requests from GET /v1/terms-requests. This deployment approves manually, so every application waits for a named person's decision.");
  await pause(p, 5000);
  await hash(p, "#/admissions/" + encodeURIComponent(regId), 2200);
  await cap("One request, and what the review cannot prove (g4_2)",
    "The registration read back — declared name, kind, sector, contact. The reference's own caution carried verbatim: nothing here establishes that the submitter speaks for the body.");
  await pause(p, 5000);
  await p.fill("#decide-reason", "confirmed against the state register (demo)");
  await pause(p, 1200);
  await p.click("#approve-registration");
  await pause(p, 2500);
  await cap("Approved — the whole decision, under the decider's name (g4_3)",
    "POST /v1/organisations/{id}/decision: approval publishes the organisation in the same transaction, the decider is the authenticated reviewer (#89), and a rejection without a reason is refused. Sender keys are named unbuilt.");
  await pause(p, 5500);
};

/* ── J3 · Setting up a project (P-1 + P-2) ────────────────────────────── */
const J3 = async (p, cap) => {
  await go(p, C, 2000);
  const out3 = p.locator("#logout");
  if (await out3.isVisible().catch(() => false)) { await out3.click(); await pause(p, 800); }
  await cap("J3 · One door, every console role (n1)",
    "Sign-in offers no role selector — a role is granted in the registry and read back, never self-declared. This screen is our design; the reference never draws it.");
  await pause(p, 4500);
  await consoleLogin(p, "orgadmin");
  await hash(p, "#/where", 2200);
  await cap("Where do you want to work? (n2)",
    "Contexts come from granted roles read live from the registry. An empty list is a true answer.");
  await pause(p, 4000);
  await hash(p, "#/org", 2200);
  await cap("The Org Admin — standing configuration, not project work (p1_1)",
    "The organisation's record, terms held, authorizations — read live, including the declined-projects queue.");
  await pause(p, 4500);
  await hash(p, "#/projects/new", 2200);
  await cap("Creating a project, and handing it over (p1_3)",
    "POST /v1/projects names a Configurator; ownership starts PENDING. Creating is not configuring — acceptance is the Configurator's to give.");
  await pause(p, 4500);
  await hash(p, "#/owners", 2200);
  await cap("Roles held, with grantor and date (p2_6)",
    "A role is a record with an owner, not a checkbox — who granted it, when, and its state.");
  await pause(p, 4000);
  await consoleLogin(p, "configurator");
  await hash(p, "#/handover", 2200);
  await cap("Ministry of Health handed you a project (n4)",
    "The receiving side the reference never draws. Declining records who and why, and returns the project — it deletes nothing.");
  await pause(p, 4500);
  await hash(p, "#/compose", 2200);
  await cap("Composition — a typed answer, no invented taxonomy (p2_1, p2_3, p2_5)",
    "Each choice is stored with its decider and date. The five named vocabularies are L2 configuration this deployment does not declare.");
  await pause(p, 4500);
  await hash(p, "#/activate", 2200);
  await cap("Activation — a refusal names what is missing (p2_7)",
    "Infrastructure conditions and declared gates, each satisfiable; ACTIVE only when all are met. The read/write asymmetry is finding #176.");
  await pause(p, 4500);
  await hash(p, "#/partners", 2200);
  await cap("Partners — approved orgs, time-bound grants (p2_17, p2_18)",
    "A grant rides the partner's terms; an end date past the terms is refused (422), proven end to end.");
  await pause(p, 4000);
  await hash(p, "#/finance", 2200);
  await cap("The finance-code link, and the support owner (p2_8, p2_10)",
    "Stored verbatim against the project; the chart-of-accounts pull behind the reference's picker does not exist and the screen says so.");
  await pause(p, 4000);
  await hash(p, "#/status", 2200);
  await cap("A funnel, not a set of totals (p2_11)",
    "Windows, exits, credentials, instructions — real queues. STP and tier-mix metric contracts are named as unbuilt (p2_12, p2_13, #31).");
  await pause(p, 4500);
  await hash(p, "#/people", 2200);
  await cap("People & roles is not yours to change (n5)",
    "An entry is never hidden by role. The guard names who can grant, shows what is readable, and shows no status code.");
  await pause(p, 4500);
};

/* ── J8 · From attestation to credential (W-4 + P-10) ─────────────────── */
const J8 = async (p, cap) => {
  await fieldLogin(p);
  await hash(p, "#/toconfirm", 2200);
  await cap("J8 · The boundary, stated: this is NOT source attestation (w3_1–w3_3)",
    "The reference's worklist belongs to the delivery platform. What CREST offers is assisted confirmation — a real W1/W4 window exit for a worker who cannot be reached, recorded as assisted, and every exit releases payment.");
  await pause(p, 6000);
  await hash(p, "#/roster", 2200);
  await cap("Evidence intake — where the roster reaches CREST (w3_4)",
    "In the reference the roster closes in the delivery platform first; this is the ingestion side, row by row against the definition.");
  await pause(p, 4000);
  await p.locator("#rosterform button.btn").click();
  await pause(p, 3000);
  await cap("Per-row verdicts — a mismatch is somebody named, not a status (p2_20, p2_21)", "Accepted rows become claims; unclear rows go to the custodian.");
  await pause(p, 4500);
  await hash(p, "#/handoff", 2200);
  await cap("Who holds it next (w3_5)", "The confirmation reaches CREST as ingested evidence; the queues that follow are real.");
  await pause(p, 4500);
  await go(p, V + "#/w6_1", 2000);
  await cap("The external evidence contact — a template out, a file back (w6_1, w6_2)",
    "Honest gap: no scoped-link endpoint exists; the panel explains the journey without faking the upload.");
  await pause(p, 4500);
  await hash(p, "#/w6_2", 2000);
  await pause(p, 3000);
};

/* ── J10 · When something stalls (W-3 + W-5) ──────────────────────────── */
const J10 = async (p, cap) => {
  await consoleLogin(p, "support");
  await hash(p, "#/cases", 2200);
  await cap("J10 · The queue of things that stalled (w5_1)",
    "Synthesized from real stalled states — held payments, unclear rows, open recoveries. No case-management service exists, and the screen says so.");
  await pause(p, 5000);
  await hash(p, "#/supportfind", 2000);
  await cap("Five ways to find a worker (w5_2)", "Lookup through the real resolve endpoint.");
  await pause(p, 3500);
  await hash(p, "#/supporttrace", 2200);
  await cap("Tracing a payment the agent cannot fix (w5_3)", "The chain — claim, window, exit, credential, instruction — each hop a real read.");
  await pause(p, 4000);
  await consoleLogin(p, "custodian");
  await hash(p, "#/recover", 2200);
  await cap("Recovery, administered by the custodian (w4 begins here)",
    "The story's open recovery is real. The confirmer now has screens of their own in the worker door — only the SMS delivery channel is still absent (#150, §16), and those screens say so.");
  await pause(p, 5500);

  // The confirmer's own side (w4_1–w4_3): Grace's nomination routes the
  // request to the supervisor; the custodian opens the recovery (a leftover
  // OPEN one from another run is reused — one live recovery per party).
  await asParty(FIXWORKER, "POST", `/v1/parties/${FIXWORKER}/recovery-contacts`,
    { contactPartyId: FIXSPVR });
  const opened = await asParty(FIXCSTD, "POST", "/v1/recoveries", {
    partyId: FIXWORKER, openedByPartyId: FIXCSTD, reason: "lost phone " + STAMP,
  });
  let recId;
  if (opened.status === 201) recId = (await opened.json()).id;
  else {
    const l = await asParty(FIXSPVR, "GET", `/v1/recoveries?confirmerPartyId=${FIXSPVR}`);
    recId = (((await l.json()) || {}).recoveries || [])
      .find((x) => x.partyId === FIXWORKER && !x.completedAt)?.id;
  }
  await go(p, W, 1500);
  const out10 = p.locator("#logout");
  if (await out10.isVisible().catch(() => false)) { await out10.click(); await pause(p, 800); }
  await p.click("#login-supervisor");
  await pause(p, 2200);
  await hash(p, "#/vouch", 2200);
  await cap("The Recovery Confirmer's request, delivered honestly (w4_1)",
    "The reference draws an SMS; this deployment has no SMS channel (#150, §16) and says so on-screen. The request itself — GET /v1/recoveries?confirmerPartyId= — is what any returning channel would deliver from.");
  await pause(p, 5500);
  if (recId) {
    await hash(p, `#/vouch/${encodeURIComponent(recId)}`, 2200);
    await cap("Two voices from distinct authorities, read live (w4_2)",
      "Completion appends a NEW identity binding of class recovery; the old bindings stay for audit, the credential history is untouched, and the recovered worker re-enters at IA-1 until they re-anchor. The refusal path (w4_3) is a recorded answer with an owner, never a dead end.");
    await pause(p, 6000);
  }
};

/* ── J11 · Seeing where it stands (P-2 monitoring + V-4 + G-4) ────────── */
const J11 = async (p, cap) => {
  await consoleLogin(p, "configurator");
  await hash(p, "#/status", 2200);
  await cap("J11 · The funnel (p2_11) — and the metric contracts named as unbuilt (p2_12, p2_13)", "");
  await pause(p, 4500);
  await hash(p, "#/payments", 2200);
  await cap("Money delayed and people waiting are different sentences (p2_14)",
    "Instructions grouped by state; the story's held payment carries its reason and its owner.");
  await pause(p, 4500);
  await consoleLogin(p, "funder");
  await hash(p, "#/portfolio", 2200);
  await cap("The funder — allocated against paid (v4_1, v4_2)",
    "Honest gap: no funding ledger exists; the paid side reads real instructions, the allocation is labelled illustrative.");
  await pause(p, 5000);
  await consoleLogin(p, "custodian");
  await hash(p, "#/dupes", 2200);
  await cap("Duplicates — a rate to drive down, and a rule that must hold (g4_6)",
    "merges_without_confirmation = 0 is a monitored metric. Probable matches hold; they never auto-merge.");
  await pause(p, 5000);
  await hash(p, "#/unclear", 2000);
  await cap("Unclear rows — a mismatch is somebody named (p2_21)", "Coverage, quality worklists and reuse metrics remain the named gaps (g4_4, g4_5, g4_7).");
  await pause(p, 4500);
};

/* ── J4 · Defining the work (P-3, 28 screens) ─────────────────────────── */
const J4 = async (p, cap) => {
  await consoleLogin(p, "author");
  await hash(p, "#/definework", 2500);
  await cap("J4 · The author's wizard — a reader over the one seeded definition (p3_1)",
    "Ten sections of the real definition. The write side — all 22 authoring branches, adaptor mapping, testing — is missing, and the manifest says exactly which screens (p3_2…p3_28).");
  await pause(p, 6000);
  await p.mouse.wheel(0, 700);
  await pause(p, 3000);
  await cap("The schema under the form (p3_19)", "What the record actually is — the stored definition, not a mock.");
  await pause(p, 4000);
  await consoleLogin(p, "approver");
  await cap("The approver — a separate session that ratifies and can never draft",
    "No wizard in this navigation; a hand-typed #/definework bounces home. Ratification itself has no endpoint (p3_15, p3_16) — the screen names the gap instead of faking a signature.");
  await pause(p, 6500);
};

/* ── J5 · Payment and putting it right (F-1 + F-2) ────────────────────── */
const J5 = async (p, cap) => {
  // F-1: the Org Admin names one owner; the walk assigns the fixture
  // organisation itself, exactly as apps.spec.js's funders walk does.
  await consoleLogin(p, "orgadmin");
  await hash(p, "#/rateowner", 2000);
  await cap("J5 · A request to put someone on payment (f1_2)",
    "Anyone can ask. Only one person can assign — the Org Admin's own act, kept as history.");
  await p.fill('[name="rateownerparty"]', FIXORG).catch(() => {});
  await p.click("#assign-owner").catch(() => {});
  await pause(p, 2500);

  await consoleLogin(p, "rateowner");
  await hash(p, "#/rate", 2200);
  await cap("The Rate Owner prices a unit somebody else defined (f1_3)",
    "The unit of work is read-only by construction; only the amount, the calculation rule and the effective date are the owner's to set.");
  await p.fill('[name="rateamount"]', "175.00").catch(() => {});
  await pause(p, 2000);
  await p.click("#rate-continue").catch(() => {});
  await pause(p, 2000);
  await cap("Publication is a new version, never an edit (f1_4)",
    "The seeded rate stays on the record; publishing names what it supersedes. No edit control exists here, and none ever will.");
  await pause(p, 3500);
  await p.click("#publish-rate").catch(() => {});
  await pause(p, 2200);
  await cap("Half done is a real, derived state (f1_5)",
    "The rate is live; the money still cannot move until a mechanism exists. Nothing here is stored — the standing is computed fresh every time.");
  await pause(p, 3500);

  // A fresh project so the mechanism half has somewhere real to live — the
  // same isolation apps.spec.js's funders walk keeps for re-runnability.
  const stamp = Date.now().toString().slice(-6);
  const r = await asParty(FIXORG, "POST", "/v1/projects", {
    name: "J5 recording " + stamp, ownerPartyId: FIXORG,
    configuration: { coverage: "Journey recording" },
  });
  const proj = await r.json();
  const pid = (proj.project || proj).id;

  await consoleLogin(p, "payowner");
  await hash(p, "#/where", 2000);
  await p.click(`[data-context="${pid}"]`).catch(() => {});
  await pause(p, 1500);
  await hash(p, "#/mech/test", 2000);
  await cap("The Payment Mechanism Owner sends one real payment (f2_4)",
    "Through the mock rail — recorded whether it succeeds or fails, never smoothed over. CREST's boundary is the instruction; the rail beyond it is connected, not owned.");
  await p.fill('[name="testdest"]', "recording-account").catch(() => {});
  await p.fill('[name="testamount"]', "10.00").catch(() => {});
  await p.click("#send-test").catch(() => {});
  await pause(p, 2500);

  await hash(p, "#/mech/recon", 2000);
  await cap("The reconciliation file, tied back line by line (f2_5)",
    "crest-recon-csv-v1 — every row an instruction id, agreed by the owner before it is trusted.");
  await p.click("#agree-recon").catch(() => {});
  await pause(p, 2200);

  await hash(p, "#/mech/statement", 2000);
  await cap("The advisory statement carries its own limits (f2_6)",
    "A held payment appears here with its reason and its owner — never missing, always explained.");
  await p.click("#agree-statement").catch(() => {});
  await pause(p, 2200);

  await hash(p, "#/mech/batching", 2000);
  await cap("Batching is paid for by the worker — the trade-off must be named (f2_7)",
    "The reference asks whether a dispute should hold payment. It never does — a dispute contests the record, not the money (W4) — and the screen answers that honestly instead of drawing a working toggle (design finding #195).");
  await p.fill('[name="batchwindow"]', "daily-17:00").catch(() => {});
  await p.fill('[name="batchtradeoff"]', "workers paid once daily wait up to a day for confirmed work").catch(() => {});
  await p.click("#record-batching").catch(() => {});
  await pause(p, 2500);

  await hash(p, "#/mech/activate", 2200);
  await cap("Activation refuses readably — never a bare no (f2_8)",
    "Four conditions, each satisfied only by a recorded act. What is unmet is named, not hidden.");
  await pause(p, 4000);
};

/* ── J12 · Systems CREST does not own (EXT) ───────────────────────────── */
const J12 = async (p, cap) => {
  await go(p, W, 2200);
  await cap("J12 · Where identity actually comes from (ext_1)",
    "eSignet/MOSIP is the identity system; CREST is a relying party. The login IS the integration — CREST never holds the national ID.");
  await pause(p, 5500);
  await go(p, F, 1500);
  await p.click("[data-login]");
  await pause(p, 1500);
  await hash(p, "#/roster", 2200);
  await cap("Where the strongest evidence is recorded (ext_2)",
    "The delivery platform owns the source record; CREST's side is the intake boundary you see here.");
  await pause(p, 5000);
  await consoleLogin(p, "payowner");
  await hash(p, "#/paysetup", 2200);
  await cap("The boundary where CREST stops (ext_3)",
    "Beyond the instruction, the rail is the payment system's — connected, not owned.");
  await pause(p, 5000);
  await go(p, V + "#/v1_2", 2200);
  await cap("Why the credential is worth anything (ext_4)",
    "A next employer verifies the signature — no account with CREST, no call home needed.");
  await pause(p, 5000);
};

const ALL = { J1, J2, J3, J4, J5, J6, J7, J8, J9, J10, J11, J12 };
const order = process.argv.slice(2).length
  ? process.argv.slice(2)
  : ["J7", "J6", "J9", "J1", "J2", "J3", "J8", "J10", "J11", "J4", "J5", "J12"];

const b = await chromium.launch();
let failed = 0;
for (const name of order) {
  if (!ALL[name]) {
    console.log("unknown journey:", name);
    continue;
  }
  if (!(await journey(b, name, ALL[name]))) failed++;
}
await b.close();
console.log(failed ? `${failed} journey(s) FAILED` : "all journeys recorded");
process.exit(failed ? 1 : 0);
