// Console session state: which persona is signed in, the claim being traced,
// the wizard step, the last error.
//
// Personas are role-derived (docs/JOURNEY_GAP_ASSESSMENT.md finding 1): each
// selectable session maps to one reference role flow, and its navigation shows
// only that flow's views. Author/approver separation is expressed here — the
// Definition Approver ratifies and can never open the authoring wizard.
//
// Honest limit, named: the separation is a session/navigation contract in the
// UI. The fixture world (harness/seed.go) seeds backend grants on only two
// parties (the programme organisation and the custodian), so every org-side
// role signs in as the organisation party and every registry/support role as
// the custodian party. Per-role backend permits are L1 work the traceability
// manifest records as missing (p1_2 role assignment).
import { createContext, useContext, useState, type ReactNode } from "react";
import { ApiError, loginAs, setSession, FIX } from "@crest/api";

export const personas = [
  {
    key: "orgadmin",
    id: FIX.org,
    who: "Adhiambo",
    role: "org admin",
    ref: "P-1",
    what: "The organisation's standing record — terms held, authorizations, status",
  },
  {
    key: "configurator",
    id: FIX.org,
    who: "Wanjiru",
    role: "project configurator",
    ref: "P-2",
    what: "PRJ-118 operations — the funnel, payments, trace, sources, reports",
  },
  {
    key: "author",
    id: FIX.org,
    who: "Dr. Achieng",
    role: "work definition author",
    ref: "P-3",
    what: "The definition, section by section — drafting is the author's alone",
  },
  {
    key: "approver",
    id: FIX.org,
    who: "Prof. Ndegwa",
    role: "work definition approver",
    ref: "P-3",
    what: "Ratifies what the author drafted — reads everything, drafts nothing",
  },
  {
    key: "rateowner",
    id: FIX.org,
    who: "Mutua",
    role: "rate owner",
    ref: "F-1",
    what: "The rate on the unit the author defined",
  },
  {
    key: "payowner",
    id: FIX.org,
    who: "Njeri",
    role: "payment mechanism owner",
    ref: "F-2",
    what: "The rail beyond the boundary where CREST stops",
  },
  {
    key: "instance",
    id: FIX.org,
    who: "Instance administrator",
    role: "instance admin",
    ref: "G-1",
    what: "The deployment itself: services, issuer, consent floor, admission",
  },
  {
    key: "custodian",
    id: FIX.custodian,
    who: "Otieno",
    role: "registry custodian",
    ref: "G-4",
    what: "Duplicates, unattributed rows, recoveries, the review queue",
  },
  {
    key: "support",
    id: FIX.custodian,
    who: "Naliaka",
    role: "support agent",
    ref: "W-3",
    what: "The queue of things that stalled — lookup, payment trace",
  },
  {
    key: "funder",
    id: FIX.org,
    who: "Funding oversight",
    role: "funding viewer",
    ref: "V-4",
    what: "Allocated against paid, and the trail from an amount downward",
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
