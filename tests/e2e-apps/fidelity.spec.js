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
  project: "crest:context:01JCREST00000000000000PRJC",
};

const FLOW_API = (() => {
  const base = process.env.BASE_URL || "http://localhost:59110";
  const local = new URL(base).port === "59110";
  const host = new URL(base).hostname;
  return {
    parties: local ? `http://${host}:59000` : base.replace(/\/$/, "") + "/api/crest-registry",
    oidc: local ? `http://${host}:59103` : base.replace(/\/$/, "") + "/api/crest-mock-oidc",
  };
})();

async function flowMintToken(request, partyId) {
  const sub = "story|" + partyId.replace("did:crest:party:", "");
  const r = await request.post(FLOW_API.oidc + "/token", {
    data: { sub, aud: "crest", expiresIn: "1h" },
  });
  if (!r.ok()) throw new Error(`the dev issuer refused a token for ${partyId} (${r.status()})`);
  const d = await r.json();
  return d.accessToken || d.access_token || d.token;
}

async function flowAsParty(request, partyId, method, path, body) {
  const token = await flowMintToken(request, partyId);
  return request.fetch(FLOW_API.parties + path, {
    method,
    headers: { Authorization: "Bearer " + token, "Content-Type": "application/json" },
    data: body === undefined ? undefined : body,
  });
}

// A run must be re-runnable on the same stack (the G-2 walk learned this):
// every registration and every published terms set is unique per call.
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

// Drives the onboarding flow to the named depth. Returns "" on success or a
// note saying what actually happened, like every other arrival.
async function flowArrive(page, request, mode) {
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
    await page.goto("/console/");
    await settle(page);
    const card = page.locator('[data-persona="instance"]');
    if (!(await card.count())) return 'no console persona card [data-persona="instance"]';
    await card.click();
    const signedIn = await page
      .locator(".appbar")
      .filter({ hasNotText: /Not signed in/i })
      .first()
      .waitFor({ state: "visible", timeout: 20000 })
      .then(() => true, () => false);
    if (!signedIn)
      return 'persona instance could not sign in within 20s (the door still says "Not signed in")';
    await settle(page);
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
    let problem;
    try {
      problem = await flowArrive(page, request, entry.arrive.slice("flow:".length));
    } catch (e) {
      problem = String((e && e.message) || e);
    }
    if (problem) return problem;
    if (entry.route) {
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
    const card = page.locator(`[data-persona="${persona}"]`);
    if (!(await card.count())) return `no console persona card [data-persona="${persona}"]`;
    await card.click();
    await settle(page);
    // First login mints a token and appends an identity binding, which is two
    // round trips: poll the appbar rather than guess a sleep. A fixed wait
    // here reads a slow login as a broken one and quarantines a screen that
    // works — which is exactly the kind of false verdict that gets a gate
    // switched off.
    const signedIn = await page
      .locator(".appbar")
      .filter({ hasNotText: /Not signed in/i })
      .first()
      .waitFor({ state: "visible", timeout: 20000 })
      .then(() => true, () => false);
    if (!signedIn) {
      return `persona ${persona} could not sign in within 20s (the door still says "Not signed in")`;
    }
    await settle(page);
  } else if (entry.arrive === "worker") {
    await page.click("#login-grace");
    await settle(page);
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
