import { api } from "@crest/api";

// Offline trust is a convenience cache, never a credential supplied trust
// root. It is populated only from this deployment's configured verification
// service and expires so a verifier is forced to refresh after key changes.
export const TRUST_CACHE_MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000;
export const CLOCK_SKEW_MS = 5 * 60 * 1000;
export const MAX_CREDENTIAL_BYTES = 1 << 20;

export type IssuerTrust = {
  issuer: string;
  verificationMethods: Record<string, string>;
  fetchedAt: string;
};

export type OfflineResult = {
  valid: boolean;
  issuer: string;
  verificationMethod: string;
  trustFetchedAt: string;
  checkedAt: string;
  reasons: string[];
  notEstablished: string[];
};

const cacheKey = () => `crest:verify:issuer-trust:${globalThis.location?.origin || "unknown"}`;

function trustFromResponse(raw: unknown, fetchedAt = new Date()): IssuerTrust {
  if (!raw || typeof raw !== "object") throw new Error("issuer trust response is not an object");
  const doc = raw as Record<string, unknown>;
  const issuer = typeof doc.issuer === "string" ? doc.issuer.trim() : "";
  if (!issuer) throw new Error("issuer trust response has no issuer");
  const methods = doc.verificationMethods;
  if (!Array.isArray(methods) || methods.length === 0) {
    throw new Error("issuer trust response has no verification methods");
  }
  const verificationMethods: Record<string, string> = {};
  for (const item of methods) {
    if (!item || typeof item !== "object") throw new Error("issuer trust contains an invalid verification method");
    const method = (item as Record<string, unknown>).verificationMethod;
    const key = (item as Record<string, unknown>).publicKeyMultibase;
    if (typeof method !== "string" || !method.startsWith(issuer + "#") || typeof key !== "string") {
      throw new Error("issuer trust contains an invalid verification method");
    }
    decodePublicKey(key);
    verificationMethods[method] = key;
  }
  return { issuer, verificationMethods, fetchedAt: fetchedAt.toISOString() };
}

export function parseCredential(raw: string): Record<string, unknown> {
  if (new TextEncoder().encode(raw).byteLength > MAX_CREDENTIAL_BYTES) {
    throw new Error("credential is too large to verify safely offline");
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("credential is not valid JSON");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("credential must be a JSON object");
  }
  assertSafeValues(parsed);
  return parsed as Record<string, unknown>;
}

function assertSafeValues(value: unknown): void {
  if (typeof value === "number") {
    if (!Number.isFinite(value) || (Number.isInteger(value) && !Number.isSafeInteger(value))) {
      throw new Error("credential contains an unsafe number");
    }
    return;
  }
  if (Array.isArray(value)) {
    value.forEach(assertSafeValues);
    return;
  }
  if (value && typeof value === "object") {
    Object.values(value).forEach(assertSafeValues);
  }
}

export function readIssuerTrust(now = new Date(), maxAgeMs = TRUST_CACHE_MAX_AGE_MS): IssuerTrust | null {
  try {
    const raw = localStorage.getItem(cacheKey());
    if (!raw) return null;
    const trust = JSON.parse(raw) as IssuerTrust;
    if (!trust || typeof trust.issuer !== "string" || typeof trust.fetchedAt !== "string") return null;
    if (!Object.keys(trust.verificationMethods || {}).length) return null;
    const fetched = Date.parse(trust.fetchedAt);
    if (!Number.isFinite(fetched) || fetched > now.getTime() + CLOCK_SKEW_MS || now.getTime() - fetched > maxAgeMs) {
      return null;
    }
    for (const [method, key] of Object.entries(trust.verificationMethods)) {
      if (!method.startsWith(trust.issuer + "#")) return null;
      decodePublicKey(key);
    }
    return trust;
  } catch {
    return null;
  }
}

export async function refreshIssuerTrust(): Promise<IssuerTrust> {
  const trust = trustFromResponse(await api.get("verification", "/v1/issuer"));
  try {
    localStorage.setItem(cacheKey(), JSON.stringify(trust));
  } catch {
    // Private browsing and storage quotas must not turn a valid online check
    // into a failure; the caller still receives the freshly fetched trust.
  }
  return trust;
}

function base58Decode(input: string): Uint8Array {
  const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";
  if (!input) return new Uint8Array();
  const bytes: number[] = [];
  for (const char of input) {
    const value = alphabet.indexOf(char);
    if (value < 0) throw new Error("invalid base58 key");
    let carry = value;
    for (let i = 0; i < bytes.length; i++) {
      carry += bytes[i] * 58;
      bytes[i] = carry & 0xff;
      carry >>= 8;
    }
    while (carry) {
      bytes.push(carry & 0xff);
      carry >>= 8;
    }
  }
  for (let i = 0; i < input.length && input[i] === "1"; i++) bytes.push(0);
  return new Uint8Array(bytes.reverse());
}

function decodePublicKey(multibase: string): Uint8Array {
  if (!multibase.startsWith("z")) throw new Error("issuer key is not base58btc multibase");
  const raw = base58Decode(multibase.slice(1));
  if (raw.length !== 34 || raw[0] !== 0xed || raw[1] !== 0x01) {
    throw new Error("issuer key is not a multicodec Ed25519 public key");
  }
  return raw.slice(2);
}

