import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { HashRouter } from "react-router-dom";
import "@crest/ui/styles.css";
import { ConsoleProvider } from "./state";
import { App } from "./App";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <HashRouter>
      <ConsoleProvider>
        <App />
      </ConsoleProvider>
    </HashRouter>
  </StrictMode>,
);
