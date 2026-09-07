// A walk of every journey app against a story-seeded stack: every route
// renders real content with no JS exception and no API error banner, and the
// screens the story populates show the story's data. Run with the compose
// stack up and `SEED_STORY=true go run ./tools/seed` done (make apps-up), or
// BASE_URL pointed at the deployed door.
const crypto = require("crypto");
const { test, expect } = require("@playwright/test");

function mutationKey(actor, method, path, body) {
  return "e2e-" + crypto.createHash("sha256")
    .update(JSON.stringify([actor, method.toUpperCase(), path, body === undefined ? null : body]))
    .digest("hex");
}

function isMutation(method) {
  return ["POST", "PUT", "PATCH", "DELETE"].includes(method.toUpperCase());
}

const FIX = {
  workerA: "did:crest:party:01JCREST00000000000000WRKA",
  workerC: "did:crest:party:01JCREST00000000000000WRKC",
  org: "did:crest:party:01JCREST000000000000000RGN",
  custodian: "did:crest:party:01JCREST00000000000000CSTD",
  supervisor: "did:crest:party:01JCREST00000000000000SPVR",
  specifier: "did:crest:party:01JCREST00000000000000SPEC",
  project: "crest:context:01JCREST00000000000000PRJC",
};

// The public seeder creates people through the registration API, so person
// ids are deployment-assigned. Their OIDC subjects remain stable fixture
// subjects: minting `story|<runtime id>` would authenticate nobody. Resolve
// the runtime ids once from /auth/me and then keep the rest of this file's
// request bodies readable in terms of FIX.
const FIXTURE_IDS = Object.freeze({ ...FIX });
const FIXTURE_SUBJECTS = {
  workerA: "story|01JCREST00000000000000WRKA",
  workerC: "story|01JCREST00000000000000WRKC",
  org: "story|01JCREST000000000000000RGN",
  custodian: "story|01JCREST00000000000000CSTD",
  supervisor: "story|01JCREST00000000000000SPVR",
  // The definition author is enrolled by the seed with a separate subject.
  specifier: "seed|specifier",
};
let runtimeReady;

// Resolve server-assigned IDs before any test constructs a request payload.
test.beforeEach(async ({ request }) => { await ensureRuntime(request); });

function fixtureKeyForParty(partyId) {
  return Object.keys(FIXTURE_IDS).find(key =>
    FIX[key] === partyId || FIXTURE_IDS[key] === partyId);
}

function providerSubject(partyId) {
  const key = fixtureKeyForParty(partyId);
  if (key && FIXTURE_SUBJECTS[key]) return FIXTURE_SUBJECTS[key];
  return "story|" + partyId.replace("did:crest:party:", "");
}

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

