// A walk of every journey app against a story-seeded stack: every route
// renders real content with no JS exception and no API error banner, and the
// screens the story populates show the story's data. Run with the compose
// stack up and `SEED_STORY=true go run ./tools/seed` done (make apps-up), or
// BASE_URL pointed at the deployed door.
const { test, expect } = require("@playwright/test");

const FIX = {
  workerA: "did:crest:party:01JCREST00000000000000WRKA",
  org: "did:crest:party:01JCREST000000000000000RGN",
  custodian: "did:crest:party:01JCREST00000000000000CSTD",
  project: "crest:context:01JCREST00000000000000PRJC",
};

// Every page object carries its uncaught exceptions; .errbar is what the apps
// render when an API call fails. Both empty = the screen is honestly alive.
function watch(page) {
  const errors = [];
  page.on("pageerror", e => errors.push(String(e)));
  return errors;
}

async function settle(page) {
  await page.waitForLoadState("networkidle");
  // Every app renders its content inside a .screen — waiting for it beats a
  // fixed sleep; the short tail covers post-render async fills.
  await page.waitForSelector(".screen", { state: "visible" });
  await page.waitForTimeout(100);
}

async function assertAlive(page, errors, where) {
  expect(errors, `JS exceptions on ${where}`).toEqual([]);
  await expect(page.locator(".errbar"), `API error banner on ${where}`).toHaveCount(0);
  const text = await page.locator("body").innerText();
  expect(text.length, `content on ${where}`).toBeGreaterThan(80);
}

test("landing names the four apps", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/");
  await settle(page);
  for (const name of ["worker", "field", "console", "Verification"]) {
    await expect(page.locator("body")).toContainText(new RegExp(name, "i"));
  }
  // Four product doors + the design-docs card (#148).
  await expect(page.locator(".app-card")).toHaveCount(5);
  await assertAlive(page, errors, "landing");
});

test("worker app: every route, on real data", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/worker/");
  await settle(page);
  await page.click("#login-grace");
  await settle(page);
  // The worker door is a desktop console now: appbar + sidebar on wide
  // viewports; the bottom nav exists but shows only under 720px.
  await expect(page.locator(".appbar")).toBeVisible();
  await expect(page.locator(".sidebar")).toBeVisible();

  // Home: the two stat cards are real counts from the story.
  await expect(page.locator(".stat").first()).toBeVisible();
  const credStat = await page.locator(".stat .n").first().innerText();
  expect(Number(credStat), "Grace holds story credentials").toBeGreaterThan(0);

  const routes = ["#/work", "#/work/declined", "#/wallet", "#/wallet/share",
    "#/wallet/deferred", "#/pay", "#/profile", "#/profile/consents",
    "#/profile/checks", "#/profile/messages", "#/profile/recovery", "#/home"];
  for (const r of routes) {
    await page.evaluate(h => { location.hash = h; }, r);
    await settle(page);
    await assertAlive(page, errors, "worker " + r);
  }

  // Money: the story raised instructions, one of them held with a reason.
  await page.evaluate(() => { location.hash = "#/pay"; });
  await settle(page);
  const payText = await page.locator("body").innerText();
  expect(payText).toMatch(/KES|held|instruction|settled|released/i);
});

test("worker app: the held payment names its owner", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/worker/");
  await settle(page);
  await page.click("#login-grace");
  await settle(page);
  await page.evaluate(() => { location.hash = "#/pay"; });
  await settle(page);
  // The story seeds exactly one held instruction for Grace; its absence means
  // the stack is not story-seeded — fail loudly rather than skip quietly.
  await expect(page.locator(".held").first()).toContainText(/Waiting on/i);
  await assertAlive(page, errors, "worker held view");
});

