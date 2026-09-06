import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { HashRouter } from "react-router-dom";
import "@crest/ui/styles.css";
import { FieldProvider } from "./state";
import { App } from "./App";

if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    void navigator.serviceWorker.register("./sw.js", { scope: "./" }).catch(() => {
      // App shell caching is an enhancement; API errors and queue storage
      // failures remain visible through the application itself.
    });
  });
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <HashRouter>
      <FieldProvider>
        <App />
      </FieldProvider>
    </HashRouter>
  </StrictMode>,
);
