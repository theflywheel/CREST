const { test, expect } = require("@playwright/test");

test("field development login uses the authenticated registry identity without rebinding", async ({ page }) => {
  const partyId = "did:crest:party:runtime-supervisor";
  const mutations = [];
  await page.route("**/token", route => route.fulfill({ json: { accessToken: "synthetic-field-token" } }));
  await page.route("**/v1/**", async route => {
    const req = route.request();
    if (req.method() !== "GET") mutations.push(req.url());
    const path = new URL(req.url()).pathname;
    let body = {};
    if (path.endsWith("/auth/me")) body = { partyId };
    else if (path.endsWith("/parties/" + partyId)) body = { displayName: "Runtime supervisor" };
    else if (path.endsWith("/authorizations/mine")) body = { authorizations: [] };
    await route.fulfill({ json: body });
  });
  await page.goto("/enrolment/");
  await page.locator("[data-login]").click();
  await expect.poll(() => page.evaluate(() => {
    return JSON.parse(sessionStorage.getItem("crest.field.session") || "{}").partyId;
  })).toBe(partyId);
  expect(mutations).toEqual([]);
});
