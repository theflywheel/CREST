// The verification surfaces' shared state (journeys V-1, V-2, P-10),
// ported 1:1 from apps/verify/app.js: the last verdict, the credential it
// was computed for, and whether the institutional session is held.
import { createContext, useContext, useState, type ReactNode } from "react";
import { api, ApiError, loginAs, setSession, FIX } from "@crest/api";

export type Verdict = {
  valid: boolean;
  tier?: number;
  revoked?: boolean;
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
  verifiedAs: string | null; // null = bare check; FIX.org = institutional
  runVerify: (credential: Credential, requestedByPartyId?: string, purpose?: string) => Promise<Verdict>;
  orgSession: boolean;
  orgParty: Record<string, string> | null;
  orgPartyErr: string | null;
  ensureOrg: () => Promise<void>;
  dropOrg: () => void;
};

const Ctx = createContext<State>(null as unknown as State);
export const useVerify = () => useContext(Ctx);

export const errText = (e: unknown) =>
  e instanceof ApiError
    ? `${e.status} ${e.code || ""} — ${e.message}`
    : String((e as Error)?.message || e);

// The same open chain-read a verifier gets; borrowing the newest credential
// is exactly what scanning the worker's printed card gives.
export async function loadSampleCredential(partyId?: string): Promise<Credential> {
  const out = await api.get(
    "verification",
    `/v1/parties/${encodeURIComponent(partyId || FIX.workerA)}/credentials`,
  );
  const c = (out.credentials || [])[0];
  if (!c) throw new Error("that person's chain holds no credentials yet");
  return c;
}

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

  const ensureOrg = async () => {
    if (orgSession) return;
    await loginAs(FIX.org);
    setOrgSession(true);
    try {
      setOrgParty(await api.get("parties", `/v1/parties/${encodeURIComponent(FIX.org)}`));
      setOrgPartyErr(null);
    } catch (e) {
      setOrgParty(null);
      setOrgPartyErr(e instanceof ApiError ? `${e.status} ${e.code || ""}` : errText(e));
    }
  };

  // V-1 is deliberately logged out: the org session is dropped on leaving V-2.
  const dropOrg = () => {
    if (!orgSession) return;
    setSession(null);
    setOrgSession(false);
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
        orgSession,
        orgParty,
        orgPartyErr,
        ensureOrg,
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
