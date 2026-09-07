// The CREST services, as the browser reaches them — TS port of
// apps/shared/api.js, behaviour preserved verbatim.
//
// Login is the mock OIDC issuer in dev; a real deployment replaces
// startLogin with an eSignet redirect — the shape of everything after the
// token is identical, which is the point of #89.

import { isLocalStack, services, type ServiceName } from "./config";
import { FIX } from "./fixtures";

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
  idempotencyKey?: string,
): Promise<any> {
  const headers: Record<string, string> = {};
  if (token) headers["Authorization"] = "Bearer " + token;
  if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
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

// getText: for the endpoints whose body is not JSON — the reconciliation
// file (f2_5) is CSV by contract, and parsing it as JSON would silently
// return null where the caller needed the lines.
async function callText(service: ServiceName, path: string): Promise<{ text: string; format: string | null }> {
  const headers: Record<string, string> = {};
  if (token) headers["Authorization"] = "Bearer " + token;
  if (onBehalfOf) headers["X-CREST-On-Behalf-Of"] = onBehalfOf;
  const res = await fetch(services[service] + path, { headers });
  const text = await res.text();
  if (!res.ok) {
    let doc: any = null;
    try { doc = JSON.parse(text); } catch { /* not JSON */ }
    throw new ApiError(res.status, doc && doc.code, (doc && doc.message) || text);
  }
  return { text, format: res.headers.get("X-CREST-Reconciliation-Format") };
}

export const api = {
  get: (svc: ServiceName, path: string) => call(svc, "GET", path),
  getText: (svc: ServiceName, path: string) => callText(svc, path),
  post: (svc: ServiceName, path: string, body?: unknown, key?: string) => call(svc, "POST", path, body, undefined, key),
  // The J3 configuration endpoints are PUTs by design: one record per key,
  // idempotent, re-answering replaces the answer rather than appending one.
  put: (svc: ServiceName, path: string, body?: unknown) => call(svc, "PUT", path, body),
  postRaw: (svc: ServiceName, path: string, body: unknown, ct: string, key?: string) => call(svc, "POST", path, body, ct, key),
  del: (svc: ServiceName, path: string) => call(svc, "DELETE", path),
};

// Development identities must already be enrolled. A claimed party ID is
// never a binding credential; invitations use the ordinary claim flow.
export async function loginAs(partyId: string): Promise<string> {
  if (!isLocalStack) {
    throw new Error("development loginAs is available only on the local stack; use the configured OIDC login");
  }
  const sub = partyId === FIX.specifier ? "seed|specifier" : "story|" + partyId.replace("did:crest:party:", "");
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
    const who = await whoAmI();
    if (!who.partyId) throw new Error("This development identity is not enrolled. Claim its invitation first.");
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

// Claiming an invitation (#123): the caller is signed in and holds no party
// yet; the code binds THIS login to the record somebody already created. The
// session token set by completeEsignet carries the request — the claim is the
// claimant's own act, never an assertion made for them.
//
// provider/providerClass are derived from the issuer this deployment actually
// authenticated against, not from a guess: an eSignet issuer is recorded as
// eSignet, anything else as the generic OIDC provider the dev stack runs.
export async function claimInvitation(code: string): Promise<{
  partyId: string;
  identityAssurance?: string;
  because?: unknown;
}> {
  const who = await whoAmI();
  const esignet = (who.issuer || "").toLowerCase().includes("esignet");
  return call("parties", "POST", "/v1/party-invitations/claim", {
    code: code.trim(),
    provider: esignet ? "esignet" : "mock-oidc",
    providerClass: esignet ? "esignet" : "generic-oidc",
  });
}
