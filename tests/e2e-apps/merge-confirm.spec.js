// Synthetic contract regression for the worker duplicate-hold confirmation
// route. It uses a disposable browser session and intercepts the confirmation
// request; it never writes to the acceptance API or creates fixture records.
const { test, expect } = require("@playwright/test");

test("worker merge confirmation preserves auth return and posts consent in the body", async ({ page }) => {
  const hold = "ui-hold-contract";
  const survivor = "did:crest:party:ui-survivor";
  await page.goto(`/worker/#/merge-confirm/${hold}?survivor=${encodeURIComponent(survivor)}`);
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem("crest.worker.pending-review"))).toContain(hold);

  await page.evaluate(() => sessionStorage.setItem("crest.worker.session", JSON.stringify({
    token: "synthetic-worker-token",
    me: "did:crest:party:ui-worker",
    label: "Synthetic worker",
  })));
  await page.reload();
  await expect(page.getByRole("heading", { name: "Confirm the surviving record" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Confirm this surviving record" })).toBeDisabled();

  let seen;
  await page.route(`**/v1/holds/${hold}/confirm`, async (route) => {
    seen = { url: route.request().url(), method: route.request().method(), body: route.request().postDataJSON() };
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ holdId: hold, survivorPartyId: survivor }) });
  });
  await page.getByRole("checkbox").check();
  await page.getByRole("button", { name: "Confirm this surviving record" }).click();
  await expect(page.getByRole("heading", { name: "Your confirmation was recorded" })).toBeVisible();
  expect(seen.method).toBe("POST");
  expect(new URL(seen.url).search).toBe("");
  expect(seen.body).toEqual({ survivorPartyId: survivor, confirmationMethod: "worker-web" });
  expect(await page.evaluate(() => sessionStorage.getItem("crest.worker.pending-review"))).toBeNull();
});
