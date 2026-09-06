// Synthetic browser regression for the worker review acknowledgement contract.
// This deliberately stubs the confirmation API and uses disposable values; it
// is not acceptance evidence and must not be run against seeded business data.
const { test, expect } = require("@playwright/test");

test("worker review route survives initial empty state and keeps acknowledgement token in POST body", async ({ page }) => {
  const claimId = "crest:claim:synthetic-review-claim";
  const token = "synthetic-review-token-do-not-use";
  const requests = [];
  let releaseWindow;
  const windowReady = new Promise((resolve) => { releaseWindow = resolve; });

  await page.route("**/v1/windows/**", async (route) => {
    const request = route.request();
    requests.push({ method: request.method(), url: request.url(), body: request.postData() });
    expect(request.url()).not.toContain(token);
    if (request.method() === "GET") {
      // Leave the first render with reviewWindow undefined long enough to prove
      // it does not dereference the not-yet-loaded response.
      await windowReady;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ contextId: "crest:context:synthetic", closesAt: "2026-09-07T12:00:00Z", reach: "reached" }),
      });
      return;
    }
    expect(request.method()).toBe("POST");
    expect(new URL(request.url()).pathname).toContain("/ack");
    expect(JSON.parse(request.postData() || "{}")).toEqual({ token });
    await route.fulfill({ status: 200, contentType: "application/json", body: "{}" });
  });

  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(String(error)));

  await page.addInitScript(([partyId, sessionToken]) => {
    sessionStorage.setItem("crest.worker.session", JSON.stringify({ token: sessionToken, me: partyId }));
  }, ["did:crest:party:synthetic-review-worker", "synthetic-session-token"]);
  await page.goto("/worker/#/review/" + encodeURIComponent(claimId) + "?token=" + encodeURIComponent(token));

  // The screen shell exists while the asynchronous window read is pending.
  await expect(page.locator(".screen")).toHaveCount(1);
  expect(pageErrors).toEqual([]);
  releaseWindow();

  await expect(page.getByRole("button", { name: "I have received this review" })).toBeVisible();
  expect(page.url()).not.toContain(token);
  expect(page.url()).not.toContain("token=");
  const read = requests.find((request) => request.method === "GET");
  expect(decodeURIComponent(new URL(read.url).pathname)).toContain(claimId);
  await page.getByRole("button", { name: "I have received this review" }).click();
  await expect.poll(() => requests.some((request) => request.method === "POST")).toBe(true);
  const ack = requests.find((request) => request.method === "POST");
  expect(ack.url).not.toContain(token);
  expect(JSON.parse(ack.body)).toEqual({ token });
  expect(pageErrors).toEqual([]);
});