test("enrolment app: every route", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/enrolment/");
  await settle(page);
  await page.click("[data-login]");
  await settle(page);
  const routes = ["#/registrations", "#/register", "#/consent", "#/roster",
    "#/toconfirm", "#/handoff"];
  for (const r of routes) {
    await page.evaluate(h => { location.hash = h; }, r);
    await settle(page);
    await assertAlive(page, errors, "enrolment " + r);
  }
  // The roster screen carries the canonical CSV header.
  await page.evaluate(() => { location.hash = "#/roster"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("outcome_value");
});

// Role-derived personas (docs/JOURNEY_GAP_ASSESSMENT.md finding 1): each
// session walks only its own reference flow's views. Together the walks still
// cover every console view.
//
// The J3 actors (orgadmin, configurator) navigate the reference's own three
// rails and hold every J3 route — nothing is hidden by role (finding F1), so
// both walk the same list.
const J3_VIEWS = ["org", "people", "projects", "projects/new", "workers", "validation",
  "intake", "definition", "paysetup", "sources", "status", "stp", "quality", "payments",
  "trace", "reports", "finance", "support"];

// Arrival, proven. The logged-OUT door also renders an .appbar, so asserting
// one is not proof of a session: a broken login would have passed. What only
// a signed-in console shows is the identity in the appbar and the logout
// control, so those are what the walk waits for.
async function signedInAs(page, who) {
  await expect(page.locator(".appbar .who-label")).toContainText(who);
  await expect(page.locator("#logout")).toBeVisible();
  await expect(page.locator("body")).not.toContainText("Sign in to CREST Console");
}

for (const [personaIdx, personaName, who, views] of [
  [0, "Peter Otieno (org admin, P-1)", "Peter Otieno", J3_VIEWS],
  [1, "Dr. Alice Mutua (configurator, P-2)", "Dr. Alice Mutua", J3_VIEWS],
  [2, "Amina Yusuf (definition author, P-3)", "Amina Yusuf", ["definework", "definition"]],
  [3, "Prof. Ndegwa (definition approver, P-3)", "Prof. Ndegwa", ["ratify", "definition"]],
  [4, "Mutua (rate owner, F-1)", "Mutua", ["paysetup"]],
  [5, "Njeri (payment mechanism owner, F-2)", "Njeri", ["paysetup"]],
  [6, "Instance administrator (G-1)", "Instance administrator", ["instance", "status"]],
  [7, "Otieno (registry custodian, G-4)", "Otieno", ["find", "dupes", "unclear", "recover", "review"]],
  [8, "Naliaka (support agent, W-3)", "Naliaka", ["cases", "supportfind", "supporttrace"]],
  [9, "Funding oversight (V-4)", "Funding oversight", ["portfolio", "status"]],
]) {
  test(`console: ${personaName} walks every view`, async ({ page }) => {
    const errors = watch(page);
    await page.goto("/console/");
    await settle(page);
    await page.click(`[data-p="${personaIdx}"]`);
    // First login mints a token and appends an identity binding — two round
    // trips. Poll for arrival rather than sleeping.
    await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
    await settle(page);
    await signedInAs(page, who);
    for (const v of views) {
      await page.evaluate(h => { location.hash = "#/" + h; }, v);
      await settle(page);
      await assertAlive(page, errors, `console #/` + v);
      // Still signed in, and still on the route asked for: a guard that
      // bounced us home would otherwise read as a passing walk.
      await signedInAs(page, who);
      expect(page.url(), `console #/${v} must not redirect away`).toContain("#/" + v);
      if (v === "instance") {
        // The instance view reads GET /v1/instance for real (#70); the local
        // stack's identity is CREST_INSTANCE_ID's compose default.
        await expect(page.locator("body")).toContainText("crest:instance:local");
      }
    }
  });
}

// n1 — the console door. Our design (docs/design/j3-connective-tissue): one
// door for every console role, and NO role selector, because a role is
// granted in the registry and read back, never self-declared.
test("console door: eSignet is the way in, and no role can be picked", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/console/");
  await settle(page);
  await expect(page.locator("#signin-title")).toContainText("Sign in to CREST Console");
  await expect(page.locator("#signin-esignet")).toBeVisible();
  await expect(page.locator("body")).toContainText("CREST never sees a credential of yours");
  // The rule this screen sets, verbatim from the design spec.
  await expect(page.locator("body")).toContainText(
    "It never asks which role you want, and never offers a role you do not hold.");
  // No role selector of any kind: no select, no radio, nothing named "role".
  await expect(page.locator("select")).toHaveCount(0);
  await expect(page.locator('input[type="radio"]')).toHaveCount(0);
  await expect(page.locator('[name*="role" i]')).toHaveCount(0);
  // The demo block is instance-configured: this local stack has a mock
  // issuer, so it renders — and each row names a role rather than offering
  // to grant one.
  await expect(page.locator('[data-panel="demo-personas"]')).toBeVisible();
  await assertAlive(page, errors, "console door");
});

// n3/F1 — one rail, two actors. The five setup entries render identically for
// the Org Admin and the Project Configurator, and no entry is removed by role.
test("console: the J3 rail is the same five entries for both actors", async ({ page }) => {
  const entries = ["Projects", "People & roles", "Work definitions", "Payment set up", "Workers"];
  for (const persona of ["orgadmin", "configurator"]) {
    await page.goto("/console/");
    await settle(page);
    await page.click(`[data-persona="${persona}"]`);
    await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
    await settle(page);
    const rail = await page.locator(".sidebar a").allInnerTexts();
    expect(rail.map(t => t.trim()), `${persona} sees the reference's setup rail`).toEqual(entries);
    // The dashboard section swaps the rail for the reference's other one —
    // per section, not per role (the F1 correction).
    await page.evaluate(() => { location.hash = "#/status"; });
    await settle(page);
    const dash = await page.locator(".sidebar a").allInnerTexts();
    expect(dash.map(t => t.trim())).toEqual(["Work status", "Quality", "Payments", "Proof", "Reports"]);
  }
});

