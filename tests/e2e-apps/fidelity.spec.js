// The fidelity gate — "exactly as the reference HTML" as a CI verdict.
//
// Phase 1, scoped to J3 (the p1_* and p2_* reference screens, plus our own
// connective-tissue screens n1–n5). It reads docs/journey-spec.json — the
// generated projection of the 17 Aug reference, one entry per screen — drives
// the real local stack to each in-scope screen, and asserts what that entry
// says the screen carries: field labels, callout titles and callout text,
// primary/secondary button labels, the step counter, the rail, the layout
// idiom, and the content the screen must NOT show.
//
// Three verdicts, and no fourth:
//
//   asserted   the screen's traceability status is `implemented` and every
//              unwaived facet held. A regression here turns CI red.
//   skipped    the status is not `implemented` — the screen is not claimed to
//              exist yet, so there is nothing to hold to. Reported WITH the
//              status and the ledger's own gap note, never silently passed.
//   quarantined a screen claimed `implemented` that the gate cannot judge
//              today for a named, issue-linked reason (fidelity-quarantine
//              .json). Failures are printed and recorded, not blocking — and
//              a quarantined screen that fully passes FAILS, so a stale
//              quarantine cannot survive the thing it was hiding being fixed.
//
// What the gate can judge: presence of the reference's words and controls,
// absence of the words the reference deliberately withholds, and the coarse
// layout idiom (desktop console frame vs phone card). What it cannot judge:
// spacing, weight, colour, rhythm, whether a screen *reads* like the
// reference — that is what `make fidelity-sheet` puts in front of a human.
//
// Extending to another journey is adding entries to fidelity-map.json.
const fs = require("fs");
const path = require("path");
const { test, expect } = require("@playwright/test");

const ROOT = path.resolve(__dirname, "..", "..");
const read = (p) => JSON.parse(fs.readFileSync(p, "utf8"));

const SPEC = read(path.join(ROOT, "docs", "journey-spec.json"));
const LEDGER = read(path.join(ROOT, "docs", "journey-traceability.json"));
const MAP = read(path.join(__dirname, "fidelity-map.json"));
const FORBIDDEN = read(path.join(__dirname, "fidelity-forbidden.json"));
const WAIVERS = read(path.join(__dirname, "fidelity-waivers.json"));
const QUARANTINE = read(path.join(__dirname, "fidelity-quarantine.json"));
const RESULTS = path.join(__dirname, "fidelity-results.jsonl");

// Reference rows AND our own design rows (n1-n5). The design screens were
// invisible to the gate while this read LEDGER.rows alone: whatever the
// ledger said about n1-n5 they reported "skipped (designed)", so a design
// screen could claim `implemented` with nothing holding it to its spec. Same
// verdict rules either way — the source of a screen decides nothing about
// whether it is judged.
const STATUS = Object.fromEntries(
  [...LEDGER.rows, ...(LEDGER.designRows || [])].map((r) => [r.id, r]),
);

