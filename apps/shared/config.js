// Where the services are, decided before app code loads.
//
// Compose dev serves the journey apps on :59110 with each service on its own
// published port; every other origin — the Railway deployment — reaches the
// services through the same-origin /api/ proxy, whose nginx refuses
// /internal/* so the service-only routes never face the internet (#102).
if (location.port !== "59110") {
  window.CREST_SERVICES = {
    parties:      "/api/crest-registry",
    definitions:  "/api/crest-definitions",
    evidence:     "/api/crest-evidence",
    confirmation: "/api/crest-confirmation",
    verification: "/api/crest-verification",
    payments:     "/api/crest-payments",
    notify:       "/api/crest-notify",
    oidc:         "/api/crest-mock-oidc",
  };
}