// n5 — a rail entry you cannot act on stays visible and says who can. The
// Configurator opens People & roles and gets the boundary, not a redirect and
// not a missing entry.
test("console: People & roles guards the configurator instead of hiding", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/console/");
  await settle(page);
  await page.click('[data-persona="configurator"]');
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await expect(page.locator('.sidebar a[href*="people"]')).toHaveCount(1);
  await page.evaluate(() => { location.hash = "#/people"; });
  await settle(page);
  expect(page.url()).toContain("#/people");
  await expect(page.locator("body")).toContainText("People & roles is not yours to change");
  await expect(page.locator("body")).toContainText("Who can do this");
  // The guard's rule, verbatim from the design spec.
  await expect(page.locator("body")).toContainText(
    "A guard states the role you would need, names somebody who can grant it");
  // No HTTP status code is shown to the user.
  await expect(page.locator("body")).not.toContainText(/\b403\b/);
  await assertAlive(page, errors, "console role guard");
});

// Author/approver separation: an approver ratifies and can never open the
// authoring wizard — even a hand-typed #/definework bounces to their home.
test("console: the approver cannot reach the author's wizard", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/console/");
  await settle(page);
  await page.click('[data-persona="approver"]');
  await settle(page);
  // Their own flow renders.
  await expect(page.locator("body")).toContainText(/Ratify/i);
  // The sidebar carries no wizard entry…
  await expect(page.locator('.sidebar a[href*="definework"]')).toHaveCount(0);
  // …and typing the route lands back on the approver's home, not the wizard.
  await page.evaluate(() => { location.hash = "#/definework"; });
  await settle(page);
  expect(page.url()).not.toContain("definework");
  await expect(page.locator("body")).toContainText(/reads everything, drafts nothing/i);
  await assertAlive(page, errors, "console approver boundary");
});

// G-2 onboarding opens with the reference's six identity fields (g2_1).
test("console: onboarding asks the six identity fields", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/console/#/onboard");
  await settle(page);
  for (const field of ["orgname", "country", "workemail", "contactname", "orgkind", "orgsector"]) {
    await expect(page.locator(`#orgapplyform [name="${field}"]`),
      `onboarding field ${field}`).toHaveCount(1);
  }
  // The reference's green callout: registration documents are DELIBERATELY
  // not asked on this screen — so no such field may exist.
  await expect(page.locator('#orgapplyform [name="orgreg"]')).toHaveCount(0);
  await expect(page.locator("body")).toContainText(/deliberately not asked/i);
  // The kind is the branching answer, and the client-side gap is stated.
  await expect(page.locator("body")).toContainText(/kind of organisation/i);
  // Desktop console window, not a mobile card: appbar + step rail + counter.
  await expect(page.locator(".appbar")).toBeVisible();
  await expect(page.locator("#stepcounter")).toContainText("Registration · 1 of 4");
  await assertAlive(page, errors, "console onboarding form");
});

// #166: the six-field profile round-trips through the registry — the status
// view renders kind/sector/country/contact from GET .../registration, not
// from anything this browser held. Proven by clearing sessionStorage of the
// profile at submit time (the app no longer stores it) and asserting the
// server-served marker.
test("console: onboarding profile round-trips through the registry", async ({ page }) => {
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  await page.goto("/console/#/onboard");
  await settle(page);
  await page.fill('[name="orgname"]', "Roundtrip Trust " + stamp);
  await page.selectOption('[name="country"]', "UG");
  await page.fill('[name="workemail"]', `rt+${stamp}@example.org`);
  await page.fill('[name="contactname"]', "Round Tripper");
  await page.selectOption('[name="orgkind"]', "verifying");
  await page.selectOption('[name="orgsector"]', "education");
  await page.click("#orgapplyform button.dominant");
  await page.waitForSelector("#acceptterms", { timeout: 20000 });
  // The browser no longer holds the profile — what the status screen shows
  // can only have come back from the registry.
  const held = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("crest.console.onboarding") || "{}"));
  expect(held.kind, "profile must not be browser-held").toBeUndefined();
  expect(held.sector, "profile must not be browser-held").toBeUndefined();
  await page.click("#acceptterms");
  await page.waitForURL(/#\/onboard\/status/, { timeout: 20000 });
  await settle(page);
  await expect(page.locator("body")).toContainText("UG · verifying · education (served by the registry)");
  await expect(page.locator("body")).toContainText("Round Tripper");
  await assertAlive(page, errors, "onboarding round-trip");
});

