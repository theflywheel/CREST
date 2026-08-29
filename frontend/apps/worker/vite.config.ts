import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The build is served from two places: the combined door at /worker/
// (Dockerfile.apps) and its own Railway service at the domain root
// (Dockerfile.app, APP=worker) — so the base is relative. Hash routing, so
// no history fallback is needed.
export default defineConfig({
  base: "./",
  plugins: [react()],
});
