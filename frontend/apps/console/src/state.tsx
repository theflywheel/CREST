// Console session state, ported from apps/console/app.js: which persona is
// signed in, the claim being traced, the wizard step, the last error.
import { createContext, useContext, useState, type ReactNode } from "react";
import { ApiError, loginAs, setSession, FIX } from "@crest/api";

// Personas. The instance administrator reuses FIX.org: the fixture world
// seeds the instance-scoped grants on the organisation party, because in
// this deployment the programme organisation is also the deployment
// operator — a real deployment would separate them.
export const personas = [
  {
    key: "org",
    id: FIX.org,
    who: "Ministry of Health",
    role: "org admin · project",
    what: "PRJ-118 — the funnel, the payments, the definition, the money set up",
  },
  {
    key: "custodian",
    id: FIX.custodian,
    who: "Otieno",
    role: "registry custodian",
    what: "Duplicates, unattributed rows, recoveries, the review queue — and support",
  },
  {
    key: "instance",
    id: FIX.org,
    who: "Instance administrator",
    role: "instance view",
    what: "The deployment itself: services, issuer, consent floor, admission",
  },
] as const;

export type PersonaKey = (typeof personas)[number]["key"];

type State = {
  me: { partyId: string; who: string; role: string } | null;
  persona: PersonaKey | null;
  err: string | null;
  fail: (e: unknown) => void;
  clearErr: () => void;
  login: (idx: number) => Promise<PersonaKey>;
  logout: () => void;
  traceClaim: string;
  setTraceClaim: (c: string) => void;
  wizStep: number;
  setWizStep: (n: number) => void;
};

const Ctx = createContext<State>(null as unknown as State);
export const useConsole = () => useContext(Ctx);

export const errText = (e: unknown) =>
  e instanceof ApiError
    ? `${e.status} ${e.code || ""} — ${e.message}`
    : String((e as Error)?.message || e);

export function ConsoleProvider(props: { children: ReactNode }) {
  const [me, setMe] = useState<State["me"]>(null);
  const [persona, setPersona] = useState<PersonaKey | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [traceClaim, setTraceClaim] = useState("");
  const [wizStep, setWizStep] = useState(0);

  const login = async (idx: number) => {
    const p = personas[idx];
    await loginAs(p.id);
    setMe({ partyId: p.id, who: p.who, role: p.role });
    setPersona(p.key);
    return p.key;
  };
  const logout = () => {
    setSession(null);
    setMe(null);
    setPersona(null);
    setErr(null);
  };

  return (
    <Ctx.Provider
      value={{
        me,
        persona,
        err,
        fail: (e) => setErr(errText(e)),
        clearErr: () => setErr(null),
        login,
        logout,
        traceClaim,
        setTraceClaim,
        wizStep,
        setWizStep,
      }}
    >
      {props.children}
    </Ctx.Provider>
  );
}
