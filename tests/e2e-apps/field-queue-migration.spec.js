const { test, expect } = require("@playwright/test");

const pending = { name: "Legacy Pending Worker", phone: "+15550100991", rosterId: "LEGACY-PENDING", at: Date.now() };
const completed = { name: "Legacy Completed Worker", phone: "+15550100992", rosterId: "LEGACY-DONE", at: Date.now(), partyId: "did:crest:party:legacy-complete" };
const actorId = "did:crest:party:runtime-supervisor";
const contextId = "crest:context:legacy-project";

async function stubFieldSession(page) {
  const mutations = [];
  await page.route("**/token", route => route.fulfill({ json: { accessToken: "synthetic-field-token" } }));
  await page.route("**/v1/**", async route => {
    const req = route.request();
    if (req.method() !== "GET") mutations.push(req.url());
    const path = new URL(req.url()).pathname;
    let body = {};
    if (path.endsWith("/auth/me")) body = { partyId: actorId };
    else if (path.endsWith("/parties/" + actorId)) body = { displayName: "Runtime supervisor" };
    else if (path.endsWith("/authorizations/mine")) {
      body = { authorizations: [{ scope: { kind: "context", contextId } }] };
    } else if (path.endsWith("/definitions")) body = { definitions: [] };
    else if (path.endsWith("/sources")) body = { sources: [] };
    await route.fulfill({ json: body });
  });
  return mutations;
}

async function encryptedCounts(page) {
  return page.evaluate(async () => {
    const db = await new Promise((resolve, reject) => {
      const req = indexedDB.open("crest.field.offline.v1", 2);
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    const tx = db.transaction(["registrations", "completed"], "readonly");
    const read = (store) => new Promise((resolve, reject) => {
      const req = tx.objectStore(store).getAll();
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    const [registrations, done] = await Promise.all([read("registrations"), read("completed")]);
    return {
      registrations: registrations.length,
      completed: done.length,
      ids: [...registrations, ...done].map((row) => row.operationId),
      shapes: [...registrations, ...done].map((row) => Object.keys(row).sort()),
    };
  });
}

async function openLegacyQueue(page, preventCleanup = false) {
  const mutations = await stubFieldSession(page);
  await page.addInitScript(({ pending, completed, preventCleanup }) => {
    localStorage.setItem("crest.enrolment.queue", JSON.stringify([pending]));
    sessionStorage.setItem("crest.enrolment.done", JSON.stringify([completed]));
    if (preventCleanup) {
      const remove = Storage.prototype.removeItem;
      Storage.prototype.removeItem = function (key) {
        if (key === "crest.enrolment.queue" || key === "crest.enrolment.done") throw new Error("storage cleanup blocked");
        return remove.call(this, key);
      };
    }
  }, { pending, completed, preventCleanup });
  await page.goto("/enrolment/");
  await page.locator("[data-login]").click();
  const button = page.locator("#import-legacy-queue");
  await expect(button).toBeEnabled({ timeout: 20000 });
  await button.click();
  await expect(page.locator("body")).toContainText(pending.name);
  await expect(page.locator("body")).toContainText(completed.name);
  expect(mutations).toEqual([]);
}

test("field queue encrypts legacy registrations before clearing plaintext", async ({ page }) => {
  await openLegacyQueue(page);
  expect(await page.evaluate(() => localStorage.getItem("crest.enrolment.queue"))).toBeNull();
  expect(await page.evaluate(() => sessionStorage.getItem("crest.enrolment.done"))).toBeNull();
  const stored = await encryptedCounts(page);
  expect(stored.registrations).toBe(1);
  expect(stored.completed).toBe(1);
  expect(stored.ids.every((id) => id.startsWith("field-legacy-"))).toBeTruthy();
  expect(stored.shapes).toEqual([
    ["ciphertext", "iv", "operationId"],
    ["ciphertext", "iv", "operationId"],
  ]);
});

test("field queue keeps legacy source when cleanup fails and retry does not duplicate", async ({ page }) => {
  await openLegacyQueue(page, true);
  expect(await page.evaluate(() => localStorage.getItem("crest.enrolment.queue"))).not.toBeNull();
  expect(await page.evaluate(() => sessionStorage.getItem("crest.enrolment.done"))).not.toBeNull();
  const first = await encryptedCounts(page);
  await page.reload();
  const button = page.locator("#import-legacy-queue");
  await expect(button).toBeEnabled({ timeout: 20000 });
  await button.click();
  expect(await encryptedCounts(page)).toEqual(first);
});
