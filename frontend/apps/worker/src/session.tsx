// Session + flash state. The flash carries the "What happens next" block a
// terminal action leaves behind, keyed to the route that shows it — the
// journeys' rule that no action ends on a spinner or a bare toast.
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { ApiError, loginAs, setSession, FIX } from "@crest/api";

type Flash = { route: string; node: ReactNode } | null;

type Session = {
  me: string | null;
  err: string | null;
  flash: Flash;
  login: () => Promise<void>;
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

export function SessionProvider(props: { children: ReactNode }) {
  const [me, setMe] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [flash, setFlash] = useState<Flash>(null);

  const login = useCallback(async () => {
    await loginAs(FIX.workerA);
    setMe(FIX.workerA);
    setErr(null);
  }, []);
  const logout = useCallback(() => {
    setSession(null);
    setMe(null);
    setFlash(null);
    setErr(null);
  }, []);
  const fail = useCallback((e: unknown) => setErr(describeError(e)), []);
  const clearErr = useCallback(() => setErr(null), []);

  const value = useMemo(
    () => ({ me, err, flash, login, logout, setFlash, fail, clearErr }),
    [me, err, flash, login, logout, fail, clearErr],
  );
  return <Ctx.Provider value={value}>{props.children}</Ctx.Provider>;
}
