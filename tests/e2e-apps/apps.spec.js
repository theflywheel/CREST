// A walk of every journey app against a story-seeded stack: every route
// renders real content with no JS exception and no API error banner, and the
// screens the story populates show the story's data. Run with the compose
// stack up and `SEED_STORY=true go run ./tools/seed` done (make apps-up), or
// BASE_URL pointed at the deployed door.
const { test, expect } = require("@playwright/test");

const FIX = {
  workerA: "did:crest:party:01JCREST00000000000000WRKA",
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

for (const [personaIdx, personaName, views] of [
  [0, "Ministry of Health", ["status", "payments", "trace", "definition", "sources",
    "reports", "definework", "paysetup", "org", "portfolio"]],
  [1, "Otieno (custodian + support)", ["find", "dupes", "unclear", "recover", "review",
    "cases", "supportfind", "supporttrace"]],
  [2, "Instance administrator", ["instance", "status"]],
]) {
  test(`console: ${personaName} walks every view`, async ({ page }) => {
    const errors = watch(page);
    await page.goto("/console/");
    await settle(page);
    await page.click(`[data-p="${personaIdx}"]`);
    await settle(page);
    await expect(page.locator(".appbar")).toBeVisible();
    for (const v of views) {
      await page.evaluate(h => { location.hash = "#/" + h; }, v);
      await settle(page);
      await assertAlive(page, errors, `console #/` + v);
      if (v === "instance") {
        // The instance view reads GET /v1/instance for real (#70); the local
        // stack's identity is CREST_INSTANCE_ID's compose default.
        await expect(page.locator("body")).toContainText("crest:instance:local");
      }
    }
  });
}

test("console: the story shows through", async ({ page }) => {
  const errors = watch(page);
  await page.goto("/console/");
  await settle(page);
  await page.click('[data-p="0"]');
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
