// The CREST services, as the browser reaches them.
//
// Dev: the compose stack's published ports on localhost, CORS-admitted via
// CREST_CORS_ORIGINS. A deployment overrides window.CREST_SERVICES before
// this module loads. Login is the mock OIDC issuer in dev; a real deployment
// replaces startLogin with an eSignet redirect — the shape of everything after
// the token is identical, which is the point of #89.

// Host-relative: the services ride the same host the page was served from,
// on the compose stack's published ports — so the app works identically on
// localhost and over a tailnet/LAN address, as long as CREST_CORS_ORIGINS
// names that origin too.
const host = location.hostname || "localhost";
const at = port => `http://${host}:${port}`;
const defaults = {
  parties:      at(59001),
  definitions:  at(59002),
  evidence:     at(59003),
  confirmation: at(59004),
  verification: at(59005),
  payments:     at(59006),
  notify:       at(59007),
  oidc:         at(59103),
};
export const services = Object.assign({}, defaults, window.CREST_SERVICES || {});

let token = null, onBehalfOf = null;
export function setSession(t, behalf) { token = t; onBehalfOf = behalf || null; }
export function actingFor(partyId) { onBehalfOf = partyId || null; }

class ApiError extends Error {
  constructor(status, code, message) {
    super(message || code || ("HTTP " + status));
    this.status = status; this.code = code;
  }
}
export { ApiError };

async function call(service, method, path, body, contentType) {
  const headers = {};
  if (token) headers["Authorization"] = "Bearer " + token;
  if (onBehalfOf) headers["X-CREST-On-Behalf-Of"] = onBehalfOf;
  let payload;
  if (body !== undefined && body !== null) {
    headers["Content-Type"] = contentType || "application/json";
    payload = contentType ? body : JSON.stringify(body);
  }
  const res = await fetch(services[service] + path, { method, headers, body: payload });
  const text = await res.text();
  let doc = null;
  try { doc = text ? JSON.parse(text) : null; } catch { doc = null; }
  if (!res.ok) throw new ApiError(res.status, doc && doc.code, doc && doc.message || text);
  return doc;
}

export const api = {
  get: (svc, path) => call(svc, "GET", path),
  post: (svc, path, body) => call(svc, "POST", path, body),
  postRaw: (svc, path, body, ct) => call(svc, "POST", path, body, ct),
  del: (svc, path) => call(svc, "DELETE", path),
};

// Dev login: mint a token from the mock issuer for a stable per-browser
// subject, then bind it to the chosen party through the real endpoint —
// self-proof by token possession, the same first-login path the services
// define (#102). The provider subject is deterministic per party so a page
// reload is the same person.
export async function loginAs(partyId, viaActorToken) {
  const sub = "web|" + partyId;
  const minted = await fetch(services.oidc + "/token", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ sub, aud: "crest", expiresIn: "12h" }),
  }).then(r => r.json());
  const t = minted.accessToken || minted.access_token || minted.token;
  const prev = token; token = t; onBehalfOf = null;
  try {
    // Idempotent: appendBinding returns the party unchanged when this exact
    // binding already exists.
    const cfg = await fetch(services.oidc + "/.well-known/openid-configuration").then(r => r.json());
    const subject = await pairwise(cfg.issuer, sub);
    await call("parties", "POST", "/v1/parties/" + encodeURIComponent(partyId) + "/identity-bindings",
      { provider: "mock-oidc", providerClass: "generic-oidc", subjectRef: subject });
  } catch (e) {
    token = prev;
    throw e;
  }
  return t;
}

// The deployment's own pairwise derivation (HMAC-SHA256 under its salt) runs
// server-side; the browser cannot and must not know the salt. What the browser
// CAN do is bind by self-proof: the services compare the token's salted
// subject to the subjectRef being bound — so we ask the services what our own
// salted subject is by... we cannot. Instead the dev issuer and stack share
// CREST_SUBJECT_SALT, and the mock issuer exposes the derivation for dev:
async function pairwise(issuer, sub) {
  // Dev-only: the harness derives this with the shared salt. The mock OIDC
  // exposes it so the browser can bind without knowing the salt.
  const r = await fetch(services.oidc + "/dev/pairwise?sub=" + encodeURIComponent(sub));
  if (!r.ok) throw new Error("the dev issuer cannot derive the pairwise subject; is mock-oidc current?");
  const d = await r.json();
  return d.subject;
}