function canonical(value: unknown): string {
  if (value === null) return "null";
  if (typeof value === "string") return JSON.stringify(value);
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new Error("credential contains a non-finite number");
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return "[" + value.map(canonical).join(",") + "]";
  if (typeof value === "object") {
    const obj = value as Record<string, unknown>;
    return "{" + Object.keys(obj).sort().map((key) => JSON.stringify(key) + ":" + canonical(obj[key])).join(",") + "}";
  }
  throw new Error("credential contains an unsupported JSON value");
}

async function sha256(value: string): Promise<Uint8Array> {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value)));
}

function signingBytes(doc: Record<string, unknown>, proof: Record<string, unknown>): Promise<Uint8Array> {
  const unsigned = { ...doc };
  delete unsigned.proof;
  const options: Record<string, unknown> = { ...proof };
  delete options.proofValue;
  if (Object.prototype.hasOwnProperty.call(doc, "@context")) {
    Object.assign(options, { "@context": doc["@context"] });
  }
  return Promise.all([sha256(canonical(options)), sha256(canonical(unsigned))]).then(([opts, body]) => {
    const input = new Uint8Array(opts.length + body.length);
    input.set(opts);
    input.set(body, opts.length);
    return input;
  });
}

function proofSignature(value: unknown): Uint8Array {
  if (typeof value !== "string" || !value.startsWith("z")) throw new Error("proof is not a base58btc signature");
  const signature = base58Decode(value.slice(1));
  if (signature.length !== 64) throw new Error("proof signature has the wrong length");
  return signature;
}

export async function verifyOffline(
  credential: Record<string, unknown>,
  trust: IssuerTrust,
  now = new Date(),
): Promise<OfflineResult> {
  const reasons: string[] = [];
  const notEstablished = [
    "current withdrawal status requires a fresh signed status list",
    "definition and source assessments are not resolved offline",
  ];
  const trustFetched = Date.parse(trust.fetchedAt);
  if (!Number.isFinite(trustFetched) || trustFetched > now.getTime() + CLOCK_SKEW_MS || now.getTime() - trustFetched > TRUST_CACHE_MAX_AGE_MS) {
    reasons.push("the issuer trust cache is stale; refresh it while online");
  }
  const proof = credential.proof;
  const issuer = typeof credential.issuer === "string" ? credential.issuer : "";
  const typedProof = proof && typeof proof === "object" ? proof as Record<string, unknown> : null;
  const method = typedProof && typeof typedProof.verificationMethod === "string" ? typedProof.verificationMethod : "";
  const checkedAt = now.toISOString();
  if (!typedProof || typedProof.type !== "DataIntegrityProof" || typedProof.cryptosuite !== "eddsa-jcs-2022") {
    reasons.push("unsupported or missing Data Integrity proof");
  }
  if (!typedProof || typedProof.proofPurpose !== "assertionMethod") {
    reasons.push("proof purpose is not assertionMethod");
  }
  if (issuer !== trust.issuer) reasons.push("credential issuer is not the configured trusted issuer");
  const publicKey = method ? trust.verificationMethods[method] : undefined;
  if (!publicKey) reasons.push("credential verification method is not in the configured trust cache");
  const created = parseTimestamp(typedProof?.created);
  const validFrom = parseTimestamp(credential.validFrom);
  if (!Number.isFinite(created)) reasons.push("proof creation time is missing or invalid");
  if (!Number.isFinite(validFrom)) reasons.push("credential validFrom is missing or invalid");
  if (Number.isFinite(created) && created > now.getTime() + CLOCK_SKEW_MS) reasons.push("proof creation time is in the future");
  if (Number.isFinite(validFrom) && validFrom > now.getTime() + CLOCK_SKEW_MS) reasons.push("credential validFrom is in the future");
  for (const field of ["expirationDate", "validUntil"]) {
    if (!(field in credential)) continue;
    const expires = parseTimestamp(credential[field]);
    if (!Number.isFinite(expires)) reasons.push(`${field} is invalid`);
    else if (expires <= now.getTime()) reasons.push(`${field} has expired`);
  }

  if (reasons.length === 0) {
    try {
      const input = await signingBytes(credential, typedProof!);
      const key = await crypto.subtle.importKey("raw", decodePublicKey(publicKey!), { name: "Ed25519" } as Algorithm, false, ["verify"]);
      const valid = await crypto.subtle.verify({ name: "Ed25519" } as Algorithm, key, proofSignature(typedProof!.proofValue), input);
      if (!valid) reasons.push("credential signature does not verify");
    } catch (err) {
      reasons.push(err instanceof Error ? err.message : "credential signature could not be checked");
    }
  }
  return { valid: reasons.length === 0, issuer, verificationMethod: method, trustFetchedAt: trust.fetchedAt, checkedAt, reasons, notEstablished };
}

function parseTimestamp(value: unknown): number {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) return NaN;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : NaN;
}
