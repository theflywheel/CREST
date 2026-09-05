// Session + flash state. The flash carries the "What happens next" block a
// terminal action leaves behind, keyed to the route that shows it — the
// journeys' rule that no action ends on a spinner or a bare toast.
//
// The session survives a reload via sessionStorage: with the real eSignet
// login (#155) a page refresh must not cost a whole redirect round-trip.
import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react";
import { api, ApiError, loginAs, setSession, whoAmI } from "@crest/api";

type Flash = { route: string; node: ReactNode } | null;

type Session = {
  me: string | null;
  meLabel: string | null;
  err: string | null;
  flash: Flash;
  login: (partyId: string, label?: string) => Promise<void>;
  completeEsignet: (token: string) => Promise<"enrolled" | "stranger">;
  signUp: (
    name: string,
    phone: string,
    consentPurpose: string,
    contextId?: string | null,
  ) => Promise<{ partyId: string; consentRecorded: boolean }>;
  // The programme a worker was pointed at — a context reference from a link
  // or read off a card, held only for this session.
  programme: string | null;
  setProgramme: (contextId: string | null) => void;
  logout: () => void;
  setFlash: (f: Flash) => void;
  fail: (e: unknown) => void;
  clearErr: () => void;
};

const Ctx = createContext<Session>(null as never);
export const useSession = () => useContext(Ctx);

export function describeError(e: unknown): string {
  return e instanceof ApiError
    ? `${e.status} ${e.code || ""} — ${e.message}`
    : String((e as any)?.message || e);
}

const SKEY = "crest.worker.session";
// The programme reference a worker arrived with — from #/join/<contextId> or
// typed off whatever their project gave them. Session-scoped: it is a
// pointer, not a membership.
const PKEY = "crest.worker.programme";

export function readProgramme(): string | null {
  try {
    return sessionStorage.getItem(PKEY);
  } catch {
    return null;
  }
}
function writeProgramme(v: string | null) {
  try {
    v ? sessionStorage.setItem(PKEY, v) : sessionStorage.removeItem(PKEY);
  } catch {
    /* a blocked sessionStorage only costs the reload convenience */
  }
}

function readStored(): { token: string; me: string; label?: string } | null {
  try {
    const raw = sessionStorage.getItem(SKEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}
function store(v: { token: string; me: string; label?: string } | null) {
  try {
    v ? sessionStorage.setItem(SKEY, JSON.stringify(v)) : sessionStorage.removeItem(SKEY);
  } catch {
    /* a blocked sessionStorage only costs the reload convenience */
  }
}

export function SessionProvider(props: { children: ReactNode }) {
  const stored = readStored();
  if (stored) setSession(stored.token);
  const [me, setMe] = useState<string | null>(stored ? stored.me : null);
  const [meLabel, setMeLabel] = useState<string | null>(stored ? stored.label || null : null);
  const [err, setErr] = useState<string | null>(null);
  const [flash, setFlash] = useState<Flash>(null);
  const pendingToken = useRef<string>("");
  const [programme, setProgrammeState] = useState<string | null>(readProgramme());
  const setProgramme = useCallback((v: string | null) => {
    writeProgramme(v);
    setProgrammeState(v);
  }, []);

  // The dev login: mock-OIDC minting a token for a named party on the local
  // stack. It takes the party id explicitly — there is no default person,
  // because a door with a default person is a door with a fixture in it.
  // A real deployment logs in through eSignet only.
  const login = useCallback(async (who: string, label?: string) => {
    const token = await loginAs(who);
    store({ token, me: who, label });
    setMe(who);
    setMeLabel(label || null);
    setErr(null);
  }, []);

  // The eSignet callback handed us a verified access token; ask the registry
  // who that makes us here. A stranger stays authenticated but unenrolled.
  const completeEsignet = useCallback(async (token: string) => {
    setSession(token);
    const who = await whoAmI();
    if (who.partyId) {
      store({ token, me: who.partyId });
      setMe(who.partyId);
      setErr(null);
      return "enrolled" as const;
    }
    // Held for the signup step: the stranger's proof of who they are IS this
    // token, and the bind below rides it.
    pendingToken.current = token;
    return "stranger" as const;
  }, []);

  // Self-registration (#155 phase B): the authenticated stranger creates
  // their own party and binds it to the identity they are holding right now.
  // The bind's proof is possession of the token whose subject is bound — the
  // one proof that needs no prior enrolment (#102). The pairwise subjectRef
  // comes from /v1/auth/me; the browser never learns how it is derived.
  // The enrollment-consent step (reference w1_5) happens BEFORE this is
  // called: the signup screen refuses to submit until consent is given, so a
  // party is never created without it. The consent is then recorded through
  // the real consents API immediately after the party exists — it cannot be
  // recorded earlier because the consent row needs a party to hang on
  // (traceability w1_5 records this ordering honestly).
  const signUp = useCallback(
    async (name: string, phone: string, consentPurpose: string, contextId?: string | null) => {
    const joining = contextId === undefined ? programme : contextId;
    const who = await whoAmI();
    const party = await api.post("parties", "/v1/parties", {
      displayName: name,
      kind: "person",
      contactRoutes: [{ kind: "phone", value: phone }],
    });
    // No assertedAt: the registry's clock is the authority for when a
    // binding was asserted, not this browser's.
    await api.post("parties", `/v1/parties/${party.id}/identity-bindings`, {
      provider: "esignet",
      providerClass: "esignet",
      subjectRef: who.subjectRef,
    });
    // Enrolment consent is scoped to a programme context (§9), and this
    // deployment tells a stranger nothing about which programmes exist — an
    // unnarrowed listing would answer who-works-where to anybody who asks.
    // So the consent is recorded only against a programme the worker was
    // actually pointed at (a join link, or a reference they were given). With
    // none, the record is created without a consent row and the screen says
    // so, rather than a programme being invented to make the call succeed.
    let consentRecorded = false;
    if (joining) {
      const q = new URLSearchParams({
        moment: "enrolment",
        purpose: consentPurpose,
        captureMethod: "screen",
        contextId: joining,
      });
      await api.post("parties", `/v1/parties/${party.id}/consents?${q}`);
      consentRecorded = true;
    }
    store({ token: pendingToken.current, me: party.id });
    setMe(party.id);
    setErr(null);
    return { partyId: party.id as string, consentRecorded };
    },
    [programme],
  );

  const logout = useCallback(() => {
    setSession(null);
    store(null);
    setMe(null);
    setMeLabel(null);
    setFlash(null);
    setErr(null);
  }, []);
  const fail = useCallback((e: unknown) => setErr(describeError(e)), []);
  const clearErr = useCallback(() => setErr(null), []);

  const value = useMemo(
    () => ({ me, meLabel, err, flash, login, completeEsignet, signUp, programme, setProgramme, logout, setFlash, fail, clearErr }),
    [me, meLabel, err, flash, login, completeEsignet, signUp, programme, setProgramme, logout, fail, clearErr],
  );
  return <Ctx.Provider value={value}>{props.children}</Ctx.Provider>;
}
