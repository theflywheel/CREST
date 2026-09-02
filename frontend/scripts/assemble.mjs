// Lay out the compose door's document root: the rebuilt apps from their Vite
// dist, everything not yet ported copied straight from apps/. The output
// (frontend/dist-site) is what infra/compose serves on :59110 — the same
// shape the Railway Dockerfiles assemble in their build stages.
import { cpSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url))); // frontend/
const repo = dirname(root);
const out = join(root, "dist-site");

rmSync(out, { recursive: true, force: true });
mkdirSync(out, { recursive: true });

// Rebuilt (Vite): one entry per ported door.
// The field door keeps its /enrolment/ URL path — bookmarks and the proxy
// allowlist both know it by that name.
const built = {
  worker: "apps/worker/dist",
  verify: "apps/verify/dist",
  enrolment: "apps/field/dist",
  console: "apps/console/dist",
};
// Not yet ported: served as-is from apps/ (with the legacy shared assets).
const legacy = [];

cpSync(join(repo, "apps/index.html"), join(out, "index.html"));
// The landing page styles itself from the design system, served as /crest.css.
cpSync(join(root, "packages/ui/src/styles.css"), join(out, "crest.css"));
for (const dir of legacy) cpSync(join(repo, "apps", dir), join(out, dir), { recursive: true });
for (const [name, dist] of Object.entries(built)) cpSync(join(root, dist), join(out, name), { recursive: true });

console.log("assembled", out);