// ── Text comparison ────────────────────────────────────────────────────────
// The reference is typeset prose: curly quotes, em dashes, non-breaking
// spaces. An implementation that types the same sentence with an ASCII
// apostrophe is faithful, so both sides are folded to one form before
// comparison. Whitespace collapses; nothing else is thrown away — the words
// themselves must be there.
function fold(s) {
  return String(s)
    .normalize("NFC")
    .replace(/[‘’ʼ]/g, "'")
    .replace(/[“”]/g, '"')
    .replace(/[–—−]/g, "-")
    .replace(/[   ]/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .toLowerCase();
}

// ── Arrival ────────────────────────────────────────────────────────────────
const DOORS = { console: "/console/", field: "/enrolment/", worker: "/worker/", verify: "/verify/" };

// ── Scripted-flow arrivals (#179) ──────────────────────────────────────────
// The G-2 onboarding screens hold mid-flow state per browser session
// (sessionStorage `crest.console.onboarding`, written by the g2_1 register
// form), so a cold-loaded route honestly says "No application in progress".
// A `flow:` arrival drives the real flow first — real POSTs against the real
// stack, a unique organisation per run — to the depth the screen needs, then
// lands on the mapped route. The steps are the same ones apps.spec.js's G-2
// walk proves end to end; this is arrival machinery, not a second proof.
//
// The acts that are NOT the organisation's — a project sending an invitation,
// the operator deciding, an operator publishing a terms set — go through the
// same service doors as bearer-authenticated API calls by the party who holds
// that act, exactly as the walk, the seeder and a real deployment do.
const FLOW_FIX = {
  org: "did:crest:party:01JCREST000000000000000RGN",
  custodian: "did:crest:party:01JCREST00000000000000CSTD",
  supervisor: "did:crest:party:01JCREST00000000000000SPVR",
  workerA: "did:crest:party:01JCREST00000000000000WRKA",
  chandra: "did:crest:party:01JCREST00000000000000WRKC",
  project: "crest:context:01JCREST00000000000000PRJC",
};

const FLOW_API = (() => {
  const base = process.env.BASE_URL || "http://localhost:59110";
  const local = new URL(base).port === "59110";
  const host = new URL(base).hostname;
  return {
    parties: local ? `http://${host}:59000` : base.replace(/\/$/, "") + "/api/crest-registry",
    verification: local ? `http://${host}:59000` : base.replace(/\/$/, "") + "/api/crest-verification",
    oidc: local ? `http://${host}:59103` : base.replace(/\/$/, "") + "/api/crest-mock-oidc",
    payments: local ? `http://${host}:59006` : base.replace(/\/$/, "") + "/api/crest-payments",
    evidence: local ? `http://${host}:59000` : base.replace(/\/$/, "") + "/api/crest-evidence",
  };
})();

// The funders wave's own fixtures (apps.spec.js's "the funders walk"), reused
// here as arrival machinery: a real assignment, a real rate, a real
// mechanism, a real held instruction, a real activation — never a second
// proof, the same acts the walk already drives end to end.
const FUNDERS_DEFN = "crest:definition:01JCREST00000000000000DEFN";

async function flowSignedInAs(page, who) {
  await page
    .locator(".appbar")
    .filter({ hasText: who })
    .first()
    .waitFor({ state: "visible", timeout: 20000 });
}

async function flowConsolePersona(page, persona) {
  await page.goto("/console/");
  await settle(page);
  const card = page.locator(`[data-persona="${persona}"]`);
  if (!(await card.count())) return `no console persona card [data-persona="${persona}"]`;
  await card.click();
  const ok = await page
    .locator("#logout")
    .waitFor({ state: "visible", timeout: 20000 })
    .then(() => true, () => false);
  if (!ok) return `persona ${persona} could not sign in within 20s`;
  await settle(page);
  return "";
}

async function flowFundersArrive(page, request, mode, route) {
  const me = FLOW_FIX.org;
  const stamp = flowStamp();

  // f1_2/f1_3/f1_4/f1_5 — the F-1 rate-owner half, exactly as the funders
  // walk drives it. Nothing here is a second proof; it is arrival machinery
  // reusing the walk's own acts so the screen lands with real state on it.
  if (mode === "funders-rate" || mode === "funders-rate-published") {
    let p = await flowConsolePersona(page, "orgadmin");
    if (p) return p;
    await flowSignedInAs(page, "Peter Otieno");
    await flowLand(page, "#/rateowner");
    await page.fill('[name="rateownerparty"]', me);
    await page.click("#assign-owner");
    const assigned = await page.locator("body").filter({ hasText: /owner assigned/i })
      .first().waitFor({ state: "visible", timeout: 20000 }).then(() => true, () => false);
    if (!assigned) return "the owner assignment did not confirm within 20s";
    await page.click("#logout");
    await settle(page);

    p = await flowConsolePersona(page, "rateowner");
    if (p) return p;
    await flowSignedInAs(page, "Nadia Okoth");

    if (mode === "funders-rate") {
      await flowLand(page, "#/rate");
      return "";
    }

    // funders-rate-published — price the unit and publish, landing on
    // whichever of ratepublish/ratestanding the caller asked for.
    await flowLand(page, "#/rate");
    await page.fill('[name="rateamount"]', "175.00");
    await page.click("#rate-continue");
    const onPublish = await page.waitForURL(/#\/ratepublish/, { timeout: 20000 }).then(() => true, () => false);
    if (!onPublish) return "authoring did not reach #/ratepublish within 20s";
    await settle(page);
    if (route === "#/ratestanding") {
      await page.click("#publish-rate");
      const onStanding = await page.waitForURL(/#\/ratestanding/, { timeout: 20000 }).then(() => true, () => false);
      if (!onStanding) return "publishing did not reach #/ratestanding within 20s";
      await settle(page);
    }
    return "";
  }

  // f2_4..f2_10 — the F-2 mechanism half. A fresh project keeps every run
  // re-runnable (a mechanism is per context, activation is one-way) — the
  // same reason the walk itself makes one.
  if (mode.startsWith("funders-")) {
    let r = await flowAsPartyOn(request, FLOW_API.parties, me, "POST", "/v1/projects", {
      name: "Fidelity gate funders " + stamp, ownerPartyId: me,
      configuration: { coverage: "Fidelity gate walk" },
    });
    if (r.status() !== 201) return `the flow's own project was refused (${r.status()})`;
    const projBody = await r.json();
    const projId = (projBody.project || projBody).id;

    r = await flowAsPartyOn(request, FLOW_API.parties, me, "POST", "/v1/authorizations", {
      id: "crest:authorization:" + fakeUlid(),
      partyId: FLOW_FIX.supervisor,
      terms: { id: "crest:terms:01JCREST00000000000000TERM", version: 1 },
      scope: { kind: "context", contextId: projId },
      functions: ["submit-work-evidence"],
      period: { start: "2026-01-01T00:00:00Z", end: "2027-12-31T00:00:00Z" },
      authorityPartyId: me, approvedByPartyId: me,
      approvedAt: "2026-09-01T00:00:00Z", state: "ACTIVE",
    });
    if (r.status() !== 201) return `the supervisor's grant on the flow's project was refused (${r.status()})`;

    let p = await flowConsolePersona(page, "payowner");
    if (p) return p;
    await flowSignedInAs(page, "Daniel Mwangi");
    await flowLand(page, "#/where");
    const chose = await page.locator(`[data-context="${projId}"]`).count();
    if (!chose) return `the flow's own project ${projId} never appeared at #/where`;
    await page.locator(`[data-context="${projId}"]`).click();
    await settle(page);

    await flowLand(page, "#/mech/test");
    await page.click("#mech-create");
    const configured = await page.locator("body").filter({ hasText: /configured, not live/i })
      .first().waitFor({ state: "visible", timeout: 20000 }).then(() => true, () => false);
    if (!configured) return "the mechanism did not reach 'configured, not live' within 20s";

    if (mode === "funders-mechanism") {
      await flowLand(page, route);
      return "";
    }

    // funders-held (f2_9) / funders-live (f2_10) — drive the invariant
    // through, then (funders-live only) every activation condition.
    await flowBindParty(request, FLOW_FIX.workerA);
    const supTok = await flowMintToken(request, FLOW_FIX.supervisor);
    const csv = "activity,outcome_value,outcome_unit,worker_id_kind,worker_id," +
      "period_start,period_end,geography,household_id,beneficiary_count,source_record_ref\n" +
      `bednet-distribution,3,bednets-distributed,phone,+15550100011,2026-09-01,2026-09-01,` +
      `Riverside,fidelity-HH-${stamp},3,fidelity-funders-${stamp}\n`;
    const batch = await request.fetch(FLOW_API.evidence +
      `/v1/batches?contextId=${encodeURIComponent(projId)}&definitionId=${encodeURIComponent(FUNDERS_DEFN)}` +
      `&submittedBy=${encodeURIComponent(FLOW_FIX.supervisor)}&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=signed-batch&systemRef=fidelity-gate`, {
      method: "POST",
      headers: { Authorization: "Bearer " + supTok, "Content-Type": "text/csv" },
      data: csv,
    });
    if (![200, 201].includes(batch.status())) return `the flow's own batch was refused (${batch.status()})`;
    const claimId = (await batch.json()).claimIds[0];
    if (!claimId) return "the batch minted no claim";

    let windowUp = false;
    for (let i = 0; i < 30 && !windowUp; i++) {
      const w = await flowAsPartyOn(request, FLOW_API.payments, FLOW_FIX.workerA, "GET", `/v1/windows/${claimId}`);
      windowUp = w.status() === 200;
      if (!windowUp) await page.waitForTimeout(2000);
    }
    if (!windowUp) return "the confirmation window never opened within 60s";
    r = await flowAsPartyOn(request, FLOW_API.payments, FLOW_FIX.workerA, "POST", `/v1/claims/${claimId}/confirm`, {});
    if (r.status() !== 200) return `the worker's confirmation exit was refused (${r.status()})`;

    let instruction;
    let held = false;
    for (let i = 0; i < 30 && !held; i++) {
      const res = await flowAsPartyOn(request, FLOW_API.payments, me, "GET", `/v1/instructions/by-claim/${claimId}`);
      if (res.status() === 200) {
        instruction = await res.json();
        held = instruction.state === "HELD";
      }
      if (!held) await page.waitForTimeout(2000);
    }
    if (!held) return "the exit's instruction never reached HELD within 60s";

    if (mode === "funders-held") {
      await flowLand(page, route);
      return "";
    }

    // funders-live — complete every activation condition, then activate.
    const mechR = await flowAsPartyOn(request, FLOW_API.payments, me, "GET",
      `/v1/mechanisms/by-context/${encodeURIComponent(projId)}`);
    const mechId = (await mechR.json()).mechanism.id;

    await flowLand(page, "#/mech/test");
    await page.fill('[name="testdest"]', "fidelity-gate-account");
    await page.fill('[name="testamount"]', "10.00");
    await page.click("#send-test");
    const testOk = await page.locator('[data-test-result="SUCCEEDED"]')
      .waitFor({ state: "visible", timeout: 20000 }).then(() => true, () => false);
    if (!testOk) return "the test disbursement did not succeed within 20s";

    await flowLand(page, "#/mech/recon");
    await page.click("#agree-recon");
    await settle(page);

    await flowLand(page, "#/mech/statement");
    await page.click("#agree-statement");
    await settle(page);

    await flowLand(page, "#/mech/batching");
    await page.fill('[name="batchwindow"]', "daily-17:00");
    await page.fill('[name="batchtradeoff"]',
      "workers paid once daily at 17:00 wait up to a day for confirmed work " + stamp);
    await page.click("#record-batching");
    const batching = await page.locator("[data-batching]")
      .waitFor({ state: "visible", timeout: 20000 }).then(() => true, () => false);
    if (!batching) return "the batching choice did not record within 20s";

    await flowLand(page, "#/mech/qualify");
    await page.click("#submit-qual");
    const submitted = await page.waitForURL(/#\/mech\/live/, { timeout: 20000 }).then(() => true, () => false);
    if (!submitted) return "submitting for verification did not reach #/mech/live within 20s";
    await settle(page);

    r = await flowAsPartyOn(request, FLOW_API.payments, FLOW_FIX.custodian, "POST",
      `/v1/mechanisms/${mechId}/records`,
      { kind: "qualification-verified", actorPartyId: FLOW_FIX.custodian });
    if (r.status() !== 201) return `the verification record was refused (${r.status()})`;

    await flowLand(page, "#/mech/activate");
    await page.click("#activate-mech");
    const live = await page.waitForURL(/#\/mech\/live/, { timeout: 20000 }).then(() => true, () => false);
    if (!live) return "activation did not reach #/mech/live within 20s";
    await settle(page);
    return "";
  }

  return `unknown flow arrival "flow:${mode}"`;
}

async function flowMintToken(request, partyId) {
  const sub = "story|" + partyId.replace("did:crest:party:", "");
  const r = await request.post(FLOW_API.oidc + "/token", {
    data: { sub, aud: "crest", expiresIn: "1h" },
  });
  if (!r.ok()) throw new Error(`the dev issuer refused a token for ${partyId} (${r.status()})`);
  const d = await r.json();
  return d.accessToken || d.access_token || d.token;
}

async function flowAsPartyOn(request, svcBase, partyId, method, path, body) {
  const token = await flowMintToken(request, partyId);
  return request.fetch(svcBase + path, {
    method,
    headers: { Authorization: "Bearer " + token, "Content-Type": "application/json" },
    data: body === undefined ? undefined : body,
  });
}

async function flowAsParty(request, partyId, method, path, body) {
  return flowAsPartyOn(request, FLOW_API.parties, partyId, method, path, body);
}

// First-login self-bind for a party the fixture world never bound — the
// exact append the browser's dev login performs, done from the test for a
// party (the funders walk's worker) that here acts only through the API.
async function flowBindParty(request, partyId) {
  const token = await flowMintToken(request, partyId);
  const sub = "story|" + partyId.replace("did:crest:party:", "");
  const pw = await request
    .get(FLOW_API.oidc + "/dev/pairwise?sub=" + encodeURIComponent(sub))
    .then((r) => r.json());
  return request.post(FLOW_API.parties + `/v1/parties/${encodeURIComponent(partyId)}/identity-bindings`, {
    headers: { Authorization: "Bearer " + token, "Content-Type": "application/json" },
    data: { provider: "mock-oidc", providerClass: "generic-oidc", subjectRef: pw.subject },
  });
}

// A run must be re-runnable on the same stack (the G-2 walk learned this):
// every registration and every published terms set is unique per call.
const FUNDERS_ULID32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const fakeUlid = () =>
  "01" + Array.from({ length: 24 }, () => FUNDERS_ULID32[Math.floor(Math.random() * 32)]).join("");

function flowStamp() {
  return (Date.now() + Math.floor(Math.random() * 1000)).toString().slice(-6);
}

// The operator publishes a wider terms set through the same open door the
// seeder uses; returns its id (26-char suffix, walk-shaped, unique per call).
async function flowPublishWiderTerms(request) {
  const id = "crest:terms:01JCRESTG2GATEX" + flowStamp() + "PAY00";
  const r = await request.post(FLOW_API.parties + "/v1/terms", {
    data: {
      id, version: 1, name: "Full delivery with payment",
      permissions: ["submit-work-evidence", "specify-definition", "ratify-definition", "set-rates", "instruct-payment"],
      publishedAt: "2026-09-01T00:00:00Z",
    },
  });
  if (r.status() !== 201) throw new Error(`publishing the wider terms set failed (${r.status()})`);
  return id;
}

// The seeded project invites the organisation. The period's end is the date
// the reference's g2_10 callout names ("This grant ends on 31 March 2027").
async function flowInvite(request, orgId) {
  const r = await flowAsParty(request, FLOW_FIX.org, "POST",
    `/v1/projects/${FLOW_FIX.project}/invitations`, {
      partyId: orgId,
      functions: ["submit-work-evidence"],
      period: { start: "2026-09-01T00:00:00Z", end: "2027-03-31T00:00:00Z" },
      note: "Nakuru community health, wards 4 and 7",
    });
  if (r.status() !== 201) throw new Error(`the project's invitation was refused (${r.status()})`);
}

// ── The workers wave's flow arrivals ───────────────────────────────────────
// Every one drives real writes — a presentation request through the
// verification service, a recovery through the registry, a dev sign-in that
// appends a real identity binding — exactly the acts apps.spec.js's walks
// prove end to end. This is arrival machinery, not a second proof.

// Console personas (mirrors frontend/apps/console/src/state.tsx): the console
// door offers only eSignet now, so console arrivals sign in programmatically —
// the same mint-and-bind the persona cards performed, with the session handed
// to the app through the key its provider restores from.
const CONSOLE_PERSONAS = {
  orgadmin: ["org", "Peter Otieno", "Org Admin"],
  configurator: ["org", "Dr. Alice Mutua", "Project Configurator"],
  author: ["org", "Amina Yusuf", "Work Definition Author"],
  approver: ["org", "Prof. Ndegwa", "Work Definition Approver"],
  rateowner: ["org", "Mutua", "Rate Owner"],
  payowner: ["org", "Njeri", "Payment Mechanism Owner"],
  instance: ["org", "Instance administrator", "Instance Admin"],
  custodian: ["custodian", "Otieno", "Registry Custodian"],
  support: ["custodian", "Naliaka", "Support Agent"],
  funder: ["org", "Funding oversight", "Funding Viewer"],
};
async function flowConsoleLogin(page, request, personaKey) {
  const row = CONSOLE_PERSONAS[personaKey];
  if (!row) return `unknown console persona "${personaKey}"`;
  const [fixKey, who, role] = row;
  const partyId = FLOW_FIX[fixKey];
  const token = await flowMintToken(request, partyId);
  const sub = "story|" + partyId.replace("did:crest:party:", "");
  const pw = await (await request.get(FLOW_API.oidc + "/dev/pairwise?sub=" + encodeURIComponent(sub))).json();
  const bind = await request.post(
    FLOW_API.parties + "/v1/parties/" + encodeURIComponent(partyId) + "/identity-bindings", {
      headers: { Authorization: "Bearer " + token, "Content-Type": "application/json" },
      data: { provider: "mock-oidc", providerClass: "generic-oidc", subjectRef: pw.subject },
    });
  if (!bind.ok()) return `the self-bind was refused for ${partyId} (${bind.status()})`;
  await page.goto("/console/");
  await page.evaluate(
    ([t, pid, w, r, k]) => sessionStorage.setItem("crest.console.session",
      JSON.stringify({ token: t, me: { partyId: pid, who: w, role: r }, persona: k })),
    [token, partyId, who, role, personaKey]);
  await page.reload();
  const signedIn = await page
    .locator(".appbar")
    .filter({ hasNotText: /Not signed in/i })
    .first()
    .waitFor({ state: "visible", timeout: 20000 })
    .then(() => true, () => false);
  if (!signedIn) return `persona ${personaKey} could not sign in within 20s (the door still says "Not signed in")`;
  await settle(page);
  return "";
}

// The worker door's login screen shows only the real pathways now, so flows
// sign in the way the dev card used to work underneath: a mock-issuer token,
// the same idempotent identity-binding append through the real endpoint, and
// the session handed to the door as a reload would find it.
async function flowWorkerLogin(page, request, who) {
  const partyId = who === "grace" ? FLOW_FIX.workerA : who === "supervisor" ? FLOW_FIX.supervisor : who;
  const token = await flowMintToken(request, partyId);
  const sub = "story|" + partyId.replace("did:crest:party:", "");
  const pw = await (await request.get(FLOW_API.oidc + "/dev/pairwise?sub=" + encodeURIComponent(sub))).json();
  const bind = await request.post(
    FLOW_API.parties + "/v1/parties/" + encodeURIComponent(partyId) + "/identity-bindings", {
      headers: { Authorization: "Bearer " + token, "Content-Type": "application/json" },
      data: { provider: "mock-oidc", providerClass: "generic-oidc", subjectRef: pw.subject },
    });
  if (!bind.ok()) return `the self-bind was refused for ${partyId} (${bind.status()})`;
  await page.goto("/worker/");
  await page.evaluate(
    ([t, me]) => sessionStorage.setItem("crest.worker.session", JSON.stringify({ token: t, me })),
    [token, partyId]);
  await page.reload();
  const ok = await page
    .locator("#logout")
    .waitFor({ state: "visible", timeout: 20000 })
    .then(() => true, () => false);
  if (!ok) return "the worker door did not accept the dev session within 20s";
  await settle(page);
  return "";
}

async function flowLand(page, hash) {
  await page.evaluate((h) => { location.hash = h; }, hash);
  await settle(page);
  await page.waitForTimeout(200);
}

// The verifier org asks to see more — the same request the presentation walk
// mints through the verify door, here through the service door directly.
async function flowMintShare(request) {
  const r = await flowAsPartyOn(request, FLOW_API.verification, FLOW_FIX.org, "POST",
    "/v1/presentation-requests", {
      subjectPartyId: FLOW_FIX.workerA,
      requestedByPartyId: FLOW_FIX.org,
      purpose: "Fidelity gate — hiring check " + flowStamp(),
    });
  if (r.status() !== 201)
    throw new Error(`the verifier's presentation request was refused (${r.status()})`);
  return (await r.json()).request.id;
}

async function flowWorkerArrive(page, request, mode) {
  if (mode === "share-pending") {
    // w1_19/w1_15 — a REQUESTED share, opened by its subject, with two lines
    // left ticked (the reference frame's own decision moment).
    const reqId = await flowMintShare(request);
    const p = await flowWorkerLogin(page, request, "grace");
    if (p) return p;
    await flowLand(page, `#/shares/${encodeURIComponent(reqId)}`);
    const boxes = page.locator("[data-dis] input[type=checkbox]");
    const n = await boxes.count();
    if (n < 2)
      return `the story gives the worker only ${n} disclosure line(s); the reference frame decides over two ticked`;
    for (let i = 2; i < n; i++) await boxes.nth(i).uncheck();
    await page.waitForTimeout(150);
    return "";
  }

  if (mode === "share-decided") {
    // w1_20 — the sent view of a share the subject decided with her own
    // bearer-checked voice: an approved strict subset where the list allows.
    const reqId = await flowMintShare(request);
    const g = await flowAsPartyOn(request, FLOW_API.verification, FLOW_FIX.workerA, "GET",
      `/v1/presentation-requests/${encodeURIComponent(reqId)}`);
    const list = ((await g.json()) || {}).disclosureList || [];
    if (!list.length) return "the disclosure list resolved empty — nothing to decide over";
    const keep = list.slice(0, Math.max(1, list.length - 1)).map((d) => d.credentialId);
    const d = await flowAsPartyOn(request, FLOW_API.verification, FLOW_FIX.workerA, "POST",
      `/v1/presentation-requests/${encodeURIComponent(reqId)}/decision`,
      { approve: true, approvedCredentialIds: keep });
    if (!d.ok()) return `the subject's decision was refused (${d.status()})`;
    const p = await flowWorkerLogin(page, request, "grace");
    if (p) return p;
    await flowLand(page, `#/shares/${encodeURIComponent(reqId)}/sent`);
    return "";
  }

  if (mode.startsWith("recovery-")) {
    // w4_1–w4_3 — an OPEN recovery routed to the signed-in nominee. The
    // nomination is the worker's own act; the opening is the custodian's (the
    // worker cannot authenticate, which is the premise). A party holds one
    // live recovery at a time, so a leftover OPEN one is reused, exactly as
    // the recovery walk does.
    await flowAsParty(request, FLOW_FIX.workerA, "POST",
      `/v1/parties/${FLOW_FIX.workerA}/recovery-contacts`,
      { contactPartyId: FLOW_FIX.supervisor }); // an existing nomination is fine
    let recId;
    const opened = await flowAsParty(request, FLOW_FIX.custodian, "POST", "/v1/recoveries", {
      partyId: FLOW_FIX.workerA, openedByPartyId: FLOW_FIX.custodian,
      reason: "lost phone — fidelity gate " + flowStamp(),
    });
    if (opened.status() === 201) {
      recId = (await opened.json()).id;
    } else {
      const l = await flowAsParty(request, FLOW_FIX.supervisor, "GET",
        `/v1/recoveries?confirmerPartyId=${FLOW_FIX.supervisor}`);
      const open = (((await l.json()) || {}).recoveries || [])
        .find((x) => x.partyId === FLOW_FIX.workerA && !x.completedAt);
      if (!open) return `no recovery could be opened (${opened.status()}) and none is open to reuse`;
      recId = open.id;
    }
    if (mode === "recovery-refused") {
      // A refusal already on the record (an earlier run's) serves the screen
      // just as well — the write is permanent by design, so no status check.
      await flowAsParty(request, FLOW_FIX.supervisor, "POST",
        `/v1/recoveries/${encodeURIComponent(recId)}/refusals`,
        { refuserPartyId: FLOW_FIX.supervisor, reason: "fidelity gate — could not be sure it was them" });
    }
    const p = await flowWorkerLogin(page, request, "supervisor");
    if (p) return p;
    await flowLand(page,
      mode === "recovery-confirmer" ? "#/vouch"
        : mode === "recovery-progress" ? `#/vouch/${encodeURIComponent(recId)}`
          : `#/vouch/${encodeURIComponent(recId)}/refused`);
    return "";
  }

  if (mode === "anchor-arrival") {
    // w1_17 — the moment belongs to the story's unanchored worker: her first
    // dev sign-in APPENDS the identity binding (the same first-login path an
    // eSignet anchor takes), and #/added reads the derived result live.
    const p = await flowWorkerLogin(page, request, FLOW_FIX.chandra);
    if (p) return p;
    await flowLand(page, "#/added");
    return "";
  }

  return `unknown flow arrival "flow:${mode}"`;
}

// Drives the onboarding flow to the named depth. Returns "" on success or a
// note saying what actually happened, like every other arrival.
async function flowArrive(page, request, mode) {
  if (/^(share-|recovery-|anchor-)/.test(mode)) return flowWorkerArrive(page, request, mode);
  const stamp = flowStamp();

  // g2_1 — register. Anonymous by design (#20); a unique organisation per
  // arrival so the run is re-runnable and screens never read another run's
  // state.
  await page.goto("/console/#/onboard");
  await settle(page);
  await page.fill('[name="orgname"]', "Fidelity Gate Trust " + stamp);
  await page.selectOption('[name="country"]', "KE");
  await page.fill('[name="workemail"]', `gate+${stamp}@fidelity.example.org`);
  await page.fill('[name="contactname"]', "Hon. Peter Okello");
  await page.click("#orgapplyform button.dominant");
  await page.waitForURL(/#\/onboard\/terms/, { timeout: 20000 });
  await settle(page);
  const orgId = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("crest.console.onboarding") || "{}").orgId);
  if (!/^did:crest:party:/.test(orgId || ""))
    return "registration did not put an orgId in the session";

  if (mode === "onboarding") return "";

  if (mode === "onboarding-terms") {
    // g2_11 lists "other sets you could ask for" only when more than one set
    // is published; publish one, then remount the terms screen so it reads
    // the full catalogue.
    await flowPublishWiderTerms(request);
    return "";
  }

  if (mode === "onboarding-invited") {
    await flowInvite(request, orgId);
    return "";
  }

  if (mode === "admissions-queue" || mode === "admissions-review") {
    // g4_1–g4_3 — the operator's side of the same door. The registration
    // above is the queue's real pending row; the reviewer is the instance
    // administrator, signed in through the door's own persona card — the same
    // first-login path every console: arrival takes. The token lives in
    // memory, so the arrival must never reload after signing in; it lands by
    // hash navigation, which mounts the screen fresh over live reads.
    {
      const p = await flowConsoleLogin(page, request, "instance");
      if (p) return p;
    }
    const h = mode === "admissions-queue"
      ? "#/admissions"
      : "#/admissions/" + encodeURIComponent(orgId);
    await page.evaluate((x) => { location.hash = x; }, h);
    await settle(page);
    await page.waitForTimeout(200);
    return "";
  }

  // Everything deeper walks Terms → Checks first (g2_11 → g2_12).
  await page.click("#requestterms");
  await page.waitForURL(/#\/onboard\/checks/, { timeout: 20000 });
  await settle(page);
  if (mode === "onboarding-terms-request") return "";

  // …then submits and lets the operator decide (never the applicant).
  await page.click("#submitchecks");
  await page.waitForURL(/#\/onboard\/status/, { timeout: 20000 });
  await settle(page);
  let r = await flowAsParty(request, FLOW_FIX.custodian, "POST",
    `/v1/organisations/${orgId}/decision`, { approve: true, decidedBy: FLOW_FIX.custodian });
  if (r.status() !== 200) return `the registration decision was refused (${r.status()})`;
  if (mode === "onboarding-approved") return "";

  if (mode === "onboarding-project") {
    // g2_10 — the accepted invitation and the grant it minted, read back.
    await flowInvite(request, orgId);
    await page.evaluate(() => { location.hash = "#/onboard/invited"; });
    await settle(page);
    await page.reload();
    await settle(page);
    await page.click("#acceptinvitation");
    await page.waitForURL(/#\/onboard\/project/, { timeout: 20000 });
    await settle(page);
    return "";
  }

  if (mode === "onboarding-wider-request" || mode === "onboarding-wider-submitted") {
    // g2_6 → g2_7 — a wider published set exists, is picked, and the request
    // opens (the service refuses it before approval, which is why this depth
    // sits after the decision).
    const widerId = await flowPublishWiderTerms(request);
    await page.evaluate(() => { location.hash = "#/onboard/wider"; });
    await settle(page);
    await page.reload();
    await settle(page);
    await page.click(`[data-terms="${widerId}@1"]`);
    await page.click("#requestwider");
    await page.waitForURL(/#\/onboard\/documents/, { timeout: 20000 });
    await settle(page);
    if (mode === "onboarding-wider-request") return "";

    // g2_7 → g2_8 — declared {kind, ref, hash} references, then submit.
    await page.fill('[name="dockind0"]', "registration-certificate");
    await page.fill('[name="docref0"]', "custody://fidelity-gate/reg-cert-" + stamp);
    await page.click("#submitrequest");
    await page.waitForURL(/#\/onboard\/review/, { timeout: 20000 });
    await settle(page);
    return "";
  }

  return `unknown flow arrival "flow:${mode}"`;
}

async function settle(page) {
  await page.waitForLoadState("networkidle").catch(() => {});
  await page.waitForSelector(".screen", { state: "visible" }).catch(() => {});
  await page.waitForTimeout(150);
}

// Every arrival returns a note saying what actually happened, so a screen the
// gate could not reach says so instead of failing on a missing label.
async function arrive(page, request, entry) {
  if (entry.arrive.startsWith("flow:")) {
    // A scripted-flow arrival drives the real flow before landing on the
    // route; any step that breaks is reported as what it is — an arrival
    // problem — never as a missing label on a screen the gate never reached.
    const mode = entry.arrive.slice("flow:".length);
    let problem;
    try {
      // The funders modes (#193) hold their console session in memory —
      // exactly like the admissions-queue/-review modes above — so they take
      // the target route themselves and land the page, never a post-return
      // reload that would sign the persona back out or reset the in-memory
      // selected project.
      problem = mode.startsWith("funders-")
        ? await flowFundersArrive(page, request, mode, entry.route)
        : await flowArrive(page, request, mode);
    } catch (e) {
      problem = String((e && e.message) || e);
    }
    if (problem) return problem;
    if (entry.route && !mode.startsWith("funders-")) {
      await page.evaluate((h) => { location.hash = h; }, entry.route);
      await settle(page);
      // Remount so the screen reads the registry's CURRENT state (the flow's
      // later writes — a decision, an invitation — happened after some
      // screens first mounted). sessionStorage survives the reload.
      await page.reload();
      await settle(page);
      await page.waitForTimeout(200);
    }
    return "";
  }
  const door = DOORS[entry.app];
  await page.goto(door);
  await settle(page);
  if (entry.arrive.startsWith("console:")) {
    const persona = entry.arrive.slice("console:".length);
    const p = await flowConsoleLogin(page, request, persona);
    if (p) return p;
  } else if (entry.arrive === "worker") {
    const p = await flowWorkerLogin(page, request, "grace");
    if (p) return p;
  } else if (entry.arrive === "field") {
    await page.click("[data-login]");
    await settle(page);
  } else if (entry.arrive !== "anon") {
    return `unknown arrive mode "${entry.arrive}"`;
  }
  if (entry.route) {
    await page.evaluate((h) => { location.hash = h; }, entry.route);
    await settle(page);
    await page.waitForTimeout(200);
  }
  return "";
}

// ── The assertions ─────────────────────────────────────────────────────────
// Each check appends to `failures` rather than throwing, so one run reports
// every facet of a screen instead of only the first broken one.
const RAIL_SEL = ".sidebar, .railnav, nav.rail, .bottomnav";

function waiverFor(sid, key) {
  const w = WAIVERS.screens[sid] || {};
  return w[key] || "";
}

async function checkScreen(page, sid, spec, failures, waived) {
  const body = fold(await page.locator("body").innerText());
  const need = (facet, needle, what) => {
    const wv = waiverFor(sid, `${facet}:${needle}`) || waiverFor(sid, facet);
    if (wv) { waived.push({ facet, needle, why: wv }); return; }
    if (!body.includes(fold(needle))) failures.push({ facet, needle, what });
  };

  // Layout idiom. A desktop-idiom reference frame is a console window: an
  // appbar and a rail. A phone frame is a card, and must not grow a desktop
  // rail on a 1280px viewport.
  const wvLayout = waiverFor(sid, "layout");
  if (wvLayout) {
    waived.push({ facet: "layout", needle: spec.layout, why: wvLayout });
  } else if (spec.layout === "desktop") {
    if (!(await page.locator(".appbar").isVisible().catch(() => false)))
      failures.push({ facet: "layout", needle: "appbar", what: "a desktop frame renders an appbar" });
    if (!(await page.locator(RAIL_SEL).first().isVisible().catch(() => false)))
      failures.push({ facet: "layout", needle: "rail", what: "a desktop frame renders a navigation rail" });
  } else {
    if (await page.locator(".sidebar").isVisible().catch(() => false))
      failures.push({ facet: "layout", needle: "phone card", what: "a phone frame must not render a desktop sidebar" });
  }

  // The appbar names the product, whoever is signed in.
  const product = (spec.appbar.match(/^CREST(?: Console| · Field)?/) || [""])[0];
  if (product) {
    const wv = waiverFor(sid, "appbar");
    if (wv) waived.push({ facet: "appbar", needle: product, why: wv });
    else if (!fold(await page.locator(".appbar").innerText().catch(() => "")).includes(fold(product)))
      failures.push({ facet: "appbar", needle: product, what: "the appbar names the product" });
  }

  // Titles are asserted only for our own design screens. Half the reference
  // frames title themselves with instance data ("PRJ-118 · Nakuru community
  // health", "Grace Wanjiku · W-4471"), which no real deployment reproduces;
  // asserting those would be asserting the fixture, not the design.
  if (spec.source === "crest-design" && spec.title) need("title", spec.title, "the screen's title");

  for (const f of spec.fields) if (f.label) need("fields", f.label, "a field the reference asks for");
  for (const c of spec.callouts) {
    if (c.title) need("callouts", c.title, "a callout the reference carries");
    if (c.text) need("callouts", c.text, "the callout's text, in full");
  }
  for (const b of spec.buttons) {
    if (b.role === "primary" || b.role === "secondary") {
      if (b.label) need("buttons", b.label, `the ${b.role} action`);
    }
  }
  if (spec.step.counter) need("counter", spec.step.counter, "the step counter");
  for (const r of spec.step.rail) if (r) need("rail", r, "a rail entry");

  // Forbidden content: the g2_1 class of bug.
  const rules = [
    ...(FORBIDDEN.screens[sid] || []),
    ...(spec.forbidden || []),
  ];
  for (const r of rules) {
    const key = `forbidden:${r.text || r.selector}`;
    const wv = waiverFor(sid, key) || waiverFor(sid, "forbidden");
    if (wv) { waived.push({ facet: "forbidden", needle: r.text || r.selector, why: wv }); continue; }
    if (r.selector) {
      if (await page.locator(r.selector).count())
        failures.push({ facet: "forbidden", needle: r.selector, what: r.why });
    } else if (r.text && body.includes(fold(r.text))) {
      failures.push({ facet: "forbidden", needle: r.text, what: r.why });
    }
  }
}

// ── The scope ──────────────────────────────────────────────────────────────
const inScope = Object.keys(MAP.screens).sort();

function record(row) {
  fs.appendFileSync(RESULTS, JSON.stringify(row) + "\n");
}

// Deliberately NOT serial: one screen's failure must not stop the rest from
// being judged. A gate that reports the first broken screen and stops is a
// gate you run once and then stop believing.
test.beforeAll(() => {
  // A screen the gate is meant to judge but has no spec entry for is a hole,
  // not a pass. Fail the whole run rather than quietly cover 20 of 29.
  const missing = inScope.filter((s) => !SPEC.screens[s]);
  expect(missing, "every in-scope screen has a journey-spec entry").toEqual([]);
  const unmapped = Object.keys(SPEC.screens).filter(
    (s) => (MAP.scope.journeys.includes(SPEC.screens[s].journey) ||
            MAP.scope.designScreens.includes(s)) && !MAP.screens[s]);
  expect(unmapped, "every in-scope spec screen has an arrival route").toEqual([]);
});

for (const sid of inScope) {
  const entry = MAP.screens[sid];
  const spec = SPEC.screens[sid];
  const led = STATUS[sid] || { status: "designed", note: "our design; not a reference screen" };
  const quarantine = QUARANTINE.screens[sid];

  test(`${sid} · ${spec ? spec.stage : "?"} · ${led.status}`, async ({ page, request }) => {
    // A flow arrival is several real round trips (register, decide, invite);
    // give it room rather than reading a slow stack as a broken screen.
    if (entry.arrive.startsWith("flow:")) test.setTimeout(180000);
    if (led.status !== "implemented") {
      const why = `status ${led.status} — ${led.note || "no surface claimed"}`;
      record({ screen: sid, verdict: "skipped", status: led.status, why });
      test.skip(true, why);
      return;
    }

    const failures = [];
    const waived = [];
    const problem = await arrive(page, request, entry);
    if (problem) failures.push({ facet: "arrival", needle: entry.route || "/", what: problem });
    else await checkScreen(page, sid, spec, failures, waived);

    if (quarantine) {
      record({ screen: sid, verdict: "quarantined", status: led.status,
               why: quarantine.why, issue: quarantine.issue, failures, waived });
      console.log(`QUARANTINED ${sid}: ${quarantine.issue} — ${quarantine.why}`);
      for (const f of failures) console.log(`  would fail: ${f.facet} · ${f.needle}`);
      // A quarantine that no longer hides anything is a lie of its own.
      expect(failures.length,
        `${sid} is quarantined (${quarantine.issue}) but now passes every facet — delete the quarantine entry`)
        .toBeGreaterThan(0);
      return;
    }

    record({ screen: sid, verdict: failures.length ? "failed" : "asserted",
             status: led.status, failures, waived });
    const lines = failures.map((f) => `  ${f.facet}: ${JSON.stringify(f.needle)} — ${f.what}`);
    expect(lines.join("\n"), `${sid} does not match its spec:\n${lines.join("\n")}`).toBe("");
  });
}
