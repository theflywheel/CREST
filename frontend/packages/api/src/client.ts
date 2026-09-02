// The CREST services, as the browser reaches them — TS port of
// apps/shared/api.js, behaviour preserved verbatim.
//
// Login is the mock OIDC issuer in dev; a real deployment replaces
// startLogin with an eSignet redirect — the shape of everything after the
// token is identical, which is the point of #89.

import { services, type ServiceName } from "./config";

let token: string | null = null;
let onBehalfOf: string | null = null;

export function setSession(t: string | null, behalf?: string | null) {
  token = t;
  onBehalfOf = behalf || null;
}
export function actingFor(partyId: string | null) {
  onBehalfOf = partyId || null;
}

export class ApiError extends Error {
  status: number;
  code: string | null;
  constructor(status: number, code: string | null, message?: string | null) {
    super(message || code || "HTTP " + status);
    this.status = status;
    this.code = code;
  }
}

async function call(
  service: ServiceName,
  method: string,
  path: string,
  body?: unknown,
  contentType?: string,
): Promise<any> {
  const headers: Record<string, string> = {};
  if (token) headers["Authorization"] = "Bearer " + token;
  if (onBehalfOf) headers["X-CREST-On-Behalf-Of"] = onBehalfOf;
  let payload: BodyInit | undefined;
  if (body !== undefined && body !== null) {
    headers["Content-Type"] = contentType || "application/json";
    payload = contentType ? (body as BodyInit) : JSON.stringify(body);
  }
  const res = await fetch(services[service] + path, { method, headers, body: payload });
  const text = await res.text();
  let doc: any = null;
  try {
    doc = text ? JSON.parse(text) : null;
  } catch {
    doc = null;
  }
  if (!res.ok) throw new ApiError(res.status, doc && doc.code, (doc && doc.message) || text);
  return doc;
}

export const api = {
  get: (svc: ServiceName, path: string) => call(svc, "GET", path),
  post: (svc: ServiceName, path: string, body?: unknown) => call(svc, "POST", path, body),
  // The J3 configuration endpoints are PUTs by design: one record per key,
  // idempotent, re-answering replaces the answer rather than appending one.
  put: (svc: ServiceName, path: string, body?: unknown) => call(svc, "PUT", path, body),
  postRaw: (svc: ServiceName, path: string, body: unknown, ct: string) => call(svc, "POST", path, body, ct),
  del: (svc: ServiceName, path: string) => call(svc, "DELETE", path),
};

// Dev login: mint a token from the mock issuer for the story seeder's own
// subject for this party, then bind it through the real endpoint. Self-bind is
// accepted only for a never-bound party or the exact subject already bound
// (#102) — so the dev login IS the story's person: the same "story|" subject,
// making the bind the idempotent same-subject re-bind, and a first-login
// bootstrap on anything the story never bound.
export async function loginAs(partyId: string): Promise<string> {
  const sub = "story|" + partyId.replace("did:crest:party:", "");
  const minted = await fetch(services.oidc + "/token", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ sub, aud: "crest", expiresIn: "12h" }),
  }).then((r) => r.json());
  const t: string = minted.accessToken || minted.access_token || minted.token;
  const prev = token;
  token = t;
  onBehalfOf = null;
  try {
    // Idempotent: appendBinding returns the party unchanged when this exact
    // binding already exists.
    const cfg = await fetch(services.oidc + "/.well-known/openid-configuration").then((r) => r.json());
    const subject = await pairwise(cfg.issuer, sub);
    await call("parties", "POST", "/v1/parties/" + encodeURIComponent(partyId) + "/identity-bindings", {
      provider: "mock-oidc",
      providerClass: "generic-oidc",
      subjectRef: subject,
    });
  } catch (e) {
    token = prev;
    throw e;
  }
  return t;
}

// The real login (#155): hand the browser to CREST's server-side eSignet
// flow. The parties service redirects to eSignet's UI, exchanges the code in
// its callback, and bounces back to this door's #/auth route with the token.
export function startEsignetLogin(): void {
  const door = location.origin + location.pathname.replace(/\/$/, "");
  location.href = services.parties + "/v1/auth/login?door=" + encodeURIComponent(door);
}

// Who am I, as this deployment sees me: pairwise subjectRef, and the party
// bound to it (empty for an authenticated stranger — the enrol prompt).
export async function whoAmI(): Promise<{ subjectRef: string; partyId: string; issuer: string }> {
  return call("parties", "GET", "/v1/auth/me");
}

// The deployment's own pairwise derivation (HMAC-SHA256 under its salt) runs
// server-side; the browser cannot and must not know the salt. The dev issuer
// and stack share CREST_SUBJECT_SALT, and the mock issuer exposes the
// derivation for dev.
async function pairwise(_issuer: string, sub: string): Promise<string> {
  const r = await fetch(services.oidc + "/dev/pairwise?sub=" + encodeURIComponent(sub));
  if (!r.ok) throw new Error("the dev issuer cannot derive the pairwise subject; is mock-oidc current?");
  const d = await r.json();
  return d.subject;
}
