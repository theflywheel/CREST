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
import { ApiError, api, loginAs, setSession, whoAmI, FIX } from "@crest/api";

export const personas = [
  {
    key: "orgadmin",
    id: FIX.org,
    who: "Peter Otieno",
    role: "Org Admin",
    ref: "P-1",
    what: "The organisation's standing record — terms held, authorizations, status",
  },
  {
    key: "configurator",
    id: FIX.org,
    who: "Dr. Alice Mutua",
    role: "Project Configurator",
    ref: "P-2",
    what: "PRJ-118 operations — the funnel, payments, trace, sources, reports",
  },
  {
    key: "author",
    // The specifier party, not the organisation. The author drafts and the
    // approver signs, and the definitions service refuses a version whose
    // ratifier is its author — so if both personas signed in as the same
    // party, every ratification this console produced would be refused and
    // the separation would be a claim no test could make. The fixture world
    // already records this party as the seeded definition's author.
    id: FIX.specifier,
    who: "Amina Yusuf",
    role: "Work Definition Author",
    ref: "P-3",
    what: "The definition, section by section — drafting is the author's alone",
  },
  {
    key: "approver",
    id: FIX.org,
    who: "Prof. Ndegwa",
    role: "Work Definition Approver",
    ref: "P-3",
    what: "Ratifies what the author drafted — reads everything, drafts nothing",
  },
  {
    key: "rateowner",
    id: FIX.org,
    who: "Nadia Okoth",
    role: "Rate Owner",
    ref: "F-1",
    what: "The rate on the unit the author defined",
  },
  {
    key: "payowner",
    id: FIX.org,
    who: "Daniel Mwangi",
    role: "Payment Mechanism Owner",
    ref: "F-2",
    what: "The rail beyond the boundary where CREST stops",
  },
  {
    key: "instance",
    id: FIX.org,
    who: "Instance administrator",
    role: "Instance Admin",
    ref: "G-1",
    what: "The deployment itself: services, issuer, consent floor, admission",
  },
  {
    key: "custodian",
    id: FIX.custodian,
    who: "Otieno",
    role: "Registry Custodian",
    ref: "G-4",
    what: "Duplicates, unattributed rows, recoveries, the review queue",
  },
  {
    key: "support",
    id: FIX.custodian,
    who: "Naliaka",
    role: "Support Agent",
    ref: "W-3",
    what: "The queue of things that stalled — lookup, payment trace",
  },
  {
    key: "funder",
    id: FIX.org,
    who: "Funding oversight",
    role: "Funding Viewer",
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
  // The eSignet return leg: the callback handed the door a verified token;
  // ask the registry who that makes us. Honest limit: this instance has no
  // per-party role-permit read yet (that self-read is L1 work still on its
  // own branch), so a real sign-in that is not the instance operator lands
  // on the Org Admin view rather than a registry-derived one — the same gap
  // the demo personas' header comment already names.
  completeEsignet: (token: string) => Promise<"enrolled" | "stranger">;
  logout: () => void;
  traceClaim: string;
  setTraceClaim: (c: string) => void;
  wizStep: number;
  setWizStep: (n: number) => void;
  // Which project this console is working on. Chosen on n2 from the projects
  // the registry says you hold a role in — never a project id typed into a
  // browser. The seeded programme project is the default so a fresh session
  // has somewhere to land.
  projectId: string;
  setProjectId: (id: string) => void;
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
  const [projectId, setProjectId] = useState<string>(FIX.project);

  const login = async (idx: number) => {
    const p = personas[idx];
    await loginAs(p.id);
    setMe({ partyId: p.id, who: p.who, role: p.role });
    setPersona(p.key);
    return p.key;
  };
  // See the completeEsignet type above for the honest limit this leans on.
  const completeEsignet = async (token: string): Promise<"enrolled" | "stranger"> => {
    setSession(token);
    const w = await whoAmI();
    if (!w.partyId) {
      setSession(null);
      return "stranger";
    }
    const inst = await api.get("parties", "/v1/instance").catch(() => null);
    const operator = Boolean(inst && inst.instance && inst.instance.operatorPartyId === w.partyId);
    const key: PersonaKey = operator ? "instance" : "orgadmin";
    const role = operator ? "Instance Admin" : "Org Admin";
    // The reference shows the signed-in person's own name. That is the
    // party's displayName, read through the same self-read a person may
    // always make of their own record (GET /v1/parties/{id}) — the literal
    // stays only as the fallback an unreadable record leaves behind.
    const who = await api
      .get("parties", `/v1/parties/${encodeURIComponent(w.partyId)}`)
      .then((p) => (p && p.displayName) || "Signed in via eSignet")
      .catch(() => "Signed in via eSignet");
    setMe({ partyId: w.partyId, who, role });
    setPersona(key);
    return "enrolled";
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
        completeEsignet,
        logout,
        traceClaim,
        setTraceClaim,
        wizStep,
        setWizStep,
        projectId,
        setProjectId,
      }}
    >
      {props.children}
    </Ctx.Provider>
  );
}