test("worker app: every route, on real data", async ({ page, request }) => {
  const errors = watch(page);
  await workerSignIn(page, request, FIX.workerA, "Grace \u00b7 community health worker");
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
    "#/profile/checks", "#/profile/messages", "#/profile/recovery",
    "#/shares", "#/vouch", "#/added", "#/home"];
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

test("worker app: the held payment names its owner", async ({ page, request }) => {
  const errors = watch(page);
  await workerSignIn(page, request, FIX.workerA, "Grace \u00b7 community health worker");
  await settle(page);
  await page.evaluate(() => { location.hash = "#/pay"; });
  await settle(page);
  // The story seeds exactly one held instruction for Grace; its absence means
  // the stack is not story-seeded — fail loudly rather than skip quietly.
  await expect(page.locator(".held").first()).toContainText(/Waiting on/i);
  await assertAlive(page, errors, "worker held view");
});

// The enrolment door no longer assumes a project: it offers a chooser only
// when the agent's grants name more than one context, and picks the single
// one otherwise. These walks are about the story's project, so choose it
// when a choice is offered and trust the door when it is not.
async function pickStoryProject(page) {
  const sel = page.locator("#context-select");
  if (await sel.count() && await sel.isVisible() && await sel.isEnabled()) {
    await sel.selectOption(FIX.project).catch(() => {});
  }
}

test("enrolment app: every route", async ({ page, request }) => {
  const errors = watch(page);
  await ensureRuntime(request);
  await page.goto("/enrolment/");
  await settle(page);
  await page.click("[data-login]");
  await settle(page);
  await pickStoryProject(page);
  await settle(page);
  const routes = ["#/registrations", "#/register", "#/confidence", "#/consent",
    "#/roster", "#/toconfirm", "#/handoff"];
  for (const r of routes) {
    await page.evaluate(h => { location.hash = h; }, r);
    await settle(page);
    await assertAlive(page, errors, "enrolment " + r);
  }
  // The roster screen carries the canonical CSV header.
  await page.evaluate(() => { location.hash = "#/roster"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("outcome_value");
  // J8 / W-4 (w3_1/w3_5): the worklist names the auto-resolve rule, and the
  // handoff draws the attestation chain with its no-person-signs close.
  await page.evaluate(() => { location.hash = "#/toconfirm"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("auto-confirms");
  await page.evaluate(() => { location.hash = "#/handoff"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Only where the project configured a manual step");
  await expect(page.locator("body")).toContainText("Many projects will have no human here at all");
  // w2_1 — the day's two real counters, from this device's own state.
  await page.evaluate(() => { location.hash = "#/registrations"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("done today");
  await expect(page.locator("body")).toContainText("to sync");
  // w2_3 — the consent script carries ownership and per-use consent, and the
  // record sentence names what the recording captures.
  await page.evaluate(() => { location.hash = "#/consent"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("nobody can see it unless you agree, each time");
  await expect(page.locator("body")).toContainText("your agent ID and the time. This is the consent record.");
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
  // P-3, the author: the whole wizard. Walked with no draft open, so each
  // screen renders its "no draft is open" state — which is itself worth
  // asserting alive, because it is what an author sees before they start and
  // the one state that must not look like a broken form. The wizard driven
  // against a real draft is its own test below.
  [2, "Amina Yusuf (definition author, P-3)", "Amina Yusuf",
    ["definework", "definition", "define/sector", "define/counting", "define/category",
      "define/unit", "define/cascade", "define/period", "define/outcome", "define/parties",
      "define/evidence", "define/source", "define/template", "define/adaptors", "define/mapping",
      "define/connect", "define/dryrun", "define/live", "define/validation", "define/payment",
      "define/roles", "define/tranches", "define/rules", "define/extend", "define/open",
      "define/anatomy", "handoff"]],
  [3, "Prof. Ndegwa (definition approver, P-3)", "Prof. Ndegwa",
    // #/ratified is the reference's confirmation panel (p3_16), drawn
    // without the shell; the wizard walk below proves it.
    ["ratify", "definition", "define/anatomy"]],
  [4, "Nadia Okoth (rate owner, F-1)", "Nadia Okoth",
    ["paysetup", "rateowner", "rate", "ratepublish", "ratestanding"]],
  [5, "Daniel Mwangi (payment mechanism owner, F-2)", "Daniel Mwangi",
    ["paysetup", "mech/test", "mech/recon", "mech/statement", "mech/batching",
      "mech/activate", "mech/qualify", "mech/live"]],
  [6, "Instance administrator (G-1)", "Instance administrator",
    ["instance", "instance/setup", "instance/covers", "instance/consent", "instance/invite",
      "instance/services", "instance/people", "status", "receipt"]],
  [7, "Otieno (registry custodian, G-4)", "Otieno",
    ["admissions", "find", "coverage", "registry-quality", "dupes", "reuse", "unclear", "recover", "review"]],
  [8, "Naliaka (support agent, W-3)", "Naliaka", ["cases", "supportfind", "supporttrace"]],
  [9, "Funding oversight (V-4)", "Funding oversight", ["portfolio", "status"]],
]) {
  test(`console: ${personaName} walks every view`, async ({ page, request }) => {
    const errors = watch(page);
    await page.goto("/console/");
    await settle(page);
    await consoleSignIn(page, request, CONSOLE_PERSONA_ORDER[personaIdx]);
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
      if (v === "instance/covers") {
        // g1_2 reads the deployment's real self-description, and says on its
        // face that these values are deploy-time configuration.
        await expect(page.locator("body")).toContainText("crest:instance:local");
        await expect(page.locator("body")).toContainText(/deploy-time/i);
      }
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
  // The rule stands without stating itself: no role selector of any kind —
  // no select, no radio, nothing named "role".
  await expect(page.locator("select")).toHaveCount(0);
  await expect(page.locator('input[type="radio"]')).toHaveCount(0);
  await expect(page.locator('[name*="role" i]')).toHaveCount(0);
  // The demo persona rows are gone: eSignet is the only door, on every
  // instance. Only the sign-in panel renders.
  await expect(page.locator('[data-panel="signin-with"]')).toBeVisible();
  await expect(page.locator('[data-panel="demo-personas"]')).toHaveCount(0);
  await assertAlive(page, errors, "console door");
});

// n3/F1 — one rail, two actors. The five setup entries render identically for
// the Org Admin and the Project Configurator, and no entry is removed by role.
test("console: the J3 rail is the same five entries for both actors", async ({ page, request }) => {
  const entries = ["Projects", "People & roles", "Work definitions", "Payment set up", "Workers"];
  for (const persona of ["orgadmin", "configurator"]) {
    await page.goto("/console/");
    await settle(page);
    await consoleSignIn(page, request, persona);
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
test("console: People & roles guards the configurator instead of hiding", async ({ page, request }) => {
  const errors = watch(page);
  await page.goto("/console/");
  await settle(page);
  await consoleSignIn(page, request, "configurator");
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
test("console: the approver cannot reach the author's wizard", async ({ page, request }) => {
  const errors = watch(page);
  await page.goto("/console/");
  await settle(page);
  await consoleSignIn(page, request, "approver");
  // Their own flow renders.
  await expect(page.locator("body")).toContainText(/Review and sign/i);
  // The sidebar carries no wizard entry…
  await expect(page.locator('.sidebar a[href*="definework"]')).toHaveCount(0);
  // …and typing the route lands back on the approver's home, not the wizard.
  await page.evaluate(() => { location.hash = "#/definework"; });
  await settle(page);
  expect(page.url()).not.toContain("definework");
  await expect(page.locator("body")).toContainText(/Review and sign/i);
  // Every wizard section, not just its front door: an approver who could
  // reach one PUT could reach them all.
  for (const wiz of ["define/sector", "define/counting", "define/unit", "define/evidence",
    "define/dryrun", "define/payment", "define/extend"]) {
    await page.evaluate(h => { location.hash = "#/" + h; }, wiz);
    await settle(page);
    expect(page.url(), `approver must not reach #/${wiz}`).not.toContain(wiz);
  }
  // And the pricing handoff is the author's act, not the approver's.
  await page.evaluate(() => { location.hash = "#/handoff"; });
  await settle(page);
  expect(page.url()).not.toContain("handoff");
  await expect(page.locator("body")).toContainText(/There is no authoring screen in this navigation/i);
  await assertAlive(page, errors, "console approver boundary");
});

// The mirror of the boundary above: the author drafts and cannot ratify. Not
// only because the navigation lacks the entry — the definitions service
// refuses a version whose ratifier is its author, and the two personas are
// two parties so that the refusal is real rather than assumed.
test("console: the author cannot reach the approver's signature", async ({ page, request }) => {
  const errors = watch(page);
  await consoleSignIn(page, request, "author");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await expect(page.locator('.sidebar a[href*="ratify"]')).toHaveCount(0);
  await page.evaluate(() => { location.hash = "#/ratify"; });
  await settle(page);
  expect(page.url()).not.toContain("ratify");
  await assertAlive(page, errors, "console author boundary");
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

test("console: a verified stranger returns to applicant onboarding without a persona", async ({ page, request }) => {
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  const token = await mintSubjectToken(request, "apps-auth-onboard|" + stamp, "onboarding applicant");

  await page.goto("/console/#/onboard");
  await settle(page);
  await page.evaluate(() => sessionStorage.setItem("crest.console.applicant-return", "/onboard"));
  await page.goto("/console/#/auth?token=" + encodeURIComponent(token));
  await settle(page);

  await expect(page.url()).toContain("#/onboard");
  await expect(page.locator("#orgapplyform")).toBeVisible();
  await expect(page.locator("#onboard-esignet")).toHaveCount(0);
  expect(await page.evaluate(() => sessionStorage.getItem("crest.console.session"))).toBeNull();

  await page.fill('[name="orgname"]', "Authenticated Applicant Trust " + stamp);
  await page.fill('[name="workemail"]', `auth-onboard+${stamp}@example.org`);
  await page.fill('[name="contactname"]', "Verified Applicant");
  await page.click("#orgapplyform button.dominant");
  await page.waitForURL(/#\/onboard\/terms/, { timeout: 20000 });
  await settle(page);
  expect(await page.evaluate(() => Boolean(sessionStorage.getItem("crest.console.applicant-session")))).toBeTruthy();
  await assertAlive(page, errors, "authenticated applicant onboarding");
});

// #166: the six-field profile round-trips through the registry — the status
// view renders kind/sector/country/contact from GET .../registration, not
// from anything this browser held. Proven by clearing sessionStorage of the
// profile at submit time (the app no longer stores it) and asserting the
// server-served marker.
test("console: onboarding profile round-trips through the registry", async ({ page, request }) => {
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  await applicantSignIn(page, request, "apps-roundtrip|" + stamp, "Round Tripper");
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

// The worker screens carry the W-1 pack's copy (J7): the fork's same-DID
// promise, the record/rate separation, the burden-of-proof rule, the payment
// buckets, and the show-face's disclosure limits — anatomy and explanation
// from the frames, every number from live records.
test("worker screens carry the w1 pack's copy (J7)", async ({ page, request }) => {
  const errors = watch(page);
  await page.goto("/worker/");
  await settle(page);
  await expect(page.locator("body")).toContainText("Both pathways issue the same Crest DID");
  await expect(page.locator("body")).toContainText("Section 11, Choice One");
  await expect(page.locator("body")).toContainText("Only the result of the check is kept");

  await workerSignIn(page, request, FIX.workerA, "Grace · community health worker");
  await settle(page);
  await page.evaluate(() => { location.hash = "#/work"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("The rate lives in a separate record from the definition");
  await page.evaluate(() => { location.hash = "#/work/declined"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("The third question matters most");

  await page.evaluate(() => { location.hash = "#/pay"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Arrived in your account");
  await expect(page.locator("body")).toContainText("Still being checked");
  await expect(page.locator("body")).toContainText("their number is on your profile");

  await page.evaluate(() => { location.hash = "#/wallet/0/show"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("They will not see your name, your number, or where you live");
  await expect(page.locator("body")).toContainText("they have to ask, and you can say no");

  await page.evaluate(() => { location.hash = "#/profile"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Your work history accumulates against it");
  await assertAlive(page, errors, "worker w1 pack copy");
});

// w1_18: the show screen presents a QR (offline presentation), JSON behind a
// toggle — no raw JSON dump as the primary face.
test("worker wallet: show-to-someone renders a QR, JSON behind a toggle", async ({ page, request }) => {
  const errors = watch(page);
  await workerSignIn(page, request, FIX.workerA, "Grace \u00b7 community health worker");
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

test("console: the story shows through", async ({ page, request }) => {
  const errors = watch(page);
  await page.goto("/console/");
  await settle(page);
  await consoleSignIn(page, request, "configurator");
  // Sources: the story registered riverside-dhis2, and #117 is named on it.
  await page.evaluate(() => { location.hash = "#/sources"; });
  await page.waitForFunction(() => location.hash === "#/sources");
  await settle(page);
  await expect(page.locator("body")).toContainText("riverside-dhis2");
  await expect(page.locator("body")).toContainText("#117");
  // Payments: the story held one instruction; the view groups by reason.
  await page.evaluate(() => { location.hash = "#/payments"; });
  await settle(page);
  await expect(page.locator("body")).toContainText(/held|nothing_to_pay/i);
  await assertAlive(page, errors, "console story views");
});

test("verify app: a real check, refusals shown, batch bounded", async ({ page, request }) => {
  const errors = watch(page);
  await ensureRuntime(request);
  await page.goto("/verify/");
  await settle(page);

  // The person's credential history is private. Read it first as the worker,
  // then hand the same signed document to the logged-out V-1 scanner, exactly
  // as a verifier would receive it from a card or wallet.
  const workerCreds = await asPartyOn(
    request,
    SVC.verification,
    FIX.workerA,
    "GET",
    `/v1/parties/${encodeURIComponent(FIX.workerA)}/credentials`,
  );
  expect(workerCreds.status(), "the worker can read their own credential chain").toBe(200);
  const workerCredential = ((await workerCreds.json()).credentials || [])[0];
  expect(workerCredential, "the worker read returns a credential to present").toBeTruthy();

  // V-1: verify Grace's credential while logged out.
  await page.evaluate(() => { location.hash = "#/v1_2"; });
  await settle(page);
  await page.locator("#verifyform textarea").fill(JSON.stringify(workerCredential));
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

  // J8 / P-10: the attestation pack's copy on the institutional panels.
  await page.evaluate(() => { location.hash = "#/w6_1"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Your institute holds no standing account");
  await expect(page.locator("body")).toContainText("what makes it worth more than the worker's own word");
  await expect(page.locator("body")).toContainText("the project contact is named on your invitation");
  await page.evaluate(() => { location.hash = "#/w6_2"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Tier count is baked into every credential already issued");
  await expect(page.locator("body")).toContainText("Deciding late is expensive");

  // An institution cannot read a worker's private history by party id. The
  // worker presents the signed document (or grants a per-share request), and
  // the institution verifies that document under its own caller identity.
  await verifyOrgSignIn(page, request, FIX.org);
  await page.evaluate(() => { location.hash = "#/person"; });
  await settle(page);
  await page.locator("#personform textarea").fill(JSON.stringify(workerCredential));
  await page.locator("#personform button.btn").last().click();
  await settle(page);
  await expect(page.locator("#personout")).toContainText(/valid/i);
});

test("mobile viewport: no horizontal overflow, console nav becomes a chip rail", async ({ page, request }) => {
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
  await consoleSignIn(page, request, "orgadmin");
  const dir = await page.locator(".sidebar").evaluate(el => getComputedStyle(el).flexDirection);
  expect(dir).toBe("row");
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBe(0);
});

test("mobile viewport: the worker door keeps its bottom nav", async ({ page, request }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await workerSignIn(page, request, FIX.workerA, "Grace \u00b7 community health worker");
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
test("console: the J3 handover is real, and so is everything after it", async ({ page, request }) => {
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  await page.goto("/console/");
  await settle(page);
  await consoleSignIn(page, request, "orgadmin");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);

  // The party this session acts as: the story's programme organisation, which
  // is the party every org-side persona signs in as (state.tsx records why).
  const me = FIX.org;

  // p1_3 — create a project and name a configurator. Naming is a proposal.
  await page.evaluate(() => { location.hash = "#/projects/new"; });
  await settle(page);
  // p1_3 — creation decides nothing compositional, and says so on its face.
  await expect(page.locator("body")).toContainText("belong to whoever configures the project, not to whoever creates it");
  await expect(page.locator("body")).toContainText("Cannot exist until the project is composed");
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

  // p2_1 — one capability answered, recorded with its decider: a switch flip
  // writes the same composition choice the old free-text form used to take.
  const cap = page.locator('[data-capability="Set up payment"]');
  await expect(cap).toHaveAttribute("aria-pressed", "true");
  await cap.click();
  await expect(cap).toHaveAttribute("aria-pressed", "false", { timeout: 20000 });

  // p2_5 — the payment posture, recorded as a composition choice: picking a
  // card narrows it, and the record survives a reload.
  await expect(page.locator("body")).toContainText("Three genuinely different postures. The third is not a failure state.");
  await page.locator('.optcard:has-text("Calculation only")').click();
  await expect(page.locator('.optcard:has-text("Calculation only")')).toHaveAttribute("aria-pressed", "true", { timeout: 20000 });
  await expect(page.locator("body")).toContainText("Pay rate is a separate object from the work definition");

  // p2_6 — a role grant on this project, listed with its grantor and date.
  await page.evaluate(() => { location.hash = "#/owners"; });
  await settle(page);
  await page.fill('[name="grantparty"]', me);
  await page.fill('[name="grantfunctions"]', "submit-work-evidence");
  await page.locator("#grantform button").click();
  await expect(page.locator("body")).toContainText("submit-work-evidence", { timeout: 20000 });
  await expect(page.locator("body")).toContainText("ACTIVE");
  // p2_6 — the rate and the mechanism are separate on purpose, on the frame.
  await expect(page.locator("body")).toContainText("separate roles on purpose");

  // p2_17 — the directory is a join over approvals somebody else made, so the
  // approved programme organisation is in it.
  await page.evaluate(() => { location.hash = "#/partners"; });
  await settle(page);
  await expect(page.locator(`[data-pick="${me}"]`)).toBeVisible();
  // p2_18 — the terms are the ceiling, and the frame says so before the form.
  await expect(page.locator("body")).toContainText("Their terms are the ceiling.");

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
  // p2_10 — the checklist is derived from the record, and the one item no
  // contract exists for stays off with the true reason on its face.
  await expect(page.locator('.li:has-text("Contact route published to workers"):not(.off)')).toBeVisible();
  await expect(page.locator('.li.off:has-text("Response expectation agreed")')).toContainText("no service standard exists yet");

  // p2_8 — the code is stored verbatim, with who linked it.
  await page.evaluate(() => { location.hash = "#/finance"; });
  await settle(page);
  // p2_8 — both ways in stay on screen; the pull card says why it cannot run.
  await expect(page.locator('.optcard.na:has-text("Pull from the finance system")')).toContainText("not available");
  await expect(page.locator('.optcard:has-text("Enter the codes by hand")')).toContainText("a mistake surfaces at reconciliation");
  await page.fill('[name="financesystem"]', "IFMIS · Kenya Treasury");
  await page.fill('[name="financecode"]', "4402-11-A" + stamp.slice(-2));
  await page.locator("#financeform button").click();
  await expect(page.locator("body")).toContainText("4402-11-A" + stamp.slice(-2), { timeout: 20000 });

  // p2_7 — a gate is declared, refuses activation by name, then is satisfied.
  await page.evaluate(() => { location.hash = "#/activate"; });
  await settle(page);
  // p2_7 — what stays optional and what never is, on the frame.
  await expect(page.locator("body")).toContainText("before a signed work definition exists");
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
  await ensureRuntime(request);
  const sub = providerSubject(partyId);
  return mintSubjectToken(request, sub, partyId);
}

async function mintSubjectToken(request, sub, label) {
  const r = await request.post(G2.oidc + "/token", {
    data: { sub, aud: "crest", expiresIn: "1h" },
  });
  expect(r.ok(), "the dev issuer mints a token for " + (label || sub)).toBeTruthy();
  const d = await r.json();
  return d.accessToken || d.access_token || d.token;
}

async function ensureRuntime(request) {
  if (runtimeReady) return runtimeReady;
  runtimeReady = (async () => {
    for (const key of ["org", "specifier", "custodian", "supervisor", "workerA"]) {
      const token = await mintSubjectToken(request, FIXTURE_SUBJECTS[key], key);
      const r = await request.get(G2.parties + "/v1/auth/me", {
        headers: { Authorization: "Bearer " + token },
      });
      expect(r.ok(), `story seed binds ${key} to an authenticated party`).toBeTruthy();
      const me = await r.json();
      expect(me.partyId, `story seed returns a runtime party id for ${key}`).toBeTruthy();
      FIX[key] = me.partyId;
    }
    const token = await mintSubjectToken(request, FIXTURE_SUBJECTS.org, "org");
    const r = await request.get(
      G2.parties + "/v1/projects?ownerPartyId=" + encodeURIComponent(FIX.org),
      { headers: { Authorization: "Bearer " + token } },
    );
    expect(r.ok(), "the seeded organisation can list its projects").toBeTruthy();
    const projects = (await r.json()).projects || [];
    const project = projects.find(p => p.name === "Riverside bednet campaign 2026");
    expect(project && project.id, "the seed exposes a runtime project id").toBeTruthy();
    FIX.project = project.id;
    const workerCToken = await mintSubjectToken(request, FIXTURE_SUBJECTS.custodian, "custodian");
    const resolved = await request.get(
      G2.parties + "/v1/resolve?kind=contact-route&value=" +
        encodeURIComponent("worker-chandra@riverside.invalid") +
        "&contextId=" + encodeURIComponent(FIX.project),
      { headers: { Authorization: "Bearer " + workerCToken } },
    );
    expect(resolved.ok(), "the custodian resolves the unbound Chandra fixture").toBeTruthy();
    FIX.workerC = (await resolved.json()).partyId;
    expect(FIX.workerC, "the Chandra resolution returns a runtime party id").toBeTruthy();
    SPVR = FIX.supervisor;
    CHANDRA = FIX.workerC;
    if (typeof P3 !== "undefined") {
      P3.author = FIX.specifier;
      P3.approver = FIX.org;
    }
  })();
  return runtimeReady;
}

// The console door offers only eSignet now — no persona cards. Existing
// enrolled fixture personas use the real token-and-binding path below; the
// applicant path uses the actual callback and its applicant-only session.
const CONSOLE_PERSONAS = {
  orgadmin: ["org", "Peter Otieno", "Org Admin"],
  configurator: ["org", "Dr. Alice Mutua", "Project Configurator"],
  author: ["specifier", "Amina Yusuf", "Work Definition Author"],
  approver: ["org", "Prof. Ndegwa", "Work Definition Approver"],
  rateowner: ["org", "Nadia Okoth", "Rate Owner"],
  payowner: ["org", "Daniel Mwangi", "Payment Mechanism Owner"],
  instance: ["org", "Instance administrator", "Instance Admin"],
  custodian: ["custodian", "Otieno", "Registry Custodian"],
  support: ["custodian", "Naliaka", "Support Agent"],
  funder: ["org", "Funding oversight", "Funding Viewer"],
};
const CONSOLE_PERSONA_ORDER = Object.keys(CONSOLE_PERSONAS);
async function consoleSignIn(page, request, personaKey) {
  const [fixKey, who, role] = CONSOLE_PERSONAS[personaKey];
  await ensureRuntime(request);
  const partyId = FIX[fixKey];
  const token = await mintToken(request, partyId);
  const sub = providerSubject(partyId);
  const pw = await (await request.get(G2.oidc + "/dev/pairwise?sub=" + encodeURIComponent(sub))).json();
  const bindPath = "/v1/parties/" + encodeURIComponent(partyId) + "/identity-bindings";
  const bind = await request.post(
    G2.parties + bindPath, {
      headers: {
        Authorization: "Bearer " + token, "Content-Type": "application/json",
      },
      data: { provider: "mock-oidc", providerClass: "generic-oidc", subjectRef: pw.subject },
    });
  expect(bind.ok(), "the self-bind is accepted for " + partyId).toBeTruthy();
  await page.goto("/console/");
  await page.evaluate(
    ([t, pid, w, r, k]) => sessionStorage.setItem("crest.console.session",
      JSON.stringify({ token: t, me: { partyId: pid, who: w, role: r }, persona: k })),
    [token, partyId, who, role, personaKey]);
  await page.reload();
  await settle(page);
}

// The dev-login card is gone from the worker door (the login screen shows only
// the real pathways), so tests sign in the way the card used to: mint a token
// from the mock issuer, append the same idempotent identity binding through
// the real endpoint, and hand the session to the app the way a reload would
// find it. Same acts, no test-only UI.
async function workerSignIn(page, request, partyId, label) {
  await ensureRuntime(request);
  partyId = FIX[fixtureKeyForParty(partyId)] || partyId;
  const token = await mintToken(request, partyId);
  const sub = providerSubject(partyId);
  const pw = await (await request.get(G2.oidc + "/dev/pairwise?sub=" + encodeURIComponent(sub))).json();
  const bindPath = "/v1/parties/" + encodeURIComponent(partyId) + "/identity-bindings";
  const bind = await request.post(
    G2.parties + bindPath, {
      headers: {
        Authorization: "Bearer " + token, "Content-Type": "application/json",
      },
      data: { provider: "mock-oidc", providerClass: "generic-oidc", subjectRef: pw.subject },
    });
  expect(bind.ok(), "the self-bind is accepted for " + partyId).toBeTruthy();
  await page.goto("/worker/");
  await page.evaluate(
    ([t, me, lb]) => sessionStorage.setItem("crest.worker.session", JSON.stringify({ token: t, me, label: lb })),
    [token, partyId, label]);
  await page.reload();
  await settle(page);
}

// The verifier's institutional surfaces are private too. Drive the same
// callback the configured provider uses so request creation/listing carries an
// enrolled organisation bearer instead of relying on a stale page token.
async function verifyOrgSignIn(page, request, partyId) {
  await ensureRuntime(request);
  // The same idempotent self-bind every other door's sign-in performs: the
  // callback resolves a party only for a subject the registry has bound.
  const bound = await bindParty(request, partyId);
  expect(bound.ok(), "the verifier's self-bind is accepted for " + partyId).toBeTruthy();
  const token = await mintToken(request, partyId);
  await page.goto("/verify/#/auth?token=" + encodeURIComponent(token));
  await settle(page);
  await expect(page.locator(".appbar .who-label")).toContainText(/onboarded verifier/i);
}

async function applicantSignIn(page, request, sub, label) {
  const token = await mintSubjectToken(request, sub, label);
  await page.goto("/console/#/onboard");
  await settle(page);
  await page.evaluate(() => sessionStorage.setItem("crest.console.applicant-return", "/onboard"));
  await page.goto("/console/#/auth?token=" + encodeURIComponent(token));
  await settle(page);
  await expect(page.url()).toContain("#/onboard");
}

async function asParty(request, partyId, method, path, body) {
  await ensureRuntime(request);
  partyId = FIX[fixtureKeyForParty(partyId)] || partyId;
  const token = await mintToken(request, partyId);
  const headers = { Authorization: "Bearer " + token, "Content-Type": "application/json" };
  if (isMutation(method)) headers["Idempotency-Key"] = mutationKey(partyId, method, path, body);
  const r = await request.fetch(G2.parties + path, {
    method,
    headers,
    data: body === undefined ? undefined : body,
  });
  return r;
}

test("console: the G-2 onboarding journey is real, screen by screen", async ({ page, request }) => {
  test.setTimeout(180000);
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  const contact = "Hon. Wangari Otieno";

  // g2_1 — register. The form is open to an applicant, but the registry
  // still requires the applicant's verified bearer token.
  await applicantSignIn(page, request, "apps-onboard-applicant|" + stamp, contact);
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
  await expect(page.locator("body")).toContainText(/a check is a verdict with a named owner/i);
  await page.click("#submitchecks");
  await page.waitForURL(/#\/onboard\/status/, { timeout: 20000 });
  await settle(page);
  await expect(page.locator("body")).toContainText(/TERMS_ACCEPTED|Pending/i);

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
  // Terms are the operator's to publish: the fixture organisation is this
  // stack's operator, and an unsigned caller is refused.
  r = await asParty(request, FIX.org, "POST", "/v1/terms", {
    id: widerId, version: 1, name: "Full delivery with payment",
    permissions: ["submit-work-evidence", "specify-definition", "ratify-definition", "set-rates", "instruct-payment"],
    publishedAt: "2026-09-01T00:00:00Z",
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

// ── G-1: the instance walk, and the requests a person has to look at ────────
// An organisation registers and accepts terms at the open door; the instance
// administrator then walks the G-1 screens (every one an honest read of the
// deployment, never a fake wizard write) and decides the registration through
// the real reviewer surface (g4_1–g4_3). The decision path's invariant is
// verification's generalisation of "every held payment has a reason with an
// owner": every approval carries a decider — the authenticated caller, checked
// by the service (#89), never a typed name — and every rejection a reason.
test("console: G-1 walks the instance, and a person decides the admission", async ({ page, request }) => {
  test.setTimeout(180000);
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  const orgName = "Nyanza Care Collective " + stamp;

  // The applicant's half: register and accept terms (manual approval model
  // leaves the registration TERMS_ACCEPTED, waiting on a person). The
  // registration endpoint requires the applicant's verified bearer token.
  await applicantSignIn(page, request, "apps-admissions-applicant|" + stamp, "Sister Achieng");
  await page.fill('[name="orgname"]', orgName);
  await page.selectOption('[name="country"]', "KE");
  await page.fill('[name="workemail"]', `admissions+${stamp}@nyanza.example.org`);
  await page.fill('[name="contactname"]', "Sister Achieng");
  await page.click("#orgapplyform button.dominant");
  await page.waitForSelector("#acceptterms", { timeout: 20000 });
  await page.click("#acceptterms");
  await page.waitForURL(/#\/onboard\/status/, { timeout: 20000 });
  const orgId = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("crest.console.onboarding") || "{}").orgId);
  expect(orgId).toMatch(/^did:crest:party:/);

  // The instance administrator signs in — a different party from the
  // applicant, which is what lets the deployment walk stand. Admission
  // review is a separate registry-custodian session.
  await page.goto("/console/");
  await settle(page);
  await consoleSignIn(page, request, "instance");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await signedInAs(page, "Instance administrator");

  // g1_1 — the front door: what the deployment IS, and no fake wizard.
  await page.evaluate(() => { location.hash = "#/instance/setup"; });
  await settle(page);
  await expect(page.locator("body")).toContainText(/Let.s set up CREST/);
  await expect(page.locator("body")).toContainText(/read live from GET \/v1\/instance/i);
  await assertAlive(page, errors, "g1_1 setup front door");

  // g1_2 — read-only coverage, walked by the frame's own Begin button.
  await page.click("#g1-begin");
  await settle(page);
  expect(page.url()).toContain("#/instance/covers");
  await expect(page.locator("body")).toContainText("What this instance covers");
  // No editable instance field exists: read-only by design, and said so.
  await expect(page.locator('input[name="instancename"]')).toHaveCount(0);
  await expect(page.locator("body")).toContainText(/has nothing to save/i);
  await expect(page.locator("body")).toContainText("crest:instance:local");

  // g1_3 — the consent floor, stated as the infrastructure facts it is.
  await page.click("#g1-next");
  await settle(page);
  expect(page.url()).toContain("#/instance/consent");
  await expect(page.locator("body")).toContainText("Consent rules, before the first worker");
  await expect(page.locator("body")).toContainText(/never unwinds a payment already made/i);

  // g1_5 — the invite frame refuses to fake a send, and names the finding.
  await page.click("#g1-next");
  await settle(page);
  expect(page.url()).toContain("#/instance/invite");
  await expect(page.locator("body")).toContainText("Inviting the first organisation");
  await expect(page.locator("body")).toContainText(/No Send button, on purpose/i);
  await expect(page.locator("body")).toContainText(/design finding/i);

  // g1_6 — the live health sweep: six services, each really asked.
  await page.click("#g1-next");
  await settle(page);
  expect(page.url()).toContain("#/instance/services");
  await expect(page.locator("body")).toContainText("The services behind all of it");
  await expect(page.locator(".stat", { hasText: "parties" })).toContainText("healthy");
  await expect(page.locator(".stat")).toHaveCount(6);

  // The instance walk ends by handing the pending decision to the registry
  // custodian; the instance administrator cannot open the queue.
  await page.click("#g1-next");
  await settle(page);
  expect(page.url()).toContain("#/instance/people");
  await expect(page.locator("body")).toContainText("Nothing at the instance level grants anything");

  await page.click("#logout");
  await settle(page);
  await consoleSignIn(page, request, "custodian");
  await expect(page.locator("#logout")).toBeVisible();
  await expect(page.locator("body")).toContainText("Otieno");
  await page.evaluate(() => { location.hash = "#/admissions"; });
  await settle(page);
  // The assigned registry custodian owns this queue and the decision below.
  await expect(page.locator("body")).toContainText("Requests a person has to look at");
  // The reference's callouts, verbatim.
  await expect(page.locator("body")).toContainText("Verifiers are not in this queue and never will be.");
  await expect(page.locator("body")).toContainText(orgName);

  // g4_2 — open this request: the declared facts and the honest limit.
  await page.click(`[data-open="${orgId}"]`);
  await settle(page);
  expect(page.url()).toContain("#/admissions/");
  await expect(page.locator("body")).toContainText(orgName);
  await expect(page.locator("body")).toContainText("Sister Achieng");
  await expect(page.locator("body")).toContainText(
    "Neither it nor the domain check establishes that the person submitting this speaks for that body");

  // g4_3 — a rejection with no reason is refused by the service, loudly.
  await page.click("#reject-registration");
  await expect(page.locator(".errbar")).toContainText(/reason/i, { timeout: 20000 });

  // Approve. The decider is the session's own authenticated party — the
  // service checks the bearer token against the name (#89).
  await page.fill('[name="decidereason"]', "confirmed against the state register " + stamp);
  await page.click("#approve-registration");
  await expect(page.locator("body")).toContainText("APPROVED", { timeout: 20000 });
  await expect(page.locator("body")).toContainText("decided by");
  // The decider on the record is the registry custodian, not the applicant or
  // the instance operator.
  await expect(page.locator("body")).toContainText("…" + FIX.custodian.slice(-6));

  // The queue no longer holds it; the decided table does, with the decider.
  await page.evaluate(() => { location.hash = "#/admissions"; });
  await settle(page);
  await expect(page.locator(`[data-open="${orgId}"]`)).toHaveCount(0);
  await expect(page.locator("tr", { hasText: orgName })).toContainText("APPROVED");
  await assertAlive(page, errors, "G-1 admission decided");
});

// ── The workers wave: consent per share, recovery, and the no-document route ─
// (reference w1_19, w1_15, w1_20, w1_7, w4_1–w4_3, w1_4, w1_17; PR #189's
// backend, driven end to end through the doors.)
//
// Invariants these walks hold:
//  - consent: nothing is shared before the worker's per-share approval — a
//    collect on an undecided request is asserted to be REFUSED; an approval
//    releases exactly the approved subset, once.
//  - derived-not-stored strength: the anchor test asserts the verifier's
//    weakest-assurance caveat disappears on re-check with nothing rewritten.
//  - no raw identifiers: every pick and nomination is a party id; no screen
//    here takes a national ID or a phone number as an identity.

const SVC = (() => {
  const base = process.env.BASE_URL || "http://localhost:59110";
  const local = new URL(base).port === "59110";
  const host = new URL(base).hostname;
  return {
    verification: local ? `http://${host}:59000` : base.replace(/\/$/, "") + "/api/crest-verification",
  };
})();

// asParty against an arbitrary service base (asParty above is parties-only).
async function asPartyOn(request, svcBase, partyId, method, path, body) {
  await ensureRuntime(request);
  partyId = FIX[fixtureKeyForParty(partyId)] || partyId;
  const token = await mintToken(request, partyId);
  const headers = { Authorization: "Bearer " + token, "Content-Type": "application/json" };
  if (isMutation(method)) headers["Idempotency-Key"] = mutationKey(partyId, method, path, body);
  return request.fetch(svcBase + path, {
    method,
    headers,
    data: body === undefined ? undefined : body,
  });
}

async function asTokenOn(request, svcBase, token, actor, method, path, body) {
  const headers = { Authorization: "Bearer " + token, "Content-Type": "application/json" };
  if (isMutation(method)) headers["Idempotency-Key"] = mutationKey(actor, method, path, body);
  return request.fetch(svcBase + path, {
    method,
    headers,
    data: body === undefined ? undefined : body,
  });
}

// First-login self-bind for a party the story never bound — the exact append
// the browser's dev login performs, done from the test for parties that act
// only through the API here.
async function bindParty(request, partyId) {
  await ensureRuntime(request);
  partyId = FIX[fixtureKeyForParty(partyId)] || partyId;
  const token = await mintToken(request, partyId);
  const sub = providerSubject(partyId);
  const pw = await request
    .get(G2.oidc + "/dev/pairwise?sub=" + encodeURIComponent(sub))
    .then(r => r.json());
  const bindPath = `/v1/parties/${encodeURIComponent(partyId)}/identity-bindings`;
  const bindBody = { provider: "mock-oidc", providerClass: "generic-oidc", subjectRef: pw.subject };
  return request.post(G2.parties + bindPath, {
    headers: {
      Authorization: "Bearer " + token, "Content-Type": "application/json",
    },
    data: bindBody,
  });
}

let SPVR = FIX.supervisor;
let CHANDRA = FIX.workerC;
const TERM = "crest:terms:01JCREST00000000000000TERM";
const ULID32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const fakeUlid = () =>
  "01" + Array.from({ length: 24 }, () => ULID32[Math.floor(Math.random() * 32)]).join("");

test("per-share consent: the presentation loop on both faces, and the decline path", async ({ page, request }) => {
  test.setTimeout(240000);
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);

  // The verifier asks, from the verify door's institutional surface. Establish
  // the org bearer explicitly; private request reads are never anonymous.
  await verifyOrgSignIn(page, request, FIX.org);
  await page.evaluate(() => { location.hash = "#/requests"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Ask to see more");
  await page.fill('[name="sharesubject"]', FIX.workerA);
  await page.fill('[name="sharepurpose"]', "Hiring for a private clinic " + stamp);
  await page.click("#share-create");
  const card = page.locator("[data-vshare]", { hasText: stamp }).first();
  await expect(card).toContainText("REQUESTED", { timeout: 20000 });
  const reqId = await card.getAttribute("data-vshare");

  // CONSENT, structurally: collecting before the worker decides is refused —
  // by the service, shown loudly, never smoothed over.
  await card.locator("[data-collect]").click();
  await expect(page.locator(".errbar")).toContainText(/not approved|nothing to collect|409/i, { timeout: 20000 });

  // The worker: who is asking and why, BEFORE any disclosure list.
  await workerSignIn(page, request, FIX.workerA, "Grace \u00b7 community health worker");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await expect(page.locator(".appbar .who-label")).toContainText("Grace");
  await page.evaluate(() => { location.hash = "#/shares"; });
  await settle(page);
  const inbox = page.locator(`[data-share="${reqId}"]`);
  await expect(inbox).toContainText("Hiring for a private clinic " + stamp);
  await expect(inbox).toContainText("wants to see more");
  // The arrival names requester and purpose only — no credential ids yet.
  expect(await inbox.innerText()).not.toMatch(/crest:credential:/);

  // The decision: per-line consent against the resolved list, the reference's
  // callouts, and an approval of a strict subset.
  await inbox.locator("[data-open-share]").click();
  await settle(page);
  await expect(page.locator("body")).toContainText("What they already have");
  await expect(page.locator("body")).toContainText("Either way, it is on your record");
  const boxes = page.locator("[data-dis] input[type=checkbox]");
  const total = await boxes.count();
  expect(total, "the story gives Grace more than one credential to decide over").toBeGreaterThan(1);
  await boxes.first().uncheck();
  await expect(page.locator("body")).toContainText(/you have unticked this/i);
  await page.click("#share-approve");
  await settle(page);

  // w1_20 — sent: what they got and what you kept, from the shared record.
  await expect(page.locator("body")).toContainText("Sent");
  await expect(page.locator("body")).toContainText("What they got");
  await expect(page.locator("body")).toContainText("What you kept");
  await expect(page.locator("body")).toContainText("you unticked this — shown to them as refused");
  await expect(page.locator("body")).toContainText("very list the verifier sees");

  // The verifier collects exactly the approved subset — once.
  await verifyOrgSignIn(page, request, FIX.org);
  await page.evaluate(() => { location.hash = "#/requests"; });
  await settle(page);
  const vcard = page.locator(`[data-vshare="${reqId}"]`);
  await expect(vcard).toContainText("APPROVED", { timeout: 20000 });
  await expect(vcard).toContainText(`${total - 1} approved`);
  await vcard.locator("[data-collect]").click();
  await expect(page.locator(`[data-collected="${reqId}"]`)).toContainText(
    `${total - 1} credential`, { timeout: 20000 });
  await expect(page.locator(`[data-collected="${reqId}"]`)).toContainText("exactly the approved list");
  // A second collect is refused: the approval was for one share.
  const again = await asPartyOn(request, SVC.verification, FIX.org, "POST",
    `/v1/presentation-requests/${reqId}/collect`);
  expect(again.status(), "consent is per share — a second collect must be refused").toBe(409);

  // The worker's own read agrees: FULFILLED, and the share is on the trail.
  await page.goto("/worker/");
  await settle(page);
  await page.evaluate(id => { location.hash = `#/shares/${id}/sent`; }, reqId);
  await settle(page);
  await expect(page.locator("body")).toContainText("FULFILLED");
  await page.evaluate(() => { location.hash = "#/profile/checks"; });
  await settle(page);
  await expect(page.locator("body")).toContainText(/shared/i);

  // The decline path: a second ask, refused with a reason on the record.
  const c = await asPartyOn(request, SVC.verification, FIX.org, "POST", "/v1/presentation-requests", {
    subjectPartyId: FIX.workerA, requestedByPartyId: FIX.org,
    purpose: "Background check we never discussed " + stamp,
  });
  expect(c.status()).toBe(201);
  const decId = (await c.json()).request.id;
  await page.evaluate(() => { location.hash = "#/shares"; });
  await settle(page);
  await page.locator(`[data-share="${decId}"] [data-open-share]`).click();
  await settle(page);
  await page.click("#share-refuse");
  await page.fill('[name="sharereason"]', "I do not know this requester " + stamp);
  await page.click("#share-refuse-confirm");
  await settle(page);
  await expect(page.locator("body")).toContainText("Refused");
  await expect(page.locator("body")).toContainText("I do not know this requester " + stamp);
  // Nothing to collect on a declined share.
  const r3 = await asPartyOn(request, SVC.verification, FIX.org, "POST",
    `/v1/presentation-requests/${decId}/collect`);
  expect(r3.status()).toBe(409);
  await assertAlive(page, errors, "presentation loop");
});

test("recovery: nomination routes, a refusal is owned, and two authorities confirm", async ({ page, request }) => {
  test.setTimeout(300000);
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);

  // Reset nominations from an earlier run of this suite (204 or 404, both fine).
  for (const cid of [SPVR, FIX.custodian]) {
    await asParty(request, FIX.workerA, "POST",
      `/v1/parties/${FIX.workerA}/recovery-contacts/${cid}/revoke`);
  }

  // w1_7 — the worker nominates: party-linked picks, revocation kept.
  await workerSignIn(page, request, FIX.workerA, "Grace \u00b7 community health worker");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await page.evaluate(() => { location.hash = "#/profile/recovery"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Who can confirm it is you?");
  await expect(page.locator("body")).toContainText("They can never see your work history or your payments");
  // A nomination is a party, never a phone number: no tel input exists here,
  // and the pick field asks for a registry id.
  await expect(page.locator('.screen input[type="tel"]')).toHaveCount(0);
  await expect(page.locator('[name="contactpartyid"]')).toHaveAttribute("placeholder", /party/i);
  await page.fill('[name="contactpartyid"]', SPVR);
  await page.click("#nominate");
  await expect(page.locator(`[data-contact="${SPVR}"]`)).toBeVisible({ timeout: 20000 });
  await page.fill('[name="contactpartyid"]', FIX.custodian);
  await page.click("#nominate");
  await expect(page.locator(`[data-contact="${FIX.custodian}"]`)).toBeVisible({ timeout: 20000 });
  // Revoke keeps the row — who you trusted, and when you stopped.
  await page.click(`[data-revoke="${FIX.custodian}"]`);
  await expect(page.locator("body")).toContainText("No longer nominated", { timeout: 20000 });
  await page.fill('[name="contactpartyid"]', FIX.custodian);
  await page.click("#nominate");
  await expect(page.locator(`[data-contact="${FIX.custodian}"]`)).toBeVisible({ timeout: 20000 });
  // Self-nomination is refused by the service, loudly.
  await page.fill('[name="contactpartyid"]', FIX.workerA);
  await page.click("#nominate");
  await expect(page.locator(".errbar")).toContainText(/someone else/i, { timeout: 20000 });

  // A recovery opens — the custodian's act; the worker cannot authenticate,
  // which is the premise. A leftover OPEN one from a failed run is reused.
  let recId;
  const opened = await asParty(request, FIX.custodian, "POST", "/v1/recoveries", {
    partyId: FIX.workerA, openedByPartyId: FIX.custodian, reason: "lost phone " + stamp,
  });
  if (opened.status() === 201) {
    recId = (await opened.json()).id;
  } else {
    expect(opened.status()).toBe(409);
    const list = await asParty(request, SPVR, "GET", `/v1/recoveries?confirmerPartyId=${SPVR}`);
    recId = (await list.json()).recoveries.find(x => x.partyId === FIX.workerA).id;
  }

  // w4_1 — the confirmer's inbox, the SMS gap said honestly.
  await page.click("#logout");
  await settle(page);
  await workerSignIn(page, request, FIX.supervisor, "District Supervisor \u00b7 recovery confirmer");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await expect(page.locator(".appbar .who-label")).toContainText("District Supervisor");
  await page.evaluate(() => { location.hash = "#/vouch"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("no SMS channel");
  const rcard = page.locator(`[data-recovery="${recId}"]`);
  await expect(rcard).toContainText("asking to recover");
  await expect(rcard).toContainText("Reply YES");

  // w4_3 — the refusal: reason required, recovery held OPEN, owner named.
  await rcard.locator("#vouch-no").click();
  await rcard.locator("#vouch-no-confirm").click(); // no reason yet
  await expect(page.locator(".errbar")).toContainText(/reason/i, { timeout: 20000 });
  await rcard.locator('[name="refusereason"]').fill("the voice on the phone was not theirs " + stamp);
  await rcard.locator("#vouch-no-confirm").click();
  await settle(page);
  await expect(page.locator("body")).toContainText("What if two never agree?");
  await expect(page.locator("body")).toContainText("the voice on the phone was not theirs " + stamp);
  await expect(page.locator("body")).toContainText("OPEN");
  await expect(page.locator("body")).toContainText("who opened this recovery");

  // One NO never closes it: the same person's YES still counts (the refusal
  // stays on the record beside it).
  await page.click("#vouch-back");
  await settle(page);
  await page.locator(`[data-recovery="${recId}"] #vouch-yes`).click();
  await page.locator(`[data-recovery="${recId}"] #vouch-yes-confirm`).click();
  await settle(page);
  await expect(page.locator("body")).toContainText("your YES is recorded");
  await expect(page.locator("body")).toContainText("1 of 2 needed");

  // The second voice must come from a DIFFERENT authority: a second approved
  // organisation vouches for the custodian, through the real open doors.
  const applicantSub = "apps-recovery-applicant|" + stamp;
  const applicantToken = await mintSubjectToken(request, applicantSub, "recovery applicant");
  const orgPath = "/v1/organisations";
  const orgBody = {
    displayName: "Ward 7 Health Office " + stamp, kind: "organisation",
    contactRoutes: [{ kind: "email", value: `w7+${stamp}@example.org` }],
  };
  const orgR = await request.post(G2.parties + orgPath, {
    headers: {
      Authorization: "Bearer " + applicantToken,
      "Content-Type": "application/json",
      "Idempotency-Key": mutationKey(applicantSub, "POST", orgPath, orgBody),
    },
    data: orgBody,
  });
  expect(orgR.status()).toBe(201);
  const org2 = (await orgR.json()).party.id;
  let r = await asTokenOn(request, G2.parties, applicantToken, org2, "POST", `/v1/organisations/${org2}/terms-acceptance`,
    { termsId: TERM, termsVersion: 1, acceptedBy: org2 });
  expect(r.status()).toBe(200);
  r = await asParty(request, FIX.custodian, "POST", `/v1/organisations/${org2}/decision`,
    { approve: true, decidedBy: FIX.custodian });
  expect(r.status()).toBe(200);
  const ownProjectBody = {
    kind: "project", name: "Ward 7 authority project " + stamp, ownerPartyId: org2,
  };
  r = await asTokenOn(request, G2.parties, applicantToken, org2, "POST", "/v1/projects", ownProjectBody);
  expect(r.status(), "the second authority's own project").toBe(201);
  const ownProjectResponse = await r.json();
  const ownProject = ownProjectResponse.id;
  expect(ownProject, "the second authority project id").toMatch(/^crest:context:/);
  r = await asTokenOn(request, G2.parties, applicantToken, org2, "POST",
    `/v1/projects/${encodeURIComponent(ownProject)}/activation`);
  expect(r.status(), "the second authority activates its own project").toBe(200);
  r = await asTokenOn(request, G2.parties, applicantToken, org2, "POST", "/v1/authorizations", {
    id: "crest:authorization:" + fakeUlid(),
    partyId: FIX.custodian,
    terms: { id: TERM, version: 1 },
    scope: { kind: "context", contextId: ownProject },
    functions: ["submit-work-evidence"],
    period: { start: "2026-01-01T00:00:00Z", end: "2027-12-31T00:00:00Z" },
    authorityPartyId: org2, approvedByPartyId: org2,
    approvedAt: "2026-09-01T00:00:00Z", state: "ACTIVE",
  });
  expect(r.status(), "the second authority's grant").toBe(201);

  // w4_2 — the custodian (also nominated) replies YES under the new authority,
  // and the quorum closes: 2 of 2 distinct authorities, CONFIRMED.
  await page.click("#logout");
  await settle(page);
  await workerSignIn(page, request, FIX.custodian, "Signed in by party id (dev)");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await page.evaluate(() => { location.hash = "#/vouch"; });
  await settle(page);
  const ccard = page.locator(`[data-recovery="${recId}"]`);
  await ccard.locator("#vouch-yes").click();
  await ccard.locator('[name="authority"]').fill(org2);
  await ccard.locator("#vouch-yes-confirm").click();
  await settle(page);
  await expect(page.locator("body")).toContainText("2 of 2 needed");
  await expect(page.locator("body")).toContainText("CONFIRMED");
  await expect(page.locator("body")).toContainText("the record is theirs again");
  await expect(page.locator("body")).toContainText("no longer speaks for the record");

  // Completion appends the new binding — the old ones stay — and frees the
  // one-live-recovery slot for the next run of this suite.
  const done = await asParty(request, FIX.custodian, "POST",
    `/v1/recoveries/${recId}/complete`, { subjectRef: "e2e-recovered-" + stamp });
  expect(done.status()).toBe(200);
  await assertAlive(page, errors, "recovery loop");
});

test("assisted enrolment: the confidence check records a method, never a tier", async ({ page, request }) => {
  test.setTimeout(120000);
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  await page.goto("/enrolment/");
  await settle(page);
  await page.click("[data-login]");
  await settle(page);
  // The door no longer assumes a project: the agent picks one of the
  // contexts their grants name. This walk is about the story's project.
  await pickStoryProject(page);
  await settle(page);
  // The route is reachable from the register screen's own frame.
  await page.evaluate(() => { location.hash = "#/register"; });
  await settle(page);
  await page.click("#to-confidence");
  await settle(page);
  expect(page.url()).toContain("#/confidence");
  await expect(page.locator("body")).toContainText("We can still register you");
  await expect(page.locator("body")).toContainText("without a document");
  // Honest about strength: IA-0 until a route is verified or an anchor binds,
  // and derived — never stored.
  await expect(page.locator("body")).toContainText("IA-0");
  await expect(page.locator("body")).toContainText(/never stored|never as a stored level/i);
  await page.fill('[name="name"]', "Halima Noor " + stamp);
  await page.fill('[name="rosterId"]', "CHW-CONF-" + stamp);
  await page.click('#confidenceform button[type="submit"]');
  await expect(page.locator("[data-confidence-done]")).toBeVisible({ timeout: 20000 });
  const pid = await page.locator("[data-confidence-done]").getAttribute("data-confidence-done");
  expect(pid).toMatch(/^did:crest:party:/);
  // The reference's secondary button works: a recovery contact, nominated now,
  // party-linked (the field prefills the supervisor's party id).
  await page.click("#confidence-nominate");
  await expect(page.locator("[data-nominated]")).toBeVisible({ timeout: 20000 });
  // Continue lands on the consent script — consent is not skipped for the
  // no-document route.
  await page.click("#confidence-continue");
  await settle(page);
  await expect(page.locator("body")).toContainText("Read this to Halima");
  // The record carries the METHOD as provenance — read back from the registry
  // by the enrolling agent, acting for the worker in the programme's context
  // (who enrolled a worker is the worker's own record, #102).
  const supTok = await mintToken(request, SPVR);
  const e = await request.get(
    G2.parties + `/v1/parties/${encodeURIComponent(pid)}/enrolment?contextId=${encodeURIComponent(FIX.project)}`,
    { headers: { Authorization: "Bearer " + supTok, "X-CREST-On-Behalf-Of": pid } },
  );
  expect(e.status()).toBe(200);
  expect((await e.json()).method).toBe("confidence-check");
  await assertAlive(page, errors, "confidence-check enrolment");
});

test("qualification arrival: the anchor lands and earned strength re-derives", async ({ page, request }) => {
  test.setTimeout(120000);
  const errors = watch(page);
  await ensureRuntime(request);

  // Before: Chandra was enrolled with no binding (IA-0). On a clean stack the
  // verifier says so — the tier is computed at the weakest assurance. On a
  // re-run she is already anchored (this very test anchored her), so the
  // before-state is asserted only when it is genuinely there to see.
  const credsR = await asPartyOn(
    request,
    SVC.verification,
    CHANDRA,
    "GET",
    `/v1/parties/${encodeURIComponent(CHANDRA)}/credentials`,
  );
  expect(credsR.status(), "Chandra can read her own private credential chain").toBe(200);
  const creds = (await credsR.json()).credentials || [];
  expect(creds.length, "the story gives Chandra a credential").toBeGreaterThan(0);
  const v1 = await request.post(SVC.verification + "/v1/verify", { data: { credential: creds[0] } });
  const before = JSON.stringify(await v1.json());
  const wasUnanchored = /weakest assurance/.test(before);

  // The anchor lands: Chandra's first sign-in APPENDS an identity binding —
  // the same first-login path an eSignet anchor takes. Nothing is rewritten.
  await page.goto("/worker/");
  await settle(page);
  await workerSignIn(page, request, CHANDRA, "Signed in by party id (dev)");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await page.evaluate(() => { location.hash = "#/added"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Added");
  await expect(page.locator("body")).toContainText("Your eight months just became portable");
  await expect(page.locator("body")).toContainText("derived right now");
  await expect(page.locator("body")).toContainText("IA-3");
  await expect(page.locator("body")).toContainText(/never stored/i);
  // Everything already earned is still there — same count, no re-issue.
  await expect(page.locator(".stat .n").first()).toContainText(String(creds.length));

  // After: the live re-check derives the strength fresh, and the
  // weakest-assurance caveat is gone — with the credential untouched.
  await page.click("#recheck");
  await expect(page.locator("#recheck-out")).toBeVisible({ timeout: 20000 });
  const out = await page.locator("#recheck-out").innerText();
  expect(out).toMatch(/derived at this check, stored nowhere/);
  expect(out).not.toMatch(/weakest assurance/i);
  if (wasUnanchored) {
    // The flip itself was observed end to end on this run: weakest-assurance
    // before the anchor, gone after, and nothing about the credential changed.
    expect(before).toContain("weakest assurance");
  }
  await assertAlive(page, errors, "qualification arrival");
});

// ── The funders wave: a rate is terms, and the gate sits in front of ────────
// disbursement (reference f1_2–f1_5, f2_4–f2_10; PR #191's backend, driven
// end to end through the console).
//
// The payment invariants this walk proves, named:
//  - Every confirmation-window exit releases payment (W4): the worker's
//    confirmation below happens while the mechanism is CONFIGURED and not
//    live, and the exit still creates its instruction — HELD with
//    mechanism_not_live, never missing. The gate chose the instruction's
//    state; it could not stop the release.
//  - Every held payment has a reason with an owner (W10): the held row is
//    asserted to carry both, on the invariant screen (f2_9).
//  - A rate is terms, not a setting: publication is a new version, no edit
//    affordance exists, and only the assigned owner may author (refusal
//    asserted).
//
// The walk runs in its own fresh project so a second run on the same stack
// finds the same starting state: a mechanism is per context, and activation
// is one-way.
const PAYSVC = (() => {
  const base = process.env.BASE_URL || "http://localhost:59110";
  const local = new URL(base).port === "59110";
  const host = new URL(base).hostname;
  return {
    payments: local ? `http://${host}:59006` : base.replace(/\/$/, "") + "/api/crest-payments",
    evidence: local ? `http://${host}:59000` : base.replace(/\/$/, "") + "/api/crest-evidence",
  };
})();
const DEFN = "crest:definition:01JCREST00000000000000DEFN";

test("console: the funders walk — rate as terms, held with an owner, released by the last gate", async ({ page, request }) => {
  test.setTimeout(300000);
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  const me = FIX.org;

  // The definition registry authorizes a payment-setup linked record with the
  // rate-owner function in the definition's project context. The older
  // fixture grant used the retired set-rates name, so the real publication
  // would be refused by core before the rate-standing screen could render.
  // Establish the current, scoped authority through the public authorization
  // API; the publication below still proves the assigned owner is the caller.
  const rateGrant = await asParty(request, me, "POST", "/v1/authorizations", {
    id: "crest:authorization:" + fakeUlid(),
    partyId: me,
    terms: { id: TERM, version: 1 },
    scope: { kind: "context", contextId: FIX.project },
    functions: ["rate-owner"],
    period: { start: "2026-01-01T00:00:00Z", end: "2027-12-31T23:59:59Z" },
    authorityPartyId: me,
    approvedByPartyId: me,
    approvedAt: "2026-09-01T00:00:00Z",
    state: "ACTIVE",
  });
  expect(rateGrant.status(), "the scoped rate-owner authority is recorded").toBe(201);

  // ── F-1 · f1_2: only one person can assign — the Org Admin records it. ──
  await consoleSignIn(page, request, "orgadmin");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await signedInAs(page, "Peter Otieno");
  await page.evaluate(() => { location.hash = "#/rateowner"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("A request to put someone on payment");
  await expect(page.locator("body")).toContainText("Anyone can ask. Only one person can assign.");
  await page.fill('[name="rateownerparty"]', me);
  await page.click("#assign-owner");
  await expect(page.locator("body")).toContainText("owner assigned", { timeout: 20000 });
  // The assignment is a record with the assigner's name and date, kept.
  await expect(page.locator("[data-assignment]").first()).toContainText("assigned by");

  // The funder role is now derivable from the assignment itself, not a seeded
  // label: the assigned party's own /rate-ownerships/mine names the definition,
  // and a party that owns no rate gets an empty list. This is what lets a real
  // eSignet login land on the rate-owner surface (console state.tsx derivation).
  const mineRates = await asPartyOn(request, PAYSVC.payments, me, "GET", "/v1/rate-ownerships/mine");
  expect(mineRates.status()).toBe(200);
  expect((await mineRates.json()).definitionIds, "the assigned owner's own read names the definition").toContain(DEFN);
  const notOwnerRates = await asPartyOn(request, PAYSVC.payments, FIX.custodian, "GET", "/v1/rate-ownerships/mine");
  expect((await notOwnerRates.json()).definitionIds || [], "a non-owner owns no rate").not.toContain(DEFN);

  // A party who is NOT the assigned owner cannot author a rate — the
  // service's refusal, not a hidden button.
  const notOwner = await asPartyOn(request, PAYSVC.payments, FIX.custodian, "POST",
    `/v1/definitions/${DEFN}/rates`,
    { authorPartyId: FIX.custodian, amountMinor: 100, currency: "KES", payerPartyId: FIX.custodian });
  expect(notOwner.status(), "only the assigned rate owner authors").toBe(403);
  expect((await notOwner.json()).code).toBe("not_rate_owner");

  // ── f1_3 / f1_4: the owner prices a unit somebody else defined. ──
  await consoleSignIn(page, request, "rateowner");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await signedInAs(page, "Nadia Okoth");
  await page.evaluate(() => { location.hash = "#/rate"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("What one unit pays");
  // The unit is read-only by construction: shown, and no control can name it.
  await expect(page.locator("body")).toContainText("Unit of work");
  await expect(page.locator("body")).toContainText("bednets");
  await expect(page.locator('input[name="unitofwork"]')).toHaveCount(0);
  await page.fill('[name="rateamount"]', "175.00");
  // Work is priced by the version in force when its period started, never
  // the latest (f1_4). The walk's work below is dated 1 Sep, so this version
  // is effective from then; the seeded v1 keeps pricing anything earlier.
  await page.fill('[name="rateeffective"]', "2026-09-01");
  await page.click("#rate-continue");
  await settle(page);
  expect(page.url()).toContain("#/ratepublish");
  await expect(page.locator("body")).toContainText("What the worker will see");
  // A rate is terms, not a setting: the seeded v1 is listed, and NO edit
  // affordance of any kind exists on the published versions.
  await expect(page.locator('[data-rateversion="1"]')).toBeVisible();
  await expect(page.locator("button", { hasText: /edit|change|update/i })).toHaveCount(0);
  await expect(page.locator("body")).toContainText("There is no edit here and never will be");
  const versionsBefore = await page.locator("[data-rateversion]").count();
  await page.click("#publish-rate");
  await page.waitForURL(/#\/ratestanding/, { timeout: 20000 });
  await settle(page);

  // ── f1_5: half done is a real state, derived — and this project has no
  // mechanism, so the standing is not-configured, said plainly. ──
  await expect(page.locator("body")).toContainText("The rate is live. The money still cannot move.");
  await expect(page.locator('[data-standing="not-configured"]')).toBeVisible();
  await expect(page.locator("body")).toContainText("Hand this to someone else");
  // f1_5's outstanding items, each owed by exactly one party.
  await expect(page.locator("body")).toContainText("What is now owed, and by whom");
  await expect(page.locator("body")).toContainText("None of these escalates on its own.");
  // Supersession is a new version: the publish added one, naming its parent.
  await page.evaluate(() => { location.hash = "#/ratepublish"; });
  await settle(page);
  const versionsAfter = await page.locator("[data-rateversion]").count();
  expect(versionsAfter, "publication is a new version, never a rewrite").toBe(versionsBefore + 1);
  await expect(page.locator("[data-rateversion]").last()).toContainText("supersedes v" + versionsBefore);
  // f1_4 — terms are what make a dispute possible, on the frame.
  await expect(page.locator("body")).toContainText("Published terms are what make a dispute possible.");
  await assertAlive(page, errors, "F-1 rate walk");

  // ── The fresh project this run's mechanism will govern. ──
  let r = await asParty(request, me, "POST", "/v1/projects", {
    name: "Funders walk " + stamp, ownerPartyId: me,
    configuration: { coverage: "Funders wave walk" },
  });
  expect(r.status(), "the walk's own project").toBe(201);
  const projBody = await r.json();
  const projId = (projBody.project || projBody).id;
  expect(projId).toMatch(/^crest:context:/);
  // The supervisor may submit evidence on it — the same grant shape the
  // fixture world holds for the seeded project.
  r = await asParty(request, me, "POST", "/v1/authorizations", {
    id: "crest:authorization:" + fakeUlid(),
    partyId: SPVR,
    terms: { id: TERM, version: 1 },
    scope: { kind: "context", contextId: projId },
    // Evidence arrives only from a registered source (§9), and only about a
    // worker who consented to this programme — so the supervisor also owns
    // the walk's source and acts for the worker at the consent moment.
    functions: ["submit-work-evidence", "work-definition-source-owner", "act-for-party"],
    period: { start: "2026-01-01T00:00:00Z", end: "2027-12-31T00:00:00Z" },
    authorityPartyId: me, approvedByPartyId: me,
    approvedAt: "2026-09-01T00:00:00Z", state: "ACTIVE",
  });
  expect(r.status(), "the supervisor's grant on the walk's project").toBe(201);
  // The mechanism owner reads the project's instructions as the finance
  // surface does — payment-owner on the walk's project, the grant the fixture
  // world holds on the seeded one.
  r = await asParty(request, me, "POST", "/v1/authorizations", {
    id: "crest:authorization:" + fakeUlid(),
    partyId: me,
    terms: { id: TERM, version: 1 },
    scope: { kind: "context", contextId: projId },
    functions: ["payment-owner", "claim-review", "read-work-evidence"],
    period: { start: "2026-01-01T00:00:00Z", end: "2027-12-31T00:00:00Z" },
    authorityPartyId: me, approvedByPartyId: me,
    approvedAt: "2026-09-01T00:00:00Z", state: "ACTIVE",
  });
  expect(r.status(), "the owner's finance grant on the walk's project").toBe(201);
  const walkSource = await asPartyOn(request, PAYSVC.evidence, SPVR, "POST", "/v1/sources", {
    adapterRef: "csv-batch@1",
    contextId: projId,
    systemRef: "csv-batch",
    sourceClass: "programme-system",
    captureMethod: "digital-capture",
    sourceExposure: "signed-batch",
    expectedEvery: "24h",
    ownerPartyId: SPVR,
  });
  expect(walkSource.status(), "the walk's source is registered by its owner").toBe(201);
  const consentPath = `/v1/parties/${encodeURIComponent(FIX.workerA)}/consents?moment=enrolment&captureMethod=assisted` +
    `&purpose=work-history-and-payment&capturedBy=${encodeURIComponent(SPVR)}&contextId=${encodeURIComponent(projId)}`;
  const walkConsent = await request.post(G2.parties + consentPath, {
    headers: {
      Authorization: "Bearer " + (await mintToken(request, SPVR)),
      "X-CREST-On-Behalf-Of": FIX.workerA,
      "Idempotency-Key": mutationKey(SPVR, "POST", consentPath, null),
    },
  });
  expect(walkConsent.status(), "the worker's consent to the walk's programme").toBe(201);

  // ── F-2: the mechanism owner configures — and no further. ──
  await consoleSignIn(page, request, "payowner");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await signedInAs(page, "Daniel Mwangi");
  // Work on the walk's project: chosen from the list the registry serves,
  // never typed into the browser.
  await page.evaluate(() => { location.hash = "#/where"; });
  await settle(page);
  await page.locator(`[data-context="${projId}"]`).click();
  await settle(page);

  // ── f2_1: one question, and it is the composition record — where CREST
  // stops, answered by the mechanism owner through the same payment-posture
  // choice the Configurator's screen writes. ──
  await page.evaluate(() => { location.hash = "#/mech/where"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("One question. Everything after it follows from the answer.");
  await expect(page.locator("body")).toContainText("It is a choice about where CREST stops.");
  await page.locator('.optcard:has-text("At a payment instruction")').click();
  await page.waitForURL(/#\/mech\/rails/, { timeout: 20000 });
  await settle(page);

  // ── f2_2: rails, chosen as a set and recorded; choosing creates the
  // mechanism this walk's records hang off. ──
  await expect(page.locator("body")).toContainText("One project is rarely one rail.");
  await page.locator('.optcard:has-text("Bank or FSP")').click();
  await page.locator('.optcard:has-text("An implementing partner")').click();
  await page.click("#save-rails");
  await expect(page.locator("body")).toContainText("rails-chosen — by", { timeout: 20000 });
  await expect(page.locator("body")).toContainText(/configured, not live/i);

  // The mechanism owner is now derivable from owning the mechanism: their own
  // /mechanisms/mine names it. This is the fact a real eSignet login reads to
  // land on the /mech surface as the payment mechanism owner (state.tsx).
  const mineMechs = await asPartyOn(request, PAYSVC.payments, me, "GET", "/v1/mechanisms/mine");
  expect(mineMechs.status()).toBe(200);
  expect((await mineMechs.json()).mechanisms.length, "the owner's own read names their mechanism").toBeGreaterThan(0);

  // ── f2_3: the connection names the provider; the secret never passes
  // through the console. ──
  await page.evaluate(() => { location.hash = "#/mech/connect"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("this only establishes that CREST can talk to your provider");
  await expect(page.locator("body")).toContainText("held by the platform operator — never entered here");
  await page.click("#save-connect");
  await expect(page.locator("body")).toContainText("provider-connected — by", { timeout: 20000 });

  await page.evaluate(() => { location.hash = "#/mech/test"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Send one real payment");
  await expect(page.locator("body")).toContainText(/configured, not live/i, { timeout: 20000 });

  // f2_8, refused readably: activation before the acts is a readable list of
  // unmet conditions, each naming the act that satisfies it — never a bare no.
  await page.evaluate(() => { location.hash = "#/mech/activate"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Before payment goes live");
  await page.click("#activate-mech");
  await expect(page.locator("body")).toContainText("Refused, readably", { timeout: 20000 });
  for (const cond of ["test-disbursement-succeeded", "reconciliation-file-agreed",
    "batching-choice-recorded", "qualification-verified"]) {
    await expect(page.locator(`[data-cond="${cond}"]`)).toBeVisible();
  }

  // ── W4, driven for real: work is confirmed while the mechanism is NOT
  // live, and the exit still releases its payment obligation. ──
  await bindParty(request, FIX.workerA);
  const supTok = await mintToken(request, SPVR);
  const csv = "activity,outcome_value,outcome_unit,worker_id_kind,worker_id," +
    "period_start,period_end,geography,household_id,beneficiary_count,source_record_ref\n" +
    `bednet-distribution,3,bednets-distributed,phone,+15550100011,2026-09-01,2026-09-01,Riverside,funders-HH-${stamp},3,funders-${stamp}\n`;
  const batchPath = `/v1/batches?contextId=${encodeURIComponent(projId)}&definitionId=${encodeURIComponent(DEFN)}&definitionVersion=1` +
    `&submittedBy=${encodeURIComponent(SPVR)}&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=signed-batch&systemRef=csv-batch`;
  const batch = await request.fetch(PAYSVC.evidence + batchPath, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + supTok, "Content-Type": "text/csv",
    },
    data: csv,
  });
  expect([200, 201], "the batch lands").toContain(batch.status());
  const claimId = (await batch.json()).claimIds[0];
  expect(claimId).toBeTruthy();

  // The window opens through the outbox — poll, then the worker confirms.
  await expect.poll(async () =>
    (await asPartyOn(request, PAYSVC.evidence, FIX.workerA, "GET", `/v1/windows/${claimId}`)).status(),
  { timeout: 60000 }).toBe(200);
  r = await asPartyOn(request, PAYSVC.evidence, FIX.workerA, "POST", `/v1/claims/${claimId}/confirm`, {});
  expect(r.status(), "the worker's confirmation exit").toBe(200);

  // The exit released the obligation; the not-live mechanism turned it into a
  // HELD instruction with a reason and a named owner — never a missing one.
  let instruction;
  await expect.poll(async () => {
    // The instruction is the worker's own record: read it as them.
    const res = await asPartyOn(request, PAYSVC.payments, FIX.workerA, "GET", `/v1/instructions/by-claim/${claimId}`);
    if (res.status() !== 200) return "no instruction yet";
    instruction = await res.json();
    return instruction.state;
  }, { timeout: 60000 }).toBe("HELD");
  expect(instruction.held.code).toBe("mechanism_not_live");
  expect(instruction.held.ownerPartyId).toBe(me);

  // ── f2_9, THE INVARIANT SCREEN: the boundary, on its face. ──
  await page.evaluate(() => { location.hash = "#/mech/qualify"; });
  await settle(page);
  await expect(page.locator("#stepcounter")).toContainText("Payment set up · 3 of 4");
  await expect(page.locator("body")).toContainText("Before any real money moves");
  // The reference's callout, verbatim.
  await expect(page.locator("body")).toContainText("Why this is the third step and not the first");
  await expect(page.locator("body")).toContainText(
    "A partner who turns out not to fit can discover it in an afternoon instead of after a document round.");
  // The boundary: confirmation exits released; only disbursement waits.
  await expect(page.locator("body")).toContainText(
    "confirm, dispute, auto-confirm, supervisor-assisted, all four");
  await expect(page.locator("body")).toContainText("Only disbursement waits on this gate");
  const heldRow = page.locator(`[data-heldinstruction="${instruction.id}"]`);
  await expect(heldRow).toBeVisible();
  await expect(heldRow).toContainText("HELD");
  await expect(heldRow).toContainText("owner");
  await expect(heldRow).toContainText("…" + me.slice(-6));

  // Verifying nothing is refused before anything is submitted (f2_9).
  const mechR = await asPartyOn(request, PAYSVC.payments, me, "GET",
    `/v1/mechanisms/by-context/${encodeURIComponent(projId)}`);
  const mechId = (await mechR.json()).mechanism.id;
  r = await asPartyOn(request, PAYSVC.payments, FIX.custodian, "POST",
    `/v1/mechanisms/${mechId}/records`,
    { kind: "qualification-verified", actorPartyId: FIX.custodian });
  expect(r.status(), "a verification of nothing verifies nothing").toBe(409);

  // Submit for verification — the screen's own act.
  await page.click("#submit-qual");
  await page.waitForURL(/#\/mech\/live/, { timeout: 20000 });
  await settle(page);
  await expect(page.locator("#stepcounter")).toContainText("Payment set up · 4 of 4");
  // The reference's honest-gap callout, verbatim (f2_10).
  await expect(page.locator("body")).toContainText("What is not watched");
  await expect(page.locator("body")).toContainText("That is a gap, not a design.");

  // Verification is another party's recorded act — the custodian's, via the
  // same door a real reviewer would use.
  r = await asPartyOn(request, PAYSVC.payments, FIX.custodian, "POST",
    `/v1/mechanisms/${mechId}/records`,
    { kind: "qualification-verified", actorPartyId: FIX.custodian });
  expect(r.status(), "the verification, recorded by a different party").toBe(201);

  // ── f2_4: one real payment through the rail, recorded either way. ──
  await page.evaluate(() => { location.hash = "#/mech/test"; });
  await settle(page);
  // A test that names no destination is refused loudly, not smoothed over.
  await page.fill('[name="testdest"]', "");
  await page.click("#send-test");
  await expect(page.locator(".errbar")).toContainText(/destination|required/i, { timeout: 20000 });
  await page.fill('[name="testdest"]', "test-account-4412");
  await page.fill('[name="testamount"]', "10.00");
  await page.click("#send-test");
  await expect(page.locator('[data-test-result="SUCCEEDED"]')).toBeVisible({ timeout: 20000 });
  await expect(page.locator('[data-test-result="SUCCEEDED"]')).toContainText("rail ref");

  // ── f2_5: the file where every line ties to an instruction. ──
  await page.evaluate(() => { location.hash = "#/mech/recon"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("crest-recon-csv-v1");
  await expect(page.locator("#recon-preview")).toContainText("instruction_id");
  await expect(page.locator("#recon-preview")).toContainText("claim_id");
  await page.click("#agree-recon");
  await expect(page.locator("body")).toContainText("reconciliation-agreement — by", { timeout: 20000 });

  // ── f2_6: the advisory statement carries its limits on itself. ──
  await page.evaluate(() => { location.hash = "#/mech/statement"; });
  await settle(page);
  await expect(page.locator("[data-limit]").first()).toContainText(/advisory only/i);
  await expect(page.locator("body")).toContainText(
    "a held payment appears here with its reason and owner; it is not missing, it is explained");
  await page.click("#agree-statement");
  await expect(page.locator("body")).toContainText("statement-agreement — by", { timeout: 20000 });

  // ── f2_7: the batching choice pays with the worker's waiting time, so a
  // choice with the trade-off unstated is refused. ──
  await page.evaluate(() => { location.hash = "#/mech/batching"; });
  await settle(page);
  // The reference's sentence is "Batching is paid for by the worker"; the
  // screen carries the same rule in its own words.
  await expect(page.locator("body")).toContainText("paid for by the worker");
  // The reference's dispute-hold field exists — and this deployment answers
  // it honestly instead of pretending: a dispute never withholds the money.
  await expect(page.locator("body")).toContainText("Hold payment if a dispute is open");
  await expect(page.locator("body")).toContainText("a dispute contests the record, not the money");
  await page.fill('[name="batchwindow"]', "daily-17:00");
  await page.click("#record-batching");
  await expect(page.locator(".errbar")).toContainText(/trade-off/i, { timeout: 20000 });
  await page.fill('[name="batchtradeoff"]',
    "workers paid once daily at 17:00 wait up to a day for confirmed work " + stamp);
  await page.click("#record-batching");
  await expect(page.locator("[data-batching]")).toContainText("chosen by", { timeout: 20000 });
  await expect(page.locator("[data-batching]")).toContainText("wait up to a day");

  // ── f2_8 → f2_10: every condition now reads a recorded act; activation
  // flips ACTIVE and releases what the gate was holding. ──
  await page.evaluate(() => { location.hash = "#/mech/activate"; });
  await settle(page);
  for (const cond of ["test-disbursement-succeeded", "reconciliation-file-agreed",
    "batching-choice-recorded", "qualification-verified"]) {
    await expect(page.locator(`[data-cond="${cond}"]`)).toHaveAttribute("data-satisfied", "true");
  }
  await page.click("#activate-mech");
  await page.waitForURL(/#\/mech\/live/, { timeout: 20000 });
  await settle(page);
  await expect(page.locator("body")).toContainText("Verified — real payments can now run");
  await expect(page.locator("body")).toContainText(/live/);
  await expect(page.locator("body")).toContainText("went live");
  // What the last gate opened: the very instruction the walk watched being
  // held is now released, priced by the version in force when the work
  // happened, with money on it.
  const released = page.locator(`[data-released="${instruction.id}"]`);
  await expect(released).toBeVisible({ timeout: 20000 });
  await expect(released).toContainText(/RELEASED|SETTLED/);
  await expect(released).toContainText("KES");
  await expect(released).toContainText("opened by this activation");
  // …and the service agrees: the held state is gone, the amount is real
  // (3 units at the version in force on 1 Sep: 175.00 KES → 525.00).
  r = await asPartyOn(request, PAYSVC.payments, FIX.workerA, "GET", `/v1/instructions/by-claim/${claimId}`);
  expect(r.status(), "the worker reads their released instruction").toBe(200);
  const after = await r.json();
  expect(after.state === "RELEASED" || after.state === "SETTLED").toBeTruthy();
  expect(after.held).toBeFalsy();
  expect(after.amountMinor).toBe(52500);
  await assertAlive(page, errors, "the funders walk");
});

// ═══════════════════════════════════════════════════════════════════════════
// P-3 — the definition-authoring wizard (reference p3_1–p3_28, p3_pay).
//
// One walk, because the thing being proven is a sequence: a definition is
// written section by section against a real draft, its open questions are the
// compiler's own list, a dry run derives tiers from real rows and commits
// nothing, submit appends an immutable version, an approver who is a different
// party signs it naming what stays pending, pricing is handed to a rate owner,
// and the event log accounts for every act with its actor.
//
// The invariant under all of it: definitions sit beneath evidence and
// payments, and **trust strength is derived, never stored**. The dry-run step
// proves that directly — the same rows, judged twice with different
// provenance, come back with different tiers, which is only possible if no
// tier was ever written down.
// ═══════════════════════════════════════════════════════════════════════════

const P3 = {
  // The fixture world records the SPEC party as the seeded definition's
  // author and the organisation as its ratifier. The console's two P-3
  // personas sign in as those two parties, so separation of duties is a fact
  // about the data rather than a claim about the navigation.
  author: "did:crest:party:01JCREST00000000000000SPEC",
  approver: "did:crest:party:01JCREST000000000000000RGN",
};
const FIX_DEFINITION = "crest:definition:01JCREST00000000000000DEFN";

// The definitions service, reached the way the doors reach it.
const DEFSVC = (() => {
  const base = process.env.BASE_URL || "http://localhost:59110";
  const url = new URL(base);
  return url.port === "59110"
    ? `http://${url.hostname}:59000`
    : base.replace(/\/$/, "") + "/api/crest-definitions";
})();

const fill = (page, name, value) => page.locator(`[name="${name}"]`).fill(value);
const choose = (page, name, value) => page.locator(`[name="${name}"]`).selectOption(value);
const press = (page, label) => page.click(`[data-btn="${label}"]`);

async function grantP3Approver(request) {
  const r = await asParty(request, FIX.org, "POST", "/v1/authorizations", {
    id: "crest:authorization:" + fakeUlid(),
    partyId: FIX.org,
    terms: { id: TERM, version: 1 },
    scope: { kind: "context", contextId: FIX.project },
    functions: ["work-definition-approver"],
    period: { start: "2026-01-01T00:00:00Z", end: "2027-12-31T23:59:59Z" },
    authorityPartyId: FIX.org,
    approvedByPartyId: FIX.org,
    approvedAt: "2026-09-01T00:00:00Z",
    state: "ACTIVE",
  });
  expect(r.status(), "the approver's scoped authority is recorded").toBe(201);
}

// A wizard step: press the frame's own button, wait for the route it names.
async function step(page, label, route) {
  await press(page, label);
  await page.waitForURL(new RegExp("#/" + route.replace(/\//g, "\\/")), { timeout: 25000 });
  await settle(page);
}

async function signInAs(page, request, persona, who) {
  await consoleSignIn(page, request, persona);
  await page.locator("#logout").waitFor({ state: "visible", timeout: 25000 });
  await settle(page);
  await signedInAs(page, who);
}

// Switch persona inside the same tab, so sessionStorage — which is where the
// draft id lives — survives. That is deliberate: the approver reads the very
// draft the author just submitted, and the console holds nothing else.
// consoleSignIn only rewrites the session key, so the rest of sessionStorage
// (the draft id) survives the switch.
async function switchTo(page, request, persona, who) {
  await consoleSignIn(page, request, persona);
  await page.locator("#logout").waitFor({ state: "visible", timeout: 25000 });
  await settle(page);
  await signedInAs(page, who);
}

// g4_4/g4_5/g4_7/w6_3, the tail-wave's four registry/receipt reads (#197):
// coverage-by-place, the quality worklist, registry reuse, and — after a
// real batch submission — the project-side receipt for what arrived.
test("console: the registry metrics read real counts, and the receipt shows a real ingestion", async ({ page, request }) => {
  test.setTimeout(120000);
  const errors = watch(page);

  // ── g4_4/g4_5/g4_7, as the registry custodian. ──
  await consoleSignIn(page, request, "custodian");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await signedInAs(page, "Otieno");

  // g4_4: the fixture world never sets a "county" attribute, so every party
  // falls into the honest unspecified bucket — never a fabricated percentage.
  await page.evaluate(() => { location.hash = "#/coverage"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Worst gaps first — this is the work list");
  await expect(page.locator("body")).toContainText(/unspecified/i);
  await expect(page.locator("body")).not.toContainText("0.0%");
  await signedInAs(page, "Otieno");

  // g4_5: a row per party with a named gap, or the honest empty state.
  await page.evaluate(() => { location.hash = "#/registry-quality"; });
  await settle(page);
  await expect(page.locator("body")).toContainText(/Registered but with a gap/i);
  await expect(page.locator("body")).toContainText("What is missing, counted");
  await signedInAs(page, "Otieno");

  // g4_7: the reuse metric, or its null state — never a fabricated 0.
  await page.evaluate(() => { location.hash = "#/reuse"; });
  await settle(page);
  await expect(page.locator("body")).toContainText(/reuse rate/i);
  await expect(page.locator("body")).toContainText(/derivation/i);
  await signedInAs(page, "Otieno");
  await assertAlive(page, errors, "custodian registry metrics");

  // ── w6_3: submit a batch for real (the same CSV-batch door the story's own
  // seeding and the funders walk use), then read its receipt back as the
  // project side. ──
  const stamp = Date.now().toString().slice(-6);
  const supTok = await mintToken(request, SPVR);
  // The pinned definition admits only its declared source systems; the walk
  // registers one of them on the project rather than inventing a name.
  const sourceRef = "csv-batch";
  const source = await asPartyOn(request, PAYSVC.evidence, SPVR, "POST", "/v1/sources", {
    adapterRef: "csv-batch@1",
    contextId: FIX.project,
    systemRef: sourceRef,
    sourceClass: "programme-system",
    captureMethod: "digital-capture",
    sourceExposure: "signed-batch",
    expectedEvery: "24h",
    ownerPartyId: SPVR,
  });
  expect(source.status(), "the receipt source is registered by its scoped owner").toBe(201);
  const csv = "activity,outcome_value,outcome_unit,worker_id_kind,worker_id," +
    "period_start,period_end,geography,household_id,beneficiary_count,source_record_ref\n" +
    `bednet-distribution,4,bednets-distributed,phone,+15550100011,2026-09-01,2026-09-01,Riverside,receipt-HH-${stamp},4,receipt-${stamp}\n`;
  const batchPath = `/v1/batches?contextId=${encodeURIComponent(FIX.project)}&definitionId=${encodeURIComponent(DEFN)}&definitionVersion=1` +
    `&submittedBy=${encodeURIComponent(SPVR)}&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=signed-batch&systemRef=${encodeURIComponent(sourceRef)}`;
  const batchRes = await request.fetch(PAYSVC.evidence + batchPath, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + supTok, "Content-Type": "text/csv",
    },
    data: csv,
  });
  expect([200, 201], "the batch lands").toContain(batchRes.status());
  const batchId = (await batchRes.json()).batch.id;
  expect(batchId).toBeTruthy();

  await switchTo(page, request, "instance", "Instance administrator");
  await page.evaluate(() => { location.hash = "#/receipt"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("What the project received, and where it sits");
  await page.fill('input[placeholder="batch id"]', batchId);
  await page.click("button:has-text('Look up')");
  await expect(page.locator("body")).toContainText(batchId, { timeout: 20000 });
  await expect(page.locator("body")).toContainText("bednets-distributed");
  await expect(page.locator("body")).toContainText(/DRAFT|NOTIFIED|ACCEPTED|DISPUTED/);
  await signedInAs(page, "Instance administrator");
  await assertAlive(page, errors, "project receipt");
});

test("console: the authoring wizard writes a definition, proves it dry, and has it ratified with its gaps named", async ({ page, request }) => {
  const errors = watch(page);
  test.setTimeout(240000);

  // The approver is a real project-scoped caller. The seed's canonical
  // definition is ratified by the custodian, while this independent authoring
  // journey uses the organisation as its approver and therefore records that
  // authority through the public grant API first.
  await grantP3Approver(request);

  // ── p3_1 · the registry. "Define new work" is a real POST; a draft exists
  //    on the server before the first screen renders. ──
  await signInAs(page, request, "author", "Amina Yusuf");
  await page.evaluate(() => { location.hash = "#/definework"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Work definitions");
  // The reference frame: one list, three filter chips over it.
  await expect(page.locator(".fchip")).toHaveCount(3);
  await step(page, "Define new work", "define/sector");
  // The draft is real and this session is authoring it.
  await page.evaluate(() => { location.hash = "#/definework"; });
  await settle(page);
  await expect(page.locator("[data-authoring]")).toContainText("definition-draft");
  // The current draft's own row does not hide its button: it says it is open
  // in this session and carries Continue back into the wizard.
  await expect(page.locator("[data-continue]")).toHaveCount(1);
  await expect(page.locator("body")).toContainText("open · this session");
  await page.evaluate(() => { location.hash = "#/define/sector"; });
  await settle(page);

  // ── p3_2 · sector. The deployment declares no vocabulary, and the screen
  //    says so rather than offering a list CREST invented. ──
  await expect(page.locator("#stepcounter")).toContainText("Sector · 1 of 9");
  await expect(page.locator("body")).toContainText("This deployment declares none");
  await fill(page, "sector", "health");
  await step(page, "Continue", "define/counting");

  // ── p3_18, early · the open questions are the compiler's own list, and a
  //    barely-started draft has many. ──
  await page.evaluate(() => { location.hash = "#/define/open"; });
  await settle(page);
  const openEarly = await page.locator("[data-problem]").count();
  expect(openEarly, "a one-section draft has real open questions").toBeGreaterThan(4);
  await expect(page.locator("body")).toContainText(/produced by the same function the submit runs/i);
  // Submitting now is refused by name, and the screen says so before you try.
  await expect(page.locator("body")).toContainText(/Submitting now will be refused, by name/i);
  await page.evaluate(() => { location.hash = "#/define/counting"; });
  await settle(page);

  // ── p3_3 · the fork. Three branches, all reachable, and the button writes
  //    the basis before it navigates. ──
  await expect(page.locator("#stepcounter")).toContainText("Counting basis · 2 of 9");
  await expect(page.locator(".optcard")).toHaveCount(3);
  // The reference frame: three plain cards; the outcome branch offered by name.
  await expect(page.locator("body")).toContainText(/Paid on a result, often measured across a population/i);
  // The two branch screens the fork offers, visited and then left — the draft
  // records whichever branch the walk continues on.
  await step(page, "Time-based instead", "define/period");
  await expect(page.locator("#stepcounter")).toContainText("Period · 3 of 7");
  await step(page, "Outcome-based", "define/outcome");
  await expect(page.locator("#stepcounter")).toContainText("Outcome · 3 of 7");
  await expect(page.locator("body")).toContainText(/evidence about a district, not about a person/i);
  await step(page, "Back", "define/counting");
  await step(page, "Continue as event", "define/category");

  // ── p3_4 · the category, scoped to the sector. ──
  await expect(page.locator("#stepcounter")).toContainText("Category · 3 of 9");
  await expect(page.locator("body")).toContainText("health");
  await fill(page, "category", "community-outreach");
  await step(page, "Continue", "define/unit");

  // ── p3_5 · the unit, and the one field the rate will price. ──
  await expect(page.locator("#stepcounter")).toContainText("Unit · 4 of 9");
  await fill(page, "outcomeUnit", "bednets-distributed");
  await fill(page, "activityCode", "bednet-distribution");
  await fill(page, "activityLabel", "Bednet distribution");
  await fill(page, "frequency", "Per campaign day");
  await fill(page, "countingModel", "Individually countable — one record per unit");
  await page.click('.optcard:has-text("Per individual event")');
  // The reference's blue note: the rate references the unit by ID, and the
  // screen echoes the unit it will reference.
  await expect(page.locator('.sidecar:has-text("referenced by ID by any rate")'))
    .toContainText("bednets-distributed");
  await step(page, "Continue", "define/cascade");

  // ── p3_21 · the cascade, as a linked definition rather than a field. ──
  await expect(page.locator("body")).toContainText("linked-definition");
  await fill(page, "roleLevel", "2");
  await fill(page, "trainedBy", FIX_DEFINITION);
  await fill(page, "trainedByVersion", "1");
  // On an event-based draft Continue goes to the parties screen, and the
  // screen says why it departs from the reference's own next step.
  await expect(page.locator("body")).toContainText(/the counting basis is/i);
  await step(page, "Continue", "define/parties");

  // ── p3_8 · parties: who works, who pays, who sits between. ──
  await expect(page.locator("#stepcounter")).toContainText("Parties · 5 of 9");
  await fill(page, "performerRole", "Community health worker");
  await choose(page, "partyType", "Individual");
  await fill(page, "attesterFunctions", "submit-work-evidence");
  await expect(page.locator("body")).toContainText(/A unit and a claim are separable/i);
  await step(page, "Continue", "define/evidence");

  // ── p3_9 · the evidence-to-tier map. Provenance rules and a ceiling; no
  //    tier is written onto anything. ──
  await expect(page.locator("#stepcounter")).toContainText("Evidence · 6 of 9");
  await expect(page.locator("body")).toContainText("the floor — no requirements");
  await expect(page.locator("body")).toContainText("Tier 1 is strongest, Tier 2 is supervised evidence, and Tier 3 is worker asserted evidence.");
  // Reference numbering: 1 is strongest, so a ceiling of 1 caps nothing.
  await choose(page, "tierCeiling", "1");
  await fill(page, "checkIntensity", "Sample — 1 in 10");
  await fill(page, "workerSummary", "You handed out bednets and recorded each household you visited.");
  await fill(page, "evidencePlain",
    "The programme's own system has your visit recorded.\nYour supervisor confirmed the day's round.");
  // Tier 2 requires the household id; tier 1 (the strongest) requires both.
  // This is what the dry run will judge rows against.
  await page.locator('[data-requires="1"]').fill("household_id, beneficiary_count");
  await page.locator('[data-requires="2"]').fill("household_id");
  // p3_8 was redrawn to the design pack's frame (75f02b7), which carries no
  // green callout here; the stored-tier sentence lives on p3_22's derived
  // tier, asserted below.
  await step(page, "Continue", "define/source");

  // ── p3_22 · the source-class choice that CAPS the tier, shown as the
  //    derived consequence it is. ──
  await expect(page.locator("#stepcounter")).toContainText("Source · 7 of 9");
  await fill(page, "sourceSystems", "dhis2-riverside, csv-batch");
  await fill(page, "requiredFields", "household_id, beneficiary_count");
  await choose(page, "sourceClass", "programme-system");
  await choose(page, "captureMethod", "digital-capture");
  await expect(page.locator("body")).toContainText("derived, not stored");
  await expect(page.locator("body")).toContainText(/recalculated every time anyone asks/i);
  // A programme system's digital capture cannot reach tier 3 under these
  // rules, and the screen derives that rather than being told it.
  await expect(page.locator(".kv")).toContainText("Tier 2");
  // The reference's own callout, on the screen that sets the ceiling.
  await expect(page.locator("body"))
    .toContainText("The source decides the highest tier that is structurally possible");

  // ── p3_23 · the draft writes its next-version template NOW, at the point
  //    the reference offers the spreadsheet path — not only after submit. ──
  await step(page, "Use a spreadsheet", "define/template");
  await expect(page.locator("[data-template-header]")).toContainText("household_id", { timeout: 20000 });
  await expect(page.locator("[data-template-header]")).toContainText("beneficiary_count");
  await expect(page.locator("body")).toContainText("The template is tied to this version (v1)");
  await step(page, "Back", "define/source");

  await step(page, "Connect a system", "define/adaptors");

  // ── p3_24 · the adaptor library, told honestly: one implemented class, the
  //    rest named as absent because that is what the service returns. ──
  await expect(page.locator("#stepcounter")).toContainText("Connection · 1 of 5");
  await expect(page.locator("body")).toContainText("csv-batch@1");
  await expect(page.locator(".g-row.dim")).not.toHaveCount(0);
  await expect(page.locator("body")).toContainText("not-implemented");
  // The reference's primary button names a class CREST does not have, so it
  // carries the gap instead of pretending.
  await expect(page.locator(".open-note")).toContainText(/DIGIT HCM is not an implemented adaptor class/i);
  await page.click('button.g-row:has-text("csv-batch@1")');
  await page.waitForTimeout(400);
  await step(page, "Start a new one", "define/mapping");

  // ── p3_25 · mapping their vocabulary onto ours. ──
  await expect(page.locator("#stepcounter")).toContainText("Connection · 2 of 5");
  // The definition's required fields are unmapped, and the screen computes
  // that against the real draft rather than illustrating it.
  await expect(page.locator("body")).toContainText("Unmapped, and required");
  await expect(page.locator('[data-callout="teal"]:has-text("Unmapped, and required")'))
    .toContainText("household_id");
  await page.locator('[data-map="household_id"]').last().fill("household_id");
  await page.locator('[data-map="beneficiary_count"]').last().fill("beneficiary_count");
  await expect(page.locator("body")).toContainText("Matched, but wrong");
  await step(page, "Continue", "define/connect");

  // ── p3_26 · connection details, credentialRef only. ──
  await expect(page.locator("#stepcounter")).toContainText("Connection · 3 of 5");
  // Nothing on this screen may be a secret: no password input anywhere.
  await expect(page.locator('input[type="password"]')).toHaveCount(0);
  await expect(page.locator("body")).toContainText("Nothing on this screen is a secret");
  await fill(page, "systemRef", "dhis2-riverside");
  await fill(page, "endpoint", "https://dhis2.example.org/api");
  await fill(page, "credentialRef", "vault:crest/sources/dhis2-riverside#token");
  // REFUSAL · a secret-shaped setting key. The service refuses it by name,
  // in the same words it would use at submit, because validate runs the same
  // compile the submission does.
  await fill(page, "settingkey", "apiKey");
  await fill(page, "settingvalue", "not-a-real-token");
  await page.click("#settingform button[type=submit]");
  await press(page, "Continue");
  await expect(page.locator("[data-secret-refusal]")).toBeVisible({ timeout: 20000 });
  await expect(page.locator("[data-secret-refusal]"))
    .toContainText(/CREST stores a credentialRef naming where the platform team keeps it, never the value/i);
  expect(page.url(), "a refused connection does not advance the wizard").toContain("define/connect");
  // Corrected, not worked around: the refused key is removed and the screen
  // proceeds. A refusal with no way back would be a dead end.
  await page.click('[data-unset="apiKey"]');
  await fill(page, "settingkey", "orgUnitLevel");
  await fill(page, "settingvalue", "4");
  await page.click("#settingform button[type=submit]");
  await step(page, "Continue", "define/dryrun");

  // ── p3_27 · the dry run. Real adaptor, real strength function, nothing
  //    written — and the tier is DERIVED, which the walk proves by asking the
  //    same rows twice under different provenance. ──
  await expect(page.locator("#stepcounter")).toContainText("Connection · 4 of 5");
  await press(page, "Run the sample");
  await expect(page.locator("[data-dryrow]").first()).toBeVisible({ timeout: 25000 });
  await expect(page.locator("body")).toContainText("committed: false");
  await expect(page.locator("[data-dryrun-note]")).toContainText(/nothing was written/i);
  await expect(page.locator("[data-dryrun-note]")).toContainText(/no unit, no source, no queue entry/i);
  const strongRow = page.locator('[data-dryrow="row 2"]');
  await expect(strongRow).toContainText("Tier 2");
  // A tier with no reason attached would be a number a verifier has to take on
  // trust; the strength function says which rule awarded it and why.
  await expect(strongRow, "the verdict carries its reasons")
    .toContainText(/awards tier 2 for programme-system evidence captured as digital-capture/i);
  // The same rows, weaker provenance: a self-reported source cannot reach the
  // tier a programme system did. Nothing about the rows changed, so the tier
  // cannot have been stored on them.
  await choose(page, "drySourceClass", "self-reported");
  await press(page, "Run the sample");
  await expect(page.locator('[data-dryrow="row 2"]')).toContainText("Tier 3", { timeout: 25000 });
  await choose(page, "drySourceClass", "programme-system");
  await press(page, "Run the sample");
  await expect(page.locator('[data-dryrow="row 2"]')).toContainText("Tier 2", { timeout: 25000 });
  await expect(page.locator("body")).toContainText(/Unresolved workers are not a mapping fault/i);
  await step(page, "Go live", "define/live");

  // ── p3_28 · registered against one version only. ──
  await expect(page.locator("#stepcounter")).toContainText("Connection · 5 of 5");
  await expect(page.locator("body")).toContainText("will bind to v1");
  await expect(page.locator("body")).toContainText("Publishing v2 unbinds the adaptor");
  await expect(page.locator("body")).toContainText(/nothing yet tells the source owner it has happened/i);
  await step(page, "Continue to validation", "define/validation");

  // ── p3_10 · validation posture: one policy field and one infrastructure
  //    field on the same screen, each labelled as what it is. ──
  await expect(page.locator("#stepcounter")).toContainText("Validation · 8 of 9");
  await fill(page, "posture", "District health office");
  await fill(page, "delayDays", "30");
  await fill(page, "issuers", "did:crest:issuer:local");
  await expect(page.locator("body")).toContainText(/refuses a credential from an issuer this list does not name/i);
  await step(page, "Continue", "define/payment");

  // ── p3_11 · the payment split. No amount anywhere on it. ──
  await expect(page.locator("#stepcounter")).toContainText("Payment · 9 of 9");
  await choose(page, "rateSetter", "Someone else will — invite sent");
  await choose(page, "mechanismSetter", "I'll set this");
  // No amount anywhere: the frame's split is two records joined by reference,
  // and the money question is the payment record's, not the definition's.
  await expect(page.locator("body")).toContainText(/stronger evidence to release money than it requires to issue/i);
  await expect(page.locator('input[name="rateamount"], input[name="currency"]')).toHaveCount(0);
  await step(page, "Continue", "define/roles");

  // ── p3_20 · the four project roles. ──
  await page.locator('[data-role="rateSetter"]').fill(P3.approver);
  await page.locator('[data-role="mechanismSetter"]').fill(P3.approver);
  await page.locator('[data-role="validator"]').fill("District health office");
  await page.locator('[data-role="approver"]').fill(P3.approver);
  await expect(page.locator("body")).toContainText(/the authority itself lives in the service that enforces it/i);
  await step(page, "Continue", "define/tranches");

  // ── p3_12 · stacked pay. Shares of a rate nobody has published yet. ──
  await fill(page, "tranchelabel", "On completion");
  await fill(page, "trancheshare", "70%");
  await fill(page, "tranchecondition", "The confirmation window exits");
  await page.click("#trancheform button[type=submit]");
  await expect(page.locator("body")).toContainText("On completion");
  await step(page, "Continue", "define/rules");

  // ── p3_13 · the two kinds of rule, kept apart. ──
  await fill(page, "preconditions", "The worker completed the level-1 training definition.");
  await fill(page, "deductionlabel", "Equipment advance");
  await fill(page, "deductionrule", "10% of each cycle until the advance is cleared");
  await page.click("#deductionform button[type=submit]");
  await expect(page.locator("body")).toContainText("Equipment advance");
  await step(page, "Continue", "define/extend");

  // ── p3_14 · the two kinds of extension, and only one of them is a form. ──
  await fill(page, "extkey", "mgnrega.contractorRef");
  await fill(page, "extlabel", "Contractor reference");
  await choose(page, "exttype", "string");
  await fill(page, "extvalue", "MGN/2026/WD/0841");
  await page.click("#extensionform button[type=submit]");
  await expect(page.locator("body")).toContainText(/is a design finding/i);
  await step(page, "Continue", "define/open");

  // ── p3_18 · nothing left undecided, and the submit that follows from it. ──
  await expect(page.locator("body")).toContainText("ready to submit");
  await expect(page.locator("[data-problem]")).toHaveCount(0);
  await expect(page.locator("body")).toContainText(/awaiting a ratifier who is not you/i);
  await step(page, "Submit for ratification", "define/anatomy");

  // ── p3_19 · the schema under the form, read from the stored version. ──
  await expect(page.locator("body")).toContainText("v1, as stored");
  const baseJson = await page.locator("[data-anatomy-base]").innerText();
  expect(baseJson, "the stored version, not the draft").toContain('"version": 1');
  expect(baseJson).toContain("bednets-distributed");
  expect(baseJson).toContain('"state": "DRAFT"');
  expect(baseJson, "the author is on the record").toContain(P3.author);
  expect(baseJson, "the extension layer is shown separately").not.toContain("mgnrega");
  await expect(page.locator("[data-anatomy-ext]")).toContainText("mgnrega.contractorRef");
  await expect(page.locator("body")).toContainText(/versioned separately/i);

  // The source connection is not transient wizard state: compilation keeps
  // the tested mapping as a LinkedRecord pinned to this exact version.
  const linkedAfterSubmit = await page.evaluate(async ({ svc, id }) => {
    const r = await fetch(`${svc}/v1/definitions/${encodeURIComponent(id)}/linked-records`);
    return r.json();
  }, { svc: DEFSVC, id: JSON.parse(baseJson).id });
  const sourceBinding = linkedAfterSubmit.linkedRecords.find((x) => x.type === "source-binding");
  expect(sourceBinding, "the tested source mapping survives draft compilation").toBeTruthy();
  expect(sourceBinding.payload.definitionVersion).toBe(1);
  expect(sourceBinding.payload.systemRef).toBe("dhis2-riverside");

  // ── REFUSAL · editing after submit. The version is immutable and the draft
  //    is its provenance, so the service refuses a section write on it. ──
  await page.evaluate(() => { location.hash = "#/define/sector"; });
  await settle(page);
  await expect(page.locator(".open-note")).toContainText(/its sections refuse writes/i);
  await expect(page.locator(".open-note")).toContainText("draft_closed");
  await expect(page.locator(".open-note")).toContainText(/that version is immutable/i);
  await press(page, "Continue");
  await expect(page.locator(".errbar")).toContainText(/no longer open|draft_closed/i, { timeout: 20000 });
  expect(page.url(), "a refused write does not advance").toContain("define/sector");

  // ── p3_23 · the template, now that a version exists to derive it from. ──
  await page.evaluate(() => { location.hash = "#/define/template"; });
  await settle(page);
  const header = await page.locator("[data-template-header]").innerText();
  for (const col of ["activity", "worker_id", "period_start", "outcome_value", "outcome_unit",
    "household_id", "beneficiary_count"]) {
    expect(header, `template column ${col}`).toContain(col);
  }
  await expect(page.locator("body")).toContainText("The template is tied to this version");

  // ── p3_15 · the approver signs, and names what stays pending. A DIFFERENT
  //    party: the service refuses a self-ratified version, and these two
  //    personas are two parties so that refusal is load-bearing. ──
  await switchTo(page, request, "approver", "Prof. Ndegwa");
  await page.evaluate(() => { location.hash = "#/ratify"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Review and sign");
  await expect(page.locator("body")).toContainText("awaiting ratification");
  await expect(page.locator("#ratify-read")).toContainText("bednets-distributed");
  // The author is the runtime's specifier party (server-assigned on a fresh
  // stack), shown shortened: its tail is what the read carries.
  await expect(page.locator("#ratify-read")).toContainText(FIX.specifier.slice(-6));
  // The ratifier names what is still open. Nothing prices this unit yet, and
  // that is a real pending field derived from the real record.
  await page.locator('[data-pending="ratePerOutcomeUnit"]').check();
  await fill(page, "pendingextra", "the district office has not confirmed the sampling rate");
  await press(page, "Sign and publish");
  await page.waitForURL(/#\/ratified/, { timeout: 25000 });
  await settle(page);

  // ── p3_16 · two records, one signature. The event log is the second one. ──
  await expect(page.locator("[data-ratified-title]")).toContainText("v1 is active");
  await expect(page.locator("[data-event='RATIFIED']")).toBeVisible();
  await expect(page.locator("[data-event='ACTIVATED']")).toBeVisible();
  await expect(page.locator("[data-event='SUBMITTED']")).toBeVisible();
  // Every act names its actor, and the two names are different parties.
  const trail = await page.locator(".grid-tbl").first().innerText();
  expect(trail, "the submission is the author's act").toContain(FIX.specifier.slice(-6));
  expect(trail, "the signature is the approver's act").toMatch(/RGN/);
  // Ratified WITH pending fields — a real recorded state, named by the
  // ratifier and not by the author.
  await expect(page.locator("[data-pendingfield]").first()).toBeVisible();
  await expect(page.locator("body")).toContainText(/The ratifier named these, not the author/i);
  await expect(page.locator("body")).toContainText("the district office has not confirmed the sampling rate");

  // ── p3_pay · pricing handed to the rate owner. The author's act, not the
  //    approver's, and a record rather than an authority. ──
  await switchTo(page, request, "author", "Amina Yusuf");
  await page.evaluate(() => { location.hash = "#/handoff"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("The definition is signed. Nothing prices it yet.");
  await expect(page.locator("body")).toContainText("unpriced");
  await fill(page, "invited", P3.approver);
  await fill(page, "handoffnote", "Priced per bednet distributed; campaign starts 1 March.");
  await press(page, "Send the invitation");
  await expect(page.locator("[data-handoff]").first()).toBeVisible({ timeout: 25000 });
  await expect(page.locator("body")).toContainText("handed off, awaiting a rate");
  await expect(page.locator(".next")).toContainText(/rate owner/i);
  await expect(page.locator(".open-note")).toContainText(/does not substitute for it/i);
  // The handoff is in the append-only log too, with the inviter's name.
  await page.evaluate(() => { location.hash = "#/ratified"; });
  await settle(page);
  await expect(page.locator("[data-event='PAYMENT_HANDOFF']")).toBeVisible();

  await assertAlive(page, errors, "the P-3 authoring walk");
});

// The other half of the ratification contract: signing with NOTHING pending
// is its own recorded state, distinguishable afterwards from a ratification
// that named fields. Driven through "Clone a version", which also proves that
// submitting a clone APPENDS v2 and leaves v1 exactly as ratified — the
// property that protects every credential already pinned to v1.
test("console: a clone submits as the next version, and ratifying names nothing pending", async ({ page, request }) => {
  const errors = watch(page);
  test.setTimeout(180000);

  await grantP3Approver(request);

  await signInAs(page, request, "author", "Amina Yusuf");
  await page.evaluate(() => { location.hash = "#/definework"; });
  await settle(page);
  await step(page, "Clone a version", "define/sector");
  // A clone carries the seeded version's answers; what that version does not
  // record stays open rather than being guessed at.
  await page.evaluate(() => { location.hash = "#/define/anatomy"; });
  await settle(page);
  const cloned = await page.locator("[data-anatomy-base]").innerText();
  const basedOn = Number(/"version":\s*(\d+)/.exec(cloned)[1]);
  expect(basedOn, "the clone is pinned to the version it came from").toBeGreaterThan(0);
  expect(cloned).toContain(FIX_DEFINITION);
  await expect(page.locator("body")).toContainText(`v${basedOn}, as stored`);

  // The two things the seeded version does not carry, filled in by hand.
  await page.evaluate(() => { location.hash = "#/define/sector"; });
  await settle(page);
  await fill(page, "sector", "health");
  await step(page, "Continue", "define/counting");
  await step(page, "Continue as event", "define/category");
  await page.evaluate(() => { location.hash = "#/define/open"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("ready to submit");
  await step(page, "Submit for ratification", "define/anatomy");
  // APPENDED, not rewritten: the clone became the NEXT version of the same
  // definition. The number is read off the page rather than hardcoded — this
  // suite is expected to run twice against one stack, so the second run
  // appends again, and a test that insisted on v2 would be asserting that
  // nothing had ever happened before it.
  const v2 = await page.locator("[data-anatomy-base]").innerText();
  const appended = Number(/"version":\s*(\d+)/.exec(v2)[1]);
  expect(appended, "a clone submits as a later version, never over its base").toBe(basedOn + 1);
  expect(v2).toContain(FIX_DEFINITION);
  await expect(page.locator("body")).toContainText(`v${appended}, as stored`);

  // v1 is untouched by any of it — the read a credential pinned to v1 still
  // resolves against.
  const v1 = await page.evaluate(async ({ svc, id }) => {
    const r = await fetch(`${svc}/v1/definitions/${encodeURIComponent(id)}?version=1`);
    return r.json();
  }, { svc: DEFSVC, id: FIX_DEFINITION });
  expect(v1.version, "v1 still answers as v1").toBe(1);
  expect(v1.state, "and is still the active, ratified version it was").toBe("ACTIVE");
  expect(v1.classification, "v1 never gained the clone's sector").toBeFalsy();

  // ── Ratified with nothing pending: the approver declares no open fields,
  //    and that is a different record from a list that happens to be empty. ──
  await switchTo(page, request, "approver", "Prof. Ndegwa");
  await page.evaluate(() => { location.hash = "#/ratify"; });
  await settle(page);
  await expect(page.locator("#ratify-read")).toContainText(`v${appended}`);
  await expect(page.locator("[data-pending]").first()).toBeVisible();
  // Nothing is checked and nothing is typed: the signature names no gaps.
  await press(page, "Sign and publish");
  await page.waitForURL(/#\/ratified/, { timeout: 25000 });
  await settle(page);
  await expect(page.locator("[data-ratified-title]")).toContainText(`v${appended} is active`);
  await expect(page.locator("[data-nothing-pending]")).toBeVisible();
  await expect(page.locator("[data-pendingfield]")).toHaveCount(0);
  await expect(page.locator("body")).toContainText(/absence of a declaration is not a declaration of nothing/i);
  await expect(page.locator("[data-event='RATIFIED']")).toBeVisible();

  await assertAlive(page, errors, "the clone-and-ratify walk");
});

// ── The oversight pack (J9 project view, J10 funder view, J11 registry view):
// the reference's anatomy and explanatory copy on the ten console dashboards,
// over live reads. Fixture figures from the reference (Turkana, KES, 84.4%)
// are deliberately NOT asserted — a screen that carried them would be lying.
test("console: the oversight dashboards carry the p2_11–16, v4_1–2 and g4_4–7 pack's copy", async ({ page, request }) => {
  test.setTimeout(150000);
  const errors = watch(page);
  const body = page.locator("body");

  // J9 — the Project Configurator's five frames.
  await consoleSignIn(page, request, "configurator");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await signedInAs(page, "Dr. Alice Mutua");
  await page.evaluate(() => { location.hash = "#/stp"; });
  await settle(page);
  await expect(body).toContainText(/Why the other \d+ fell out — ranked/);
  await expect(body).toContainText("What changed this month");
  await page.evaluate(() => { location.hash = "#/quality"; });
  await settle(page);
  await expect(body).toContainText("system recorded");
  await expect(body).toContainText("The one action on this screen");
  await expect(body).toContainText("There is a system holding this data already");
  await page.evaluate(() => { location.hash = "#/payments"; });
  await settle(page);
  await expect(body).toContainText("The row that crosses a boundary");
  await expect(body).toContainText("One list, two dashboards, and neither of you can close it alone.");
  await page.click('[data-act="unowned"]');
  await expect(body).toContainText(/Cleared but never instructed, or held with nobody holding — \d+/);
  await page.evaluate(() => { location.hash = "#/trace"; });
  await settle(page);
  await expect(body).toContainText("One row worth acting on");
  await expect(body).toContainText("the single most common way a worker quietly loses money");
  await page.click('[data-act="bundle"]');
  await expect(body).toContainText("no bundle endpoint exists");
  await page.evaluate(() => { location.hash = "#/reports"; });
  await settle(page);
  await expect(body).toContainText("Validated work value");
  await expect(body).toContainText("Unspent against allocation");
  await expect(body).toContainText("Off by default");
  await expect(body).toContainText("Turns an aggregate report into a list of named people and their earnings");
  await expect(body).toContainText("Needs a governance decision, not a checkbox");
  await expect(body).toContainText("Your project is configured and running");
  await page.click('[data-act="preview"]');
  await expect(body).toContainText("what is on this screen is the preview");
  await signedInAs(page, "Dr. Alice Mutua");
  await assertAlive(page, errors, "J9 project dashboards");

  // J10 — the funder's portfolio (v4_1) and the drill beneath it (v4_2).
  await switchTo(page, request, "funder", "Funding oversight");
  await page.evaluate(() => { location.hash = "#/portfolio"; });
  await settle(page);
  await expect(body).toContainText("the number that matters");
  await expect(body).toContainText("Not the largest number, the worst ratio");
  await expect(body).toContainText("Stuck");
  await page.locator('[data-act="open-project"]').first().click();
  await expect(body).toContainText("What was delivered, on");
  await expect(body).toContainText("What is deliberately absent");
  await expect(body).toContainText("Nothing on this screen names a worker, and nothing needs to.");
  await expect(body).toContainText("Follow the tier column");
  await expect(body).toContainText("An unanswered query is not escalated anywhere.");
  // The drill groups real claims by the unit's geography and definition: the
  // story's place and outcome unit are live reads, never the reference's Loima.
  await expect(body).toContainText("Where the work happened");
  await expect(body).toContainText("Riverside");
  await expect(body).toContainText("bednets-distributed");
  await expect(body).not.toContainText("Loima");
  await page.click('[data-act="query"]');
  await expect(body).toContainText("has no object and no endpoint yet");
  await page.click('[data-act="individual"]');
  await expect(body).toContainText("Deliberately absent from this view");
  await signedInAs(page, "Funding oversight");
  await assertAlive(page, errors, "J10 funder portfolio");

  // J11 — the registry custodian's four frames.
  await switchTo(page, request, "custodian", "Otieno");
  await page.evaluate(() => { location.hash = "#/coverage"; });
  await settle(page);
  await expect(body).toContainText("Gap to close");
  await expect(body).toContainText("people missing");
  await page.evaluate(() => { location.hash = "#/dupes"; });
  await settle(page);
  await expect(body).toContainText("open pairs");
  await expect(body).toContainText("median time to resolve");
  await expect(body).toContainText("merged without confirmation");
  await expect(body).toContainText("Duplicate rate falls if pairs are closed as different without being looked at.");
  await expect(body).toContainText("which is why low confidence must never auto-merge");
  await page.click('[data-act="bulk"]');
  await expect(body).toContainText("a batch cannot carry one");
  await page.evaluate(() => { location.hash = "#/reuse"; });
  await settle(page);
  await expect(body).toContainText("used by one project only");
  await expect(body).toContainText("used by two or more");
  await expect(body).toContainText("never again, never retired");
  await expect(body).toContainText("You reviewed the registry");
  await expect(body).toContainText("An assigned gap that nobody works is not escalated.");
  await signedInAs(page, "Otieno");
  await assertAlive(page, errors, "J11 registry dashboards");
});

// ── #123: the invitation in front of the first-login bootstrap. The org admin
// creates a person and a role from People & roles and gets a one-time link;
// a stranger the registry has never seen claims it with their own login and
// lands in the console AS that person, with the persona derived from the
// grant. The return leg is driven directly (#/auth?token=…) with a mock
// issuer token, which is exactly what the eSignet callback hands the door.
test("console: an invited person claims their record with their own login, once", async ({ page, request }) => {
  test.setTimeout(120000);
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  const body = page.locator("body");

  await consoleSignIn(page, request, "orgadmin");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await page.evaluate(() => { location.hash = "#/people"; });
  await settle(page);
  await expect(body).toContainText("Invite a person");
  await page.fill('[data-field="invite-name"]', "Invited Author " + stamp);
  await page.selectOption('[data-field="invite-contact-kind"]', "email");
  await page.fill('[data-field="invite-contact"]', `author-${stamp}@example.org`);
  await page.selectOption('[data-field="invite-function"]', "specify-definition");
  await page.click('[data-act="invite"]');
  await expect(page.locator("[data-invite-link]")).toBeVisible({ timeout: 20000 });
  await expect(body).toContainText("The invitation, shown once");
  const link = (await page.locator("[data-invite-link]").innerText()).trim();
  const code = link.split("#/claim/")[1];
  expect(code, "the link carries a claim code").toBeTruthy();
  // The grant must have landed, or the persona below cannot derive.
  await expect(body).not.toContainText("The record exists; the role does not yet");

  // A stranger: a subject the registry has never bound.
  const mint = async (sub) => {
    const r = await request.post(G2.oidc + "/token", { data: { sub, aud: "crest", expiresIn: "1h" } });
    const d = await r.json();
    return d.accessToken || d.access_token || d.token;
  };
  const token = await mint("invitee|" + stamp);
  await page.evaluate(() => sessionStorage.clear());
  await page.goto("/console/#/claim/" + code);
  await settle(page);
  await expect(body).toContainText("You were invited to CREST Console");
  await expect(page.locator("#claim-esignet")).toBeVisible();
  // The button would hand the browser to eSignet; the return leg is what
  // the door acts on, so drive it with the token the callback would carry.
  await page.evaluate((c) => sessionStorage.setItem("crest.console.claim", c), code);
  await page.evaluate((t) => { location.hash = "#/auth?token=" + encodeURIComponent(t); }, token);
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await settle(page);
  await expect(page.locator(".appbar .who-label")).toContainText("Invited Author " + stamp);
  await expect(page.locator(".appbar .who-label")).toContainText("Work Definition Author");
  await assertAlive(page, errors, "claimed invitation");

  // Spent: a second stranger with the same code is told so, in words.
  const token2 = await mint("latecomer|" + stamp);
  await page.evaluate(() => sessionStorage.clear());
  await page.goto("/console/#/claim/" + code);
  await settle(page);
  await page.evaluate((c) => sessionStorage.setItem("crest.console.claim", c), code);
  await page.evaluate((t) => { location.hash = "#/auth?token=" + encodeURIComponent(t); }, token2);
  await expect(body).toContainText("That invitation was already claimed", { timeout: 20000 });
});

// The operator publishes the terms an organisation can be admitted on, from
// the console — on a clean deployment nothing can be admitted until this
// happens, and only the operator's own session is accepted for it.
test("console: the operator publishes terms, and only the operator can", async ({ page, request }) => {
  const errors = watch(page);
  const stamp = Date.now().toString().slice(-6);
  // The gate is the deployment's own operator, read from its self-description;
  // a stack that names a different party than the fixture organisation cannot
  // be walked as the operator by this suite, and says so rather than failing.
  const inst = await (await request.get(G2.parties + "/v1/instance")).json();
  test.skip(inst.instance.operatorPartyId !== FIX.org,
    "this stack names " + inst.instance.operatorPartyId + " as operator, not the fixture organisation");
  await consoleSignIn(page, request, "instance");
  await page.locator("#logout").waitFor({ state: "visible", timeout: 20000 });
  await page.evaluate(() => { location.hash = "#/instance/consent"; });
  await settle(page);
  await expect(page.locator("body")).toContainText("Terms an organisation can be on");
  await page.fill('[data-field="terms-name"]', "Delivery terms " + stamp);
  await page.check('[data-perm="register-workers"]');
  await page.click('[data-act="publish-terms"]');
  await expect(page.locator("[data-terms-row]", { hasText: "Delivery terms " + stamp })).toBeVisible({ timeout: 20000 });
  await assertAlive(page, errors, "terms published by the operator");

  // A person who is not the operator is refused by the registry, by name.
  const r = await asParty(request, FIX.custodian, "POST", "/v1/terms", { name: "Not mine " + stamp, permissions: ["register-workers"] });
  expect(r.status(), "a non-operator publishing terms").toBe(403);
  expect((await r.json()).code).toBe("not_the_operator");
});
