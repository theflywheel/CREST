// Where the services are, decided before app.js loads.
//
// Compose dev serves the app on :59100 with each service on its own published
// port. Every other origin — the Railway deployment — reaches the services
// through the same-origin /api/ proxy, whose nginx refuses /internal/* so the
// service-only routes never face the internet (#102).
if (location.port !== "59100") {
  window.CREST_SERVICES = {
    parties:      "/api/crest-registry",
    definitions:  "/api/crest-definitions",
    evidence:     "/api/crest-evidence",
    confirmation: "/api/crest-payments", // the payments application answers the window (#129)
    verification: "/api/crest-verification",
    payments:     "/api/crest-payments",
    oidc:         "/api/crest-mock-oidc",
  };
}
