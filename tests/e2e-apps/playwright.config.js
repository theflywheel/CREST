// BASE_URL points at the LOCAL compose `apps` door (:59110), the default.
// Since 2026-09-09 (#155 phase 4) these suites are local-stack only: they mint
// dev-issuer tokens and assert on the seeded story world, and the deployed
// fleet runs neither — eSignet is its only trusted issuer and its world is
// whatever real people created through the doors. apps.spec.js, fidelity.spec.js
// and journeys.mjs refuse a non-local BASE_URL rather than pretend.
const { defineConfig } = require("@playwright/test");
module.exports = defineConfig({
  testDir: ".",
  timeout: 60000,
  expect: { timeout: 15000 },
  retries: 0,
  use: {
    baseURL: process.env.BASE_URL || "http://localhost:59110",
    viewport: { width: 1280, height: 900 },
  },
  reporter: [["list"]],
});
