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
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
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
  completeEsignet: (token: string) => Promise<"enrolled" | "stranger">;
  logout: () => void;
  // Re-asserts the signed-in person's token on the api session (see provider).
  assertSession: () => void;
  traceClaim: string;
  setTraceClaim: (c: string) => void;
  wizStep: number;
  setWizStep: (n: number) => void;
  // Which project this console is working on. Loaded from the registry's own
  // answer (GET /v1/projects, narrowed to the signed-in party) — never a
  // project id typed into a browser, and never a fixture id: on a clean slate
  // the honest value is "", and every view shows its empty state.
  projects: ProjectRow[];
  projectId: string;
  setProjectId: (id: string) => void;
  // Which work definition the console reads. From GET /v1/definitions — the
  // newest ACTIVE one by default; "" while the deployment has defined nothing.
  definitions: DefRow[];
  definitionId: string;
  setDefinitionId: (id: string) => void;
};

export type ProjectRow = { id: string; name?: string; kind?: string; state?: string; ownerPartyId?: string };
export type DefRow = {
  id: string;
  version: number;
  state: string;
  outcomeUnit?: string;
  activity?: { code?: string; label?: string };
};

const Ctx = createContext<State>(null as unknown as State);
export const useConsole = () => useContext(Ctx);

export const errText = (e: unknown) =>
  e instanceof ApiError
    ? `${e.status} ${e.code || ""} — ${e.message}`
    : String((e as Error)?.message || e);

// The session survives a reload via sessionStorage — with eSignet as the only
// door, a refresh must not cost a whole redirect round-trip. The e2e suite
// writes this same key to sign in programmatically (the sign-in screen offers
// no personas any more).
const SKEY = "crest.console.session";
type Stored = { token: string; me: NonNullable<State["me"]>; persona: PersonaKey };
function readStored(): Stored | null {
  try {
    const raw = sessionStorage.getItem(SKEY);
    return raw ? (JSON.parse(raw) as Stored) : null;
  } catch {
    return null;
  }
}
function store(v: Stored | null) {
  try {
    v ? sessionStorage.setItem(SKEY, JSON.stringify(v)) : sessionStorage.removeItem(SKEY);
  } catch {
    /* a blocked sessionStorage only costs the reload convenience */
  }
}

export function ConsoleProvider(props: { children: ReactNode }) {
  // Restored ONCE. Re-asserting the stored token on every render fought the
  // onboarding screens, which deliberately swap the session to the
  // organisation's own token (ensureOrgSession) — a provider re-render midway
  // put the person's token back and the org's reads failed party_not_proven.
  const [stored] = useState(() => {
    const v = readStored();
    if (v) setSession(v.token);
    return v;
  });
  // The person's own token, kept so the shell can re-assert it. The
  // onboarding screens swap the api session to the ORGANISATION's token
  // (ensureOrgSession) on purpose; walking back into the console must put
  // the person back, or every view asks about a party the wire no longer
  // authenticates as (party_not_proven).
  const [personToken, setPersonToken] = useState<string | null>(stored ? stored.token : null);
  const [me, setMe] = useState<State["me"]>(stored ? stored.me : null);
  const [persona, setPersona] = useState<PersonaKey | null>(stored ? stored.persona : null);
  const [err, setErr] = useState<string | null>(null);
  const [traceClaim, setTraceClaim] = useState("");
  const [wizStep, setWizStep] = useState(0);
  const [projects, setProjects] = useState<ProjectRow[]>([]);
  const [projectId, setProjectId] = useState<string>("");
  const [definitions, setDefinitions] = useState<DefRow[]>([]);
  const [definitionId, setDefinitionId] = useState<string>("");

  // The world this session can see, from the services' own answers. Loaded on
  // sign-in (and reload) rather than hardcoded: the fixture world passes
  // through the same two queries, so the seeded console reads identically.
  useEffect(() => {
    if (!me) {
      setProjects([]);
      setProjectId("");
      setDefinitions([]);
      setDefinitionId("");
      return;
    }
    let live = true;
    (async () => {
      const owned = await api
        .get("parties", "/v1/projects?ownerPartyId=" + encodeURIComponent(me.partyId))
        .catch(() => null);
      let list: ProjectRow[] = (owned && owned.projects) || [];
      if (!list.length) {
        const configured = await api
          .get("parties", "/v1/projects?configuratorPartyId=" + encodeURIComponent(me.partyId))
          .catch(() => null);
        list = (configured && configured.projects) || [];
      }
      if (!live) return;
      setProjects(list);
      setProjectId((cur) => (cur && list.some((p) => p.id === cur) ? cur : list[0]?.id || ""));

      const listed = await api.get("definitions", "/v1/definitions?limit=100").catch(() => null);
      if (!live) return;
      const defs: DefRow[] = (listed && listed.definitions) || [];
      setDefinitions(defs);
      setDefinitionId((cur) =>
        cur && defs.some((d) => d.id === cur)
          ? cur
          : defs.find((d) => d.state === "ACTIVE")?.id || defs[0]?.id || "",
      );
    })();
    return () => {
      live = false;
    };
  }, [me]);

  const login = async (idx: number) => {
    const p = personas[idx];
    const token = await loginAs(p.id);
    const who = { partyId: p.id, who: p.who, role: p.role };
    setPersonToken(token);
    setMe(who);
    setPersona(p.key);
    store({ token, me: who, persona: p.key });
    return p.key;
  };
  // The eSignet return leg: the callback handed the door a verified token;
  // ask the registry who that makes us. Honest limit, restated from the
  // header: per-party role permits are L1 work the traceability manifest
  // records as missing (p1_2 role assignment), so a real sign-in lands on the
  // Org Admin view rather than a registry-derived one.
  const completeEsignet = async (token: string) => {
    setSession(token);
    const w = await whoAmI();
    if (!w.partyId) {
      setSession(null);
      return "stranger" as const;
    }
    const who = { partyId: w.partyId, who: "Signed in via eSignet", role: "Org Admin" };
    setPersonToken(token);
    setMe(who);
    setPersona("orgadmin");
    store({ token, me: who, persona: "orgadmin" });
    return "enrolled" as const;
  };
  const assertSession = () => {
    if (personToken) setSession(personToken);
  };
  const logout = () => {
    setPersonToken(null);
    setSession(null);
    store(null);
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
        assertSession,
        traceClaim,
        setTraceClaim,
        wizStep,
        setWizStep,
        projects,
        projectId,
        setProjectId,
        definitions,
        definitionId,
        setDefinitionId,
      }}
    >
      {props.children}
    </Ctx.Provider>
  );
}
