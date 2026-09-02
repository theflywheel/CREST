// Session + flash state. The flash carries the "What happens next" block a
// terminal action leaves behind, keyed to the route that shows it — the
// journeys' rule that no action ends on a spinner or a bare toast.
//
// The session survives a reload via sessionStorage: with the real eSignet
// login (#155) a page refresh must not cost a whole redirect round-trip.
import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react";
import { api, ApiError, loginAs, setSession, whoAmI, FIX } from "@crest/api";

type Flash = { route: string; node: ReactNode } | null;

type Session = {
  me: string | null;
  err: string | null;
  flash: Flash;
  login: () => Promise<void>;
  completeEsignet: (token: string) => Promise<"enrolled" | "stranger">;
  signUp: (name: string, phone: string) => Promise<string>;
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

function readStored(): { token: string; me: string } | null {
  try {
    const raw = sessionStorage.getItem(SKEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}
function store(v: { token: string; me: string } | null) {
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
  const [err, setErr] = useState<string | null>(null);
  const [flash, setFlash] = useState<Flash>(null);
  const pendingToken = useRef<string>("");

  // The dev login: mock-OIDC minting a token for the story's Grace. Rendered
  // only on the local stack; a real deployment logs in through eSignet.
  const login = useCallback(async () => {
    const token = await loginAs(FIX.workerA);
    store({ token, me: FIX.workerA });
    setMe(FIX.workerA);
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
  const signUp = useCallback(async (name: string, phone: string) => {
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
    store({ token: pendingToken.current, me: party.id });
    setMe(party.id);
    setErr(null);
    return party.id as string;
  }, []);

  const logout = useCallback(() => {
    setSession(null);
    store(null);
    setMe(null);
    setFlash(null);
    setErr(null);
  }, []);
  const fail = useCallback((e: unknown) => setErr(describeError(e)), []);
  const clearErr = useCallback(() => setErr(null), []);

  const value = useMemo(
    () => ({ me, err, flash, login, completeEsignet, signUp, logout, setFlash, fail, clearErr }),
    [me, err, flash, login, completeEsignet, signUp, logout, fail, clearErr],
  );
  return <Ctx.Provider value={value}>{props.children}</Ctx.Provider>;
}