// W-1 entry: two equal enrollment pathways, neither a fallback (w1_1).
test("worker entry presents both enrollment pathways", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/worker/");
  await settle(page);
  await expect(page.locator('[data-pathway="self"]')).toBeVisible();
  await expect(page.locator('[data-pathway="assisted"]')).toBeVisible();
  await expect(page.locator("body")).toContainText(/neither is a fallback/i);
  // The self path names the consent-before-record rule (w1_5).
  await expect(page.locator("body")).toContainText(/before any record is created/i);
  // The assisted pathway hands off to the field door.
  await expect(page.locator("#login-assisted")).toHaveAttribute("href", "/enrolment/");
  await assertAlive(page, errors, "worker entry pathways");
});

// w1_18: the show screen presents a QR (offline presentation), JSON behind a
// toggle — no raw JSON dump as the primary face.
test("worker wallet: show-to-someone renders a QR, JSON behind a toggle", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/worker/");
  await settle(page);
  await page.click("#login-grace");
  await settle(page);
  await page.evaluate(() => { location.hash = "#/wallet/0/show"; });
  await settle(page);
  // Either the QR image, or (for an oversized credential) the honest
  // PixelPass gap note — never a silent failure. The QR is drawn
  // asynchronously on the device, so wait for one of the two terminal states
  // rather than reading the "Drawing the code…" moment as a missing QR: on a
  // slow CI runner that race made this test fail for no product reason.
  await page.waitForFunction(
    () => !!document.querySelector("#cred-qr") || /PixelPass/i.test(document.body.innerText),
    null, { timeout: 15000 });
  const qr = page.locator("#cred-qr");
  if (await qr.count()) {
    await expect(qr).toBeVisible();
  } else {
    await expect(page.locator("body")).toContainText(/PixelPass/i);
  }
  // The JSON is present but folded.
  await expect(page.locator("#show-json")).toBeVisible();
  await expect(page.locator("details pre")).toBeHidden();
  await assertAlive(page, errors, "worker wallet QR show");
});

test("console: the story shows through", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/console/");
  await settle(page);
  await page.click('[data-p="1"]');
  await settle(page);
  // Sources: the story registered riverside-dhis2, and #117 is named on it.
  await page.evaluate(() => { location.hash = "#/sources"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("riverside-dhis2");
  await expect(page.locator("body")).toContainText("#117");
  // Payments: the story held one instruction; the view groups by reason.
  await page.evaluate(() => { location.hash = "#/payments"; });
  await settle(page);
  await expect(page.locator("body")).toContainText(/held|nothing_to_pay/i);
  await assertAlive(page, errors, "console story views");
});

test("verify app: a real check, refusals shown, batch bounded", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/verify/");
  await settle(page);

  // V-1: load Grace's newest credential and verify it, logged out.
  await page.evaluate(() => { location.hash = "#/v1_2"; });
  await settle(page);
  await page.click("#loadsample");
  await settle(page);
  await page.locator("#verifyform button.btn").last().click();
  await settle(page);
  await expect(page.locator("body")).toContainText(/verified|valid|yes/i);
  await assertAlive(page, errors, "verify v1_3");

  // Static + institutional routes all render.
  for (const r of ["#/v1_1", "#/v2_1", "#/v2_2", "#/v2_3", "#/person", "#/w6_1", "#/w6_2"]) {
    await page.evaluate(h => { location.hash = h; }, r);
    await settle(page);
    await assertAlive(page, errors, "verify " + r);
  }

  // Resolve a person: the whole chain, by party id.
  await page.evaluate(() => { location.hash = "#/person"; });
  await settle(page);
  await page.locator("#personform input").first().fill(FIX.workerA);
  await page.locator("#personform button.btn").last().click();
  await settle(page);
  await expect(page.locator("body")).toContainText(/credential/i);
});

test("mobile viewport: no horizontal overflow, console nav becomes a chip rail", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  for (const path of ["/worker/", "/enrolment/", "/console/", "/verify/"]) {
    await page.goto(path);
    await settle(page);
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, path + " must not scroll sideways").toBe(0);
  }
  // Console after login: the sidebar must have become the horizontal chip rail
  // (row direction), and the pane must still render without sideways scroll.
  await page.goto("/console/");
  await settle(page);
  await page.click('[data-p="0"]');
  await settle(page);
  const dir = await page.locator(".sidebar").evaluate(el => getComputedStyle(el).flexDirection);
  expect(dir).toBe("row");
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBe(0);
});

test("mobile viewport: the worker door keeps its bottom nav", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/worker/");
  await settle(page);
  await page.click("#login-grace");
  await settle(page);
  // Under 720px the worker's sidebar chip rail yields to the phone-style
  // bottom nav — the phone experience survives the desktop rebuild.
  await expect(page.locator(".bottomnav")).toBeVisible();
  await expect(page.locator(".sidebar")).toBeHidden();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBe(0);
});

