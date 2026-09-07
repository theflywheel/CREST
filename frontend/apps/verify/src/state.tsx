// The verification surfaces' shared state (journeys V-1, V-2, P-10),
// ported 1:1 from apps/verify/app.js: the last verdict, the credential it
// was computed for, and whether the institutional session is held.
import { createContext, useContext, useState, type ReactNode } from "react";
import { api, ApiError, setSession, startEsignetLogin, whoAmI } from "@crest/api";
import { readIssuerTrust, refreshIssuerTrust, verifyOffline } from "./offline";

export type Verdict = {
  valid: boolean;
  tier?: number;
  revoked?: boolean;
  statusCheckedAt?: string;
  offline?: boolean;
  contested?: unknown[];
  reasons?: string[];
  trustChain?: Array<{ claim: string; checkable: boolean; how?: string; trusting?: string }>;
  notEstablished?: string[];
};

export type Credential = {
  id?: string;
  credentialSubject?: { workEvent?: WorkEvent };
};

export type WorkEvent = {
  activity?: string;
  outcome?: { value: string | number; unit: string };
  period?: { start?: string; end?: string };
  geography?: string;
  householdId?: string;
  beneficiaryCount?: number;
  supervisorPresent?: boolean;
  sourceRecordRef?: string;
  definition?: { id?: string; version?: string };
  definitionRef?: string;
  evidenceFields?: string[];
};

type State = {
  err: string | null;
  fail: (e: unknown) => void;
  clearErr: () => void;
  verdict: Verdict | null;
  credential: Credential | null;
  verifiedAs: string | null; // null = bare check; otherwise the actual caller
  runVerify: (credential: Credential, requestedByPartyId?: string, purpose?: string) => Promise<Verdict>;
  runOfflineVerify: (credential: Credential) => Promise<Verdict>;
  refreshOfflineTrust: () => Promise<void>;
  orgSession: boolean;
  orgParty: Record<string, string> | null;
  orgPartyErr: string | null;
  ensureOrg: () => Promise<void>;
  beginLogin: () => void;
  completeEsignet: (token: string) => Promise<"enrolled" | "stranger">;
  dropOrg: () => void;
};

const Ctx = createContext<State>(null as unknown as State);
export const useVerify = () => useContext(Ctx);

export const errText = (e: unknown) =>
  e instanceof ApiError
    ? `${e.status} ${e.code || ""} — ${e.message}`
    : String((e as Error)?.message || e);

export function VerifyProvider(props: { children: ReactNode }) {
  const [err, setErr] = useState<string | null>(null);
  const [verdict, setVerdict] = useState<Verdict | null>(null);
  const [credential, setCredential] = useState<Credential | null>(null);
  const [verifiedAs, setVerifiedAs] = useState<string | null>(null);
  const [orgSession, setOrgSession] = useState(false);
  const [orgParty, setOrgParty] = useState<Record<string, string> | null>(null);
  const [orgPartyErr, setOrgPartyErr] = useState<string | null>(null);

  const fail = (e: unknown) => setErr(errText(e));

  const runVerify = async (cred: Credential, requestedByPartyId?: string, purpose?: string) => {
    const v: Verdict = await api.post("verification", "/v1/verify", {
      credential: cred,
      requestedByPartyId: requestedByPartyId || undefined,
      purpose: purpose || undefined,
    });
    setVerdict(v);
    setCredential(cred);
    setVerifiedAs(requestedByPartyId || null);
    return v;
  };

  const runOfflineVerify = async (cred: Credential) => {
    const trust = readIssuerTrust();
    if (!trust) throw new Error("no fresh issuer keys are cached; refresh trusted issuer keys while online first");
    const result = await verifyOffline(cred as Record<string, unknown>, trust);
    const v: Verdict = {
      valid: result.valid,
      offline: true,
      reasons: result.reasons,
      notEstablished: result.notEstablished,
      trustChain: result.valid
        ? [{ claim: "credential signature", checkable: true, how: `verified locally with ${result.verificationMethod}` }]
        : undefined,
    };
    setVerdict(v);
    setCredential(cred);
    setVerifiedAs(null);
    return v;
  };

  const refreshOfflineTrust = async () => {
    await refreshIssuerTrust();
  };

  const ensureOrg = async () => {
    if (orgSession) return;
    // Logged out is a state V-2 renders (its sign-in button), not an error
    // to bar the screen with: only a session that exists and is refused is.
    let caller: { partyId?: string };
    try {
      caller = await whoAmI();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return;
      throw e;
    }
    if (!caller.partyId) return;
    try {
      setOrgParty(await api.get("parties", `/v1/parties/${encodeURIComponent(caller.partyId)}`));
      setOrgSession(true);
      setOrgPartyErr(null);
    } catch (e) {
      setOrgParty(null);
      setOrgPartyErr(e instanceof ApiError ? `${e.status} ${e.code || ""}` : errText(e));
    }
  };

  const completeEsignet = async (token: string) => {
    setSession(token);
    const caller = await whoAmI();
    if (!caller.partyId) {
      setSession(null);
      return "stranger" as const;
    }
    setOrgParty(await api.get("parties", `/v1/parties/${encodeURIComponent(caller.partyId)}`));
    setOrgPartyErr(null);
    setOrgSession(true);
    return "enrolled" as const;
  };

  // V-1 is deliberately logged out: the org session is dropped on leaving V-2.
  const dropOrg = () => {
    if (!orgSession) return;
    setSession(null);
    setOrgSession(false);
    setOrgParty(null);
    setOrgPartyErr(null);
  };

  return (
    <Ctx.Provider
      value={{
        err,
        fail,
        clearErr: () => setErr(null),
        verdict,
        credential,
        verifiedAs,
        runVerify,
        runOfflineVerify,
        refreshOfflineTrust,
        orgSession,
        orgParty,
        orgPartyErr,
        ensureOrg,
        beginLogin: startEsignetLogin,
        completeEsignet,
        dropOrg,
      }}
    >
      {props.children}
    </Ctx.Provider>
  );
}

export const short = (id: unknown) => {
  const s = String(id || "");
  return s.length > 22 ? s.slice(0, 12) + "…" + s.slice(-7) : s;
};
export const day = (ts?: string) =>
  ts
    ? new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })
    : "—";
