import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { webcrypto } from "node:crypto";
import ts from "typescript";

// The app has no test bundler dependency. Transpile the browser module and its
// node:test contract into a temporary ESM directory, then let Node's real test
// runner execute the same implementation used by Vite.
if (!globalThis.crypto) Object.defineProperty(globalThis, "crypto", { value: webcrypto });
const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, "..", "src");
const temp = await mkdtemp(join(tmpdir(), "crest-verify-offline-"));
try {
  let offline = await readFile(join(src, "offline.ts"), "utf8");
  offline = offline.replace('import { api } from "@crest/api";', 'const api = { get: async () => { throw new Error("unexpected network request during offline contract test"); } };');
  const compilerOptions = { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext };
  await writeFile(join(temp, "offline.mjs"), ts.transpileModule(offline, { compilerOptions }).outputText);
  let tests = await readFile(join(src, "offline.test.ts"), "utf8");
  tests = tests.replace('from "./offline"', `from "${join(temp, "offline.mjs")}"`);
  await writeFile(join(temp, "offline.test.mjs"), ts.transpileModule(tests, { compilerOptions }).outputText);
  await import(pathToFileURL(join(temp, "offline.test.mjs")));
} finally {
  await rm(temp, { recursive: true, force: true });
}