// ── J3 Phase B: the project backend, driven through the screens ────────────
// One walk, because the facts are sequential: a project is created, handed
// over, declined with a reason, re-handed, accepted, composed, staffed,
// granted to a partner, financed, supported, gated and activated. Every
// assertion below reads a value back from the service through a screen — a
// pass means the record exists, not that a form submitted.
//
// This also closes the coverage the J3 backend PR marked partial: the project
// role listing, the organisation role listing, the APPROVED-only partner
// directory, the partner-grant listing, the partner-grant end-date refusal,
// and the support-owner party check are each exercised here.
test("console: the J3 handover is real, and so is everything after it", async ({ page }) => {
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  await page.goto("/console/");
  await settle(page);
  await page.click('[data-persona="orgadmin"]');
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);

  // The party this session acts as: the story's programme organisation, which
  // is the party every org-side persona signs in as (state.tsx records why).
  const me = "did:crest:party:01JCREST000000000000000RGN";

  // p1_3 — create a project and name a configurator. Naming is a proposal.
  await page.evaluate(() => { location.hash = "#/projects/new"; });
  await settle(page);
  await page.fill('[name="projectname"]', "Bednet campaign " + stamp);
  await page.fill('[name="coverage"]', "Ward 4, Ward 7 · Kisumu County");
  await page.fill('[name="configurator"]', me);
  await page.click("#create-project");
  await page.waitForURL(/#\/handover/, { timeout: 20000 });
  await settle(page);
  // n4 — what arrived, read from the record.
  await expect(page.locator("body")).toContainText("Bednet campaign " + stamp);
  await expect(page.locator("body")).toContainText("Ward 4, Ward 7");
  await expect(page.locator("body")).toContainText(/waiting on an answer/i);

  // n4's decline: a real outcome, with a reason and an actor, and nothing
  // deleted. F2's whole point.
  await page.fill('[name="declinereason"]', "not my programme area " + stamp);
  await page.click("#decline-handover");
  await settle(page);
  await expect(page.locator("body")).toContainText(/declined/i);
  await expect(page.locator("body")).toContainText("not my programme area " + stamp);
  // The trail keeps both events, which is the reason the trail exists.
  await expect(page.locator("body")).toContainText("NAMED");
  await expect(page.locator("body")).toContainText("DECLINED");

  // The Org Admin's queue: a declined project comes back rather than vanishing.
  await page.evaluate(() => { location.hash = "#/org"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Handed back to you");
  await expect(page.locator("body")).toContainText("not my programme area " + stamp);
  // …and is handed on again, which returns it to PENDING.
  await page.fill('[name="rehandto"]', me);
  await page.click("#rehand");
  await expect(page.locator("body")).toContainText(/waiting on an answer again/i, { timeout: 20000 });

  // Accept it, and the write door opens.
  await page.evaluate(() => { location.hash = "#/handover"; });
  await settle(page);
  await page.click("#accept-handover");
  await page.waitForURL(/#\/compose/, { timeout: 20000 });
  await settle(page);

  // p2_1/p2_3/p2_5 — one composition choice, recorded with its decider.
  await page.fill('[name="choice"]', "worker-sourcing");
  await page.fill('[name="value"]', "register-and-import-" + stamp);
  await page.locator("#composeform button").click();
  await expect(page.locator("body")).toContainText("register-and-import-" + stamp, { timeout: 20000 });
  await expect(page.locator("body")).toContainText("worker-sourcing");

  // p2_6 — a role grant on this project, listed with its grantor and date.
  await page.evaluate(() => { location.hash = "#/owners"; });
  await settle(page);
  await page.fill('[name="grantparty"]', me);
  await page.fill('[name="grantfunctions"]', "submit-work-evidence");
  await page.locator("#grantform button").click();
  await expect(page.locator("body")).toContainText("submit-work-evidence", { timeout: 20000 });
  await expect(page.locator("body")).toContainText("ACTIVE");

  // p2_17 — the directory is a join over approvals somebody else made, so the
  // approved programme organisation is in it.
  await page.evaluate(() => { location.hash = "#/partners"; });
  await settle(page);
  await expect(page.locator(`[data-pick="${me}"]`)).toBeVisible();

  // p2_18 — a grant with no end date is refused: "the grant lapses by itself"
  // is the screen's whole subject, so an endless one is not a grant.
  await page.fill('[name="grantpartner"]', me);
  await page.fill('[name="grantfns"]', "submit-work-evidence");
  await page.locator("#partnergrantform button").click();
  await expect(page.locator(".errbar")).toContainText(/end date|422|until|period/i, { timeout: 20000 });
  // With one, it stands — and rides the terms the partner actually accepted.
  await page.fill('[name="grantuntil"]', "2027-03-31");
  await page.locator("#partnergrantform button").click();
  await expect(page.locator("body")).toContainText(/lapses on its own end date/i, { timeout: 20000 });
  await expect(page.locator("body")).toContainText("Grants standing on this project");

  // p2_10 — a support owner must resolve to a real party: a support owner
  // nobody can name is the dead end that leaves a worker unexplained.
  await page.evaluate(() => { location.hash = "#/support"; });
  await settle(page);
  await page.fill('[name="supportparty"]', "did:crest:party:01JNOSUCHPARTY0000000000");
  await page.locator("#supportform button").click();
  await expect(page.locator(".errbar")).toBeVisible({ timeout: 20000 });
  await page.fill('[name="supportparty"]', me);
  await page.fill('[name="supportvalue"]', "+254700000" + stamp.slice(-3));
  await page.locator("#supportform button").click();
  await expect(page.locator("body")).toContainText("+254700000" + stamp.slice(-3), { timeout: 20000 });

  // p2_8 — the code is stored verbatim, with who linked it.
  await page.evaluate(() => { location.hash = "#/finance"; });
  await settle(page);
  await page.fill('[name="financesystem"]', "IFMIS · Kenya Treasury");
  await page.fill('[name="financecode"]', "4402-11-A" + stamp.slice(-2));
  await page.locator("#financeform button").click();
  await expect(page.locator("body")).toContainText("4402-11-A" + stamp.slice(-2), { timeout: 20000 });

  // p2_7 — a gate is declared, refuses activation by name, then is satisfied.
  await page.evaluate(() => { location.hash = "#/activate"; });
  await settle(page);
  await page.fill('[name="gatename"]', "rate-published");
  await page.locator("#gateform button").click();
  await expect(page.locator("body")).toContainText("rate-published", { timeout: 20000 });
  await page.click("#activate-project");
  // The refusal names what is missing rather than being a dead end.
  await expect(page.locator(".errbar")).toContainText(/rate-published|unmet|condition|409/i, { timeout: 20000 });
  await page.click('[data-satisfy="rate-published"]');
  // Satisfied at the service's clock: the row now carries a date, which is
  // the only proof that the gate was answered rather than re-declared.
  await expect(page.locator('[data-satisfy="rate-published"]')).toHaveCount(0, { timeout: 20000 });
  await page.click("#activate-project");
  await expect(page.locator("body")).toContainText("ACTIVE", { timeout: 20000 });

  // n2 — the context list is read from granted roles, and the project we just
  // built is in it.
  await page.evaluate(() => { location.hash = "#/where"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Bednet campaign " + stamp);
  await assertAlive(page, errors, "console J3 phase B walk");
});

// ── G-2: the whole onboarding journey, driven through the screens ───────────
// register → standalone → invited → decline-with-reason → re-invite →
// terms-as-request → checks → operator decision → accept → the project →
// wider terms → documents → sent for review → withdraw → resubmit → recorded
// checks → approved. Every assertion reads a value back from the registry
// through a screen, and the walk asserts the signed-in identity (the appbar's
// who-label), not just that an appbar exists.
//
// The acts that are NOT the organisation's — a project sending an invitation,
// the operator deciding, a reviewer recording a check verdict — go through
// the same service doors as bearer-authenticated API calls by the party who
// holds that act (the fixture organisation owns the seeded project; the
// custodian decides), exactly as the seeder and a real deployment would.
const G2 = (() => {
  const base = process.env.BASE_URL || "http://localhost:59110";
  const local = new URL(base).port === "59110";
  const host = new URL(base).hostname;
  return {
    parties: local ? `http://${host}:59000` : base.replace(/\/$/, "") + "/api/crest-registry",
    oidc: local ? `http://${host}:59103` : base.replace(/\/$/, "") + "/api/crest-mock-oidc",
  };
})();

async function mintToken(request, partyId) {
  const sub = "story|" + partyId.replace("did:crest:party:", "");
  const r = await request.post(G2.oidc + "/token", {
    data: { sub, aud: "crest", expiresIn: "1h" },
  });
  expect(r.ok(), "the dev issuer mints a token for " + partyId).toBeTruthy();
  const d = await r.json();
  return d.accessToken || d.access_token || d.token;
}

async function asParty(request, partyId, method, path, body) {
  const token = await mintToken(request, partyId);
  const r = await request.fetch(G2.parties + path, {
    method,
    headers: { Authorization: "Bearer " + token, "Content-Type": "application/json" },
    data: body === undefined ? undefined : body,
  });
  return r;
}

test("console: the G-2 onboarding journey is real, screen by screen", async ({ page, request }) => {
  test.setTimeout(180000);
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  const contact = "Hon. Wangari Otieno";

  // g2_1 — register. The application is anonymous by design (#20).
  await page.goto("/console/#/onboard");
  await settle(page);
  await page.fill('[name="orgname"]', "Lakeside Health Trust " + stamp);
  await page.selectOption('[name="country"]', "KE");
  await page.fill('[name="workemail"]', `w.otieno+${stamp}@lakeside.example.org`);
  await page.fill('[name="contactname"]', contact);
  await page.click("#orgapplyform button.dominant");
  await page.waitForURL(/#\/onboard\/terms/, { timeout: 20000 });
  await settle(page);
  const orgId = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("crest.console.onboarding") || "{}").orgId);
  expect(orgId, "the registry answered with the organisation's id").toMatch(/^did:crest:party:/);

  // g2_5 — the registration stands alone: the screen signs the session in AS
  // the organisation and reads its real (empty) invitation inbox.
  await page.evaluate(() => { location.hash = "#/onboard/standalone"; });
  await settle(page);
  await expect(page.locator(".appbar .who-label")).toContainText(contact);
  await expect(page.locator(".appbar .who-label")).toContainText("Onboarding Authorising Signatory");
  await expect(page.locator("body")).toContainText("Sitting here uninvited is normal, not stalled");
  await expect(page.locator("body")).toContainText("Still being decided:");
  await assertAlive(page, errors, "g2_5 standalone");

  // A project invites the new organisation — the fixture org owns the seeded
  // project and sends the offer through the real endpoint.
  const invite = () =>
    asParty(request, FIX.org, "POST", `/v1/projects/${FIX.project}/invitations`, {
      partyId: orgId,
      functions: ["submit-work-evidence"],
      period: { start: "2026-09-01T00:00:00Z", end: "2027-03-31T00:00:00Z" },
      note: "Nakuru community health, wards 4 and 7",
    });
  let r = await invite();
  expect(r.status(), "the project's offer is recorded").toBe(201);

  // g2_9 — the inbox shows the offer; accepting BEFORE the registry decided
  // is refused with "not yet", and the offer does not expire with the wait.
  await page.evaluate(() => { location.hash = "#/onboard/invited"; });
  await settle(page);
  await expect(page.locator(".appbar .who-label")).toContainText(contact);
  await expect(page.locator("body")).toContainText("One waiting. You did not apply for this and did not need to.");
  await expect(page.locator("body")).toContainText("submit-work-evidence");
  await page.click("#acceptinvitation");
  await expect(page.locator("body")).toContainText(/Not yet, not no/i);
  await expect(page.locator(".errbar")).toHaveCount(0);

  // Declining requires a reason — first without one (refused on-screen), then
  // with one (recorded by the service, shown back from the read).
  await page.click("#declineinvitation");
  await expect(page.locator(".errbar")).toContainText(/reason/i);
  await page.fill('[name="declineinvreason"]', "outside our ward coverage " + stamp);
  await page.click("#declineinvitation");
  await expect(page.locator("body")).toContainText("declined — outside our ward coverage " + stamp, { timeout: 20000 });

  // The project re-invites; the walk continues on the fresh offer.
  r = await invite();
  expect(r.status(), "a second offer after a decline").toBe(201);

  // g2_11 — "Request these terms" records the acceptance and walks to the
  // checks screen (g2_12), which is the Certificates step, honestly empty of
  // verdicts (no automated checker exists; nothing pretends one ran).
  await page.evaluate(() => { location.hash = "#/onboard/terms"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Request these terms");
  await page.click("#requestterms");
  await page.waitForURL(/#\/onboard\/checks/, { timeout: 20000 });
  await settle(page);
  await expect(page.locator("#stepcounter")).toContainText("Registration · 3 of 4");
  await expect(page.locator("body")).toContainText("Read the third row");
  await expect(page.locator("body")).toContainText(/No check verdict has been recorded yet/i);
  await page.click("#submitchecks");
  await page.waitForURL(/#\/onboard\/status/, { timeout: 20000 });
  await settle(page);
  await expect(page.locator("body")).toContainText(/TERMS_ACCEPTED|PENDING/);

  // The operator decides — never the organisation itself. The custodian is
  // this stack's named decider.
  r = await asParty(request, FIX.custodian, "POST", `/v1/organisations/${orgId}/decision`, {
    approve: true, decidedBy: FIX.custodian,
  });
  expect(r.status(), "the registration decision").toBe(200);

  // g2_9 → g2_10 — the acceptance now stands, and creates the grant in the
  // same transaction; the project screen reads it back.
  await page.evaluate(() => { location.hash = "#/onboard/invited"; });
  await settle(page);
  await page.click("#acceptinvitation");
  await page.waitForURL(/#\/onboard\/project/, { timeout: 20000 });
  await settle(page);
  await expect(page.locator(".appbar .who-label")).toContainText(contact);
  await expect(page.locator("body")).toContainText("You are on crest:context:");
  await expect(page.locator("body")).toContainText("crest:authorization:");
  await expect(page.locator("body")).toContainText("To 31 March 2027");
  await expect(page.locator("body")).toContainText("Three clocks, and only one of them runs out");
  await assertAlive(page, errors, "g2_10 project");

  // The operator publishes a wider terms set (the same open door the seeder
  // uses); the organisation can now ask to move to it.
  // Unique per run: a re-run against the same stack must not find its own
  // earlier set already published (and possibly already accepted at
  // registration, since the terms screen offers the newest published set).
  const widerId = "crest:terms:01JCRESTG2WDERT" + stamp + "PAY00";
  r = await request.post(G2.parties + "/v1/terms", {
    data: {
      id: widerId, version: 1, name: "Full delivery with payment",
      permissions: ["submit-work-evidence", "specify-definition", "ratify-definition", "set-rates", "instruct-payment"],
      publishedAt: "2026-09-01T00:00:00Z",
    },
  });
  expect(r.status(), "the operator publishes the wider set").toBe(201);

  // g2_6 — the wider set is a real published object, picked and requested.
  await page.evaluate(() => { location.hash = "#/onboard/wider"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("You need wider terms to put your name to something");
  await expect(page.locator("body")).toContainText("Full delivery with payment, version 1");
  await page.click(`[data-terms="${widerId}@1"]`);
  await page.click("#requestwider");
  await page.waitForURL(/#\/onboard\/documents/, { timeout: 20000 });
  await settle(page);

  // g2_7 — documents are DECLARED references, never uploads: no file input
  // exists on the screen, and the declaration round-trips through the draft.
  await expect(page.locator("body")).toContainText("What we need to see");
  await expect(page.locator('input[type="file"]')).toHaveCount(0);
  await page.fill('[name="dockind0"]', "registration-certificate");
  await page.fill('[name="docref0"]', "custody://lakeside/reg-cert-" + stamp);
  await page.fill('[name="dochash0"]', "sha256:deadbeef" + stamp);
  await page.click("#savedraft");
  await expect(page.locator("body")).toContainText(/Draft saved — 1 document reference/i, { timeout: 20000 });
  await page.click("#submitrequest");
  await page.waitForURL(/#\/onboard\/review/, { timeout: 20000 });
  await settle(page);

  // g2_8 — sent for review, promoted from compressed: the real request state,
  // and Withdraw is the organisation's own real act.
  await expect(page.locator("body")).toContainText("Sent for review");
  await expect(page.locator("body")).toContainText("What is not blocked");
  await expect(page.locator(".stat .n", { hasText: "Open" })).toBeVisible();
  await page.click("#withdrawrequest");
  await expect(page.locator(".stat .n", { hasText: "WITHDRAWN" })).toBeVisible({ timeout: 20000 });

  // Asking again is a new request — the old answer survives as its own record.
  await page.evaluate(() => { location.hash = "#/onboard/wider"; });
  await settle(page);
  await page.click(`[data-terms="${widerId}@1"]`);
  await page.click("#requestwider");
  await page.waitForURL(/#\/onboard\/documents/, { timeout: 20000 });
  await settle(page);
  await page.fill('[name="dockind0"]', "registration-certificate");
  await page.fill('[name="docref0"]', "custody://lakeside/reg-cert-" + stamp);
  await page.click("#submitrequest");
  await page.waitForURL(/#\/onboard\/review/, { timeout: 20000 });
  await settle(page);
  const requestId = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("crest.console.onboarding") || "{}").requestId);
  expect(requestId).toMatch(/^crest:terms-request:/);

  // A check is a RECORDED VERDICT with a named owner — the reviewer records
  // it through the real door, and the screen shows verdict and owner.
  r = await asParty(request, FIX.custodian, "POST", `/v1/terms-requests/${requestId}/checks`, {
    name: "business-register", outcome: "PASS", ownerKind: "party", recordedBy: FIX.custodian,
    note: "confirmed against the Business Registration Service",
  });
  expect(r.status(), "the check verdict is recorded").toBe(201);
  await page.evaluate(() => { location.hash = "#/onboard/review"; });
  await page.reload();
  await settle(page);
  await expect(page.locator("body")).toContainText("business-register");
  await expect(page.locator("body")).toContainText("PASS — " + FIX.custodian);

  // The decision is a named person's, never the applicant's; approval moves
  // the registration to the requested terms in the same transaction.
  r = await asParty(request, FIX.custodian, "POST", `/v1/terms-requests/${requestId}/decision`, {
    approve: true, decidedBy: FIX.custodian,
  });
  expect(r.status(), "the terms request decision").toBe(200);
  await page.reload();
  await settle(page);
  await expect(page.locator(".stat .n", { hasText: "APPROVED" })).toBeVisible({ timeout: 20000 });
  await expect(page.locator("body")).toContainText("Decided by " + FIX.custodian);

  // The registration now shows the wider terms — read from the registry.
  await page.evaluate(() => { location.hash = "#/onboard/status"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("APPROVED");
  await expect(page.locator("body")).toContainText(widerId + " v1");
  await assertAlive(page, errors, "G-2 whole journey");
});
