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
import { clearApplicantSessionStorage } from "./views/Onboard";

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
      // A restored session may hold an expired token (eSignet's are short).
      // Verify it before reading the world: a dead token means signed out,
      // not a page of invalid_token error bars.
      try {
        await whoAmI();
      } catch (e) {
        if (live && e instanceof ApiError && e.status === 401) logout();
        if (e instanceof ApiError && e.status === 401) return;
      }
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
      if (!list.length) {
        // Neither owner nor configurator: the projects this person holds a
        // grant on (an author's world is where somebody granted them a role).
        const mine = await api.get("parties", "/v1/authorizations/mine").catch(() => null);
        const ctxIds = Array.from(
          new Set(
            ((mine && mine.authorizations) || [])
              .map((a: { scope?: { contextId?: string }; contextId?: string }) => a.contextId || a.scope?.contextId)
              .filter(Boolean) as string[],
          ),
        );
        const fetched = await Promise.all(
          ctxIds.map((id) =>
            api.get("parties", "/v1/projects/" + encodeURIComponent(id)).then((p) => p.project || p).catch(() => null),
          ),
        );
        list = fetched.filter(Boolean) as ProjectRow[];
      }
      // A mechanism owner's projects are the contexts of the mechanisms they
      // own — the record their persona derives from names the project too.
      let ownedRates: string[] = [];
      if (!list.length) {
        const mechs = await api
          .get("payments", "/v1/mechanisms/mine")
          .then((r) => ((r && r.mechanisms) || []) as Array<{ contextId?: string }>)
          .catch(() => []);
        const ctxIds = Array.from(new Set(mechs.map((m) => m.contextId).filter(Boolean) as string[]));
        // The project read answers its owner and configurator; a mechanism
        // owner is neither, and the context id is still theirs to work in.
        const fetched = await Promise.all(
          ctxIds.map((id) =>
            api
              .get("parties", "/v1/projects/" + encodeURIComponent(id))
              .then((p) => (p.project || p) as ProjectRow)
              .catch(() => ({ id, name: id }) as ProjectRow),
          ),
        );
        list = fetched;
      }
      // A rate owner's definition is the one they were assigned, whatever
      // else is listed.
      ownedRates = await api
        .get("payments", "/v1/rate-ownerships/mine")
        .then((r) => ((r && r.definitionIds) || []) as string[])
        .catch(() => []);
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
          : ownedRates.find((id) => defs.some((d) => d.id === id)) ||
            ownedRates[0] ||
            defs.find((d) => d.state === "ACTIVE")?.id ||
            defs[0]?.id ||
            "",
      );
    })();
    return () => {
      live = false;
    };
  }, [me]);

  const login = async (idx: number) => {
    const p = personas[idx];
    const token = await loginAs(p.id);
    const identity = await whoAmI();
    const who = { partyId: identity.partyId, who: p.who, role: p.role };
    setPersonToken(token);
    setMe(who);
    setPersona(p.key);
    store({ token, me: who, persona: p.key });
    return p.key;
  };
  // The eSignet return leg: the callback handed the door a verified token;
  // ask the registry who that makes us.
  const completeEsignet = async (token: string) => {
    setSession(token);
    const w = await whoAmI();
    if (!w.partyId) {
      // A callback may be switching away from a previous role-derived
      // session. Clear that state before returning "stranger"; AuthReturn
      // can then deliberately preserve the verified token for applicant
      // onboarding without reviving the old persona.
      setSession(null);
      setPersonToken(null);
      setMe(null);
      setPersona(null);
      store(null);
      return "stranger" as const;
    }
    // The role is derived from the registry, not chosen in a browser. Three
    // real facts, in order: the instance's published self-description names
    // its operator; a person's own grants (/v1/authorizations/mine — the #68
    // self-read) name the functions they hold, and specify-definition /
    // ratify-definition are the P-3 roles; everyone else lands on the
    // organisation side.
    const inst = await api.get("parties", "/v1/instance").catch(() => null);
    const operator = Boolean(inst && inst.instance && inst.instance.operatorPartyId === w.partyId);
    let key: PersonaKey = "orgadmin";
    let role = "Org Admin";
    if (operator) {
      key = "instance";
      role = "Instance Operator";
    } else {
      const mine = await api.get("parties", "/v1/authorizations/mine").catch(() => null);
      const fns = new Set(
        ((mine && mine.authorizations) || []).flatMap((a: { functions?: string[] }) => a.functions || []),
      );
      if (fns.has("specify-definition")) {
        key = "author";
        role = "Work Definition Author";
      } else if (fns.has("ratify-definition")) {
        key = "approver";
        role = "Work Definition Approver";
      } else if (fns.has("resolve-unclear-evidence")) {
        // The registry custodian holds the one decision the system refuses
        // to make for itself — attributing a row nobody could match — and
        // that grant is the fact the persona is read from.
        key = "custodian";
        role = "Registry Custodian";
      } else if (
        await api
          .get("parties", `/v1/projects?configuratorPartyId=${encodeURIComponent(w.partyId)}`)
          .then((r) => ((r && r.projects) || []).length > 0)
          .catch(() => false)
      ) {
        // A configurator is named on a project by its owner (p1_3); the
        // project's own record is the fact, not a grant.
        key = "configurator";
        role = "Project Configurator";
      } else {
        // The funder roles are not party grants — a rate owner is named by a
        // rate-owner assignment, a mechanism owner by owning a mechanism. So
        // they are derived from the payments service's own ownership records,
        // the same shape as the P-3 grants above: a real fact about this
        // party, read back, not a persona chosen in the browser. Mechanism
        // ownership is checked first only because a party that somehow holds
        // both would most need the /mech surface; the ordering is not a
        // precedence claim about the roles.
        const owned = await api
          .get("payments", "/v1/mechanisms/mine")
          .then((r) => (r && r.mechanisms) || [])
          .catch(() => []);
        if (owned.length) {
          key = "payowner";
          role = "Payment Mechanism Owner";
        } else {
          const rates = await api
            .get("payments", "/v1/rate-ownerships/mine")
            .then((r) => (r && r.definitionIds) || [])
            .catch(() => []);
          if (rates.length) {
            key = "rateowner";
            role = "Rate Owner";
          }
        }
      }
    }
    // The reference shows the signed-in person's own name. That is the
    // party's displayName, read through the same self-read a person may
    // always make of their own record (GET /v1/parties/{id}) — the literal
    // stays only as the fallback an unreadable record leaves behind.
    const name = await api
      .get("parties", `/v1/parties/${encodeURIComponent(w.partyId)}`)
      .then((p) => (p && (p.displayName || (p.party && p.party.displayName))) || "Signed in via eSignet")
      .catch(() => "Signed in via eSignet");
    const who = {
      partyId: w.partyId,
      who: name,
      role,
    };
    setPersonToken(token);
    setMe(who);
    setPersona(key);
    store({ token, me: who, persona: key });
    return "enrolled" as const;
  };
  const assertSession = () => {
    if (personToken) setSession(personToken);
  };
  const logout = () => {
    setPersonToken(null);
    setSession(null);
    store(null);
    clearApplicantSessionStorage();
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
