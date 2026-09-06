import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { HashRouter } from "react-router-dom";
import "@crest/ui/styles.css";
import { VerifyProvider } from "./state";
import { App } from "./App";

if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    void navigator.serviceWorker.register("./sw.js", { scope: "./" }).catch(() => {
      // The verifier stays usable online when the browser disallows workers.
    });
  });
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <HashRouter>
      <VerifyProvider>
        <App />
      </VerifyProvider>
    </HashRouter>
  </StrictMode>,
);
