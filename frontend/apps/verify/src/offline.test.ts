import test from "node:test";
import assert from "node:assert/strict";
import { CLOCK_SKEW_MS, MAX_CREDENTIAL_BYTES, TRUST_CACHE_MAX_AGE_MS, parseCredential, verifyOffline, type IssuerTrust } from "./offline";

const method = "did:crest:issuer:fixture#key-56475aa75463474c0285df5d";
const key = "z6MkehRgf7yJbgaGfYsdoAsKdBPE3dj2CYhowQdcjqSJgvVd";
const signed = {
  "@context": ["https://www.w3.org/ns/credentials/v2", "urn:crest:context:work-event-credential:1"],
  credentialSubject: { id: "fixture-subject" },
  id: "crest:credential:01JCREST00000000000000CRED",
  issuer: "did:crest:issuer:fixture",
  proof: {
    created: "2026-09-06T01:00:00Z",
    cryptosuite: "eddsa-jcs-2022",
    proofPurpose: "assertionMethod",
    proofValue: "z5ywmU7jAW2PrqyGX2sjvQpbNVEcbk9C9D3ik7LGhPGpQswKcjwQL6L12LwXUBCooAP2TuiJjmcj9XLfUASyU5Y85",
    type: "DataIntegrityProof",
    verificationMethod: method,
  },
  type: ["VerifiableCredential", "WorkEventCredential"],
  validFrom: "2026-09-06T00:00:00Z",
};

const trust: IssuerTrust = {
  issuer: "did:crest:issuer:fixture",
  verificationMethods: { [method]: key },
  fetchedAt: "2026-09-06T02:00:00Z",
};
const checkedAt = new Date("2026-09-06T02:01:00Z");

test("offline verification checks the real Ed25519/JCS signature", async () => {
  const result = await verifyOffline(signed, trust, checkedAt);
  assert.equal(result.valid, true);
  assert.deepEqual(result.reasons, []);
  assert.equal(result.notEstablished.length, 2);
});

test("offline verification rejects tampering and future proof times", async () => {
  const altered = structuredClone(signed);
  altered.credentialSubject.id = "another-subject";
  assert.equal((await verifyOffline(altered, trust, checkedAt)).valid, false);

  const future = structuredClone(signed);
  future.proof.created = new Date(checkedAt.getTime() + CLOCK_SKEW_MS + 1).toISOString();
  assert.equal((await verifyOffline(future, trust, checkedAt)).valid, false);
});

test("offline verification refuses an expired trust cache", async () => {
  const stale = { ...trust, fetchedAt: new Date(checkedAt.getTime() - TRUST_CACHE_MAX_AGE_MS - 1).toISOString() };
  const result = await verifyOffline(signed, stale, checkedAt);
  assert.equal(result.valid, false);
  assert.match(result.reasons[0], /trust cache is stale/);
});

test("offline verification requires assertion purpose and rejects expiry", async () => {
  const wrongPurpose = structuredClone(signed);
  wrongPurpose.proof.proofPurpose = "authentication";
  assert.equal((await verifyOffline(wrongPurpose, trust, checkedAt)).valid, false);

  const expired = structuredClone(signed);
  (expired as Record<string, unknown>).expirationDate = "2026-09-06T02:00:00Z";
  assert.equal((await verifyOffline(expired, trust, checkedAt)).valid, false);
});

test("credential parsing bounds size and unsafe integers", () => {
  assert.throws(() => parseCredential("{" + "\"x\":\"" + "a".repeat(MAX_CREDENTIAL_BYTES) + "\"}"), /too large/);
  assert.throws(() => parseCredential('{"x":9007199254740992}'), /unsafe number/);
});
