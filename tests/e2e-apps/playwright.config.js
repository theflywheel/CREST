// Point BASE_URL at any door serving the journey apps: the compose `apps`
// service (default, :59110) or the Railway deployment. The suite only reads
// and uses the same confirm/dispute writes a demo visitor would.
const { defineConfig } = require("@playwright/test");
module.exports = defineConfig({
  testDir: ".",
  timeout: 45000,
  retries: 0,
  use: {
    baseURL: process.env.BASE_URL || "http://localhost:59110",
    viewport: { width: 1280, height: 900 },
  },
  reporter: [["list"]],
});
