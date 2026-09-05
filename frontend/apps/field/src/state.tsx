// The field app's state and actions. The session is the agent who is
// actually signed in — eSignet on any deployment, the dev shortcut only on
// the local stack — and every act this door records carries THAT party's id.
// Nothing here names a fixture party, a fixture project or a fixture
// definition: the project comes from the grants this agent holds, and the
// definition from the definitions the deployment has activated.
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { api, ApiError, loginAs, actingFor, setSession, whoAmI, isLocalStack, FIX } from "@crest/api";
import { queue, setQueue, pushDone, type Reg } from "./queue";

export type Batch = {
  rowsTotal: number;
  rowsAccepted: number;
  rowsUnclear: number;
  rows: Array<{ label: string; ok: boolean }>;
};

export type DefRow = { id: string; state?: string; activity?: { label?: string; code?: string }; version?: number };

type Me = { partyId: string; label: string };

type State = {
  me: Me | null;
  err: string | null;
  fail: (e: unknown) => void;
  clearErr: () => void;
  devLogin: () => Promise<void>;
  completeEsignet: (token: string) => Promise<"signed" | "stranger">;
  logout: () => void;
  // The project this agent is working in, and the grants that offer it.
  contexts: string[];
  contextId: string | null;
  setContextId: (id: string) => void;
  grantsLoaded: boolean;
  // The definitions this deployment has activated; a definition document
  // carries no project reference, so the agent picks one rather than the
  // door guessing.
  definitions: DefRow[];
  definitionId: string | null;
  setDefinitionId: (id: string) => void;
  reg: Reg | null;
  setReg: (r: Reg | null) => void;
  batch: Batch | null;
  submitRegistration: (reg: Reg) => Promise<string>;
  syncQueued: (i: number) => Promise<"consent" | "hold" | null>;
  recordConsent: () => Promise<void>;
  assist: (claimId: string, partyId?: string) => Promise<void>;
  differ: (claimId: string, partyId: string | undefined, figure: string, reason: string) => Promise<void>;
  closeRoster: (csv: string) => Promise<void>;
};

const Ctx = createContext<State>(null as unknown as State);
export const useField = () => useContext(Ctx);

export const errText = (e: unknown) =>
  e instanceof ApiError
    ? `${e.status} ${e.code || ""} — ${e.message}`
    : String((e as Error)?.message || e);

export const short = (id: unknown) => {
  const s = String(id || "");
  return s.length > 18 ? s.slice(0, 10) + "…" + s.slice(-6) : s;
};
export const when = (ts?: string) =>
  ts ? new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short" }) : "—";

// The sentence every action says when the agent holds no project grant. A
// registration recorded under no programme would be a record with nobody
// answerable for it, so the door refuses rather than inventing a context.
export const NO_PROJECT =
  "You hold no project grant on this deployment yet, so there is no programme to record this under. Your organisation invites you through the console.";

const SKEY = "crest.field.session";
type Stored = { token: string; partyId: string; label: string; contextId?: string; definitionId?: string };

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

// The name to show is the registry's, read back from the party record — not
// a label this browser decided.
async function displayNameOf(partyId: string): Promise<string> {
  const p = await api.get("parties", `/v1/parties/${encodeURIComponent(partyId)}`).catch(() => null);
  const doc = (p && (p.party || p)) || null;
  return (doc && doc.displayName) || short(partyId);
}

export function FieldProvider(props: { children: ReactNode }) {
  const stored = readStored();
  if (stored) setSession(stored.token);
  const [me, setMe] = useState<Me | null>(stored ? { partyId: stored.partyId, label: stored.label } : null);
  const [err, setErr] = useState<string | null>(null);
  const [reg, setReg] = useState<Reg | null>(null);
  const [batch, setBatch] = useState<Batch | null>(null);
  const [contexts, setContexts] = useState<string[]>([]);
  const [contextId, setCtx] = useState<string | null>(stored?.contextId || null);
  const [grantsLoaded, setGrantsLoaded] = useState(false);
  const [definitions, setDefinitions] = useState<DefRow[]>([]);
  const [definitionId, setDefId] = useState<string | null>(stored?.definitionId || null);

  const fail = useCallback((e: unknown) => setErr(errText(e)), []);

  const setContextId = useCallback((id: string) => {
    setCtx(id);
    const cur = readStored();
    if (cur) store({ ...cur, contextId: id });
  }, []);
  const setDefinitionId = useCallback((id: string) => {
    setDefId(id);
    const cur = readStored();
    if (cur) store({ ...cur, definitionId: id });
  }, []);

  // The dev shortcut, local stack only: mint a token for the party the local
  // stack has bound and sign in as them. The label still comes from the
  // registry, so nothing is asserted here that the deployment does not say.
  const devLogin = useCallback(async (partyId: string) => {
    const t = await loginAs(partyId);
    const label = await displayNameOf(partyId);
    setMe({ partyId, label });
    store({ token: t, partyId, label });
    setErr(null);
  }, []);

  // The eSignet return leg: the callback handed this door a verified token;
  // ask the registry which party — if any — it makes us here.
  const completeEsignet = useCallback(async (t: string) => {
    setSession(t);
    const who = await whoAmI();
    if (!who.partyId) return "stranger" as const;
    const label = await displayNameOf(who.partyId);
    setMe({ partyId: who.partyId, label });
    store({ token: t, partyId: who.partyId, label });
    setErr(null);
    return "signed" as const;
  }, []);

  const logout = useCallback(() => {
    setSession(null);
    store(null);
    setMe(null);
    setCtx(null);
    setDefId(null);
    setContexts([]);
    setGrantsLoaded(false);
    setErr(null);
  }, []);

  // What this agent may work in: the distinct contexts of the grants they
  // hold. One grant needs no chooser; several get one; none is said plainly.
  useEffect(() => {
    if (!me) return;
    let live = true;
    (async () => {
      const mine = await api.get("parties", "/v1/authorizations/mine").catch(() => null);
      const ids = Array.from(
        new Set(
          ((mine && mine.authorizations) || [])
            .map((a: { scope?: { contextId?: string }; contextId?: string }) => a.contextId || a.scope?.contextId)
            .filter(Boolean) as string[],
        ),
      );
      if (!live) return;
      setContexts(ids);
      setGrantsLoaded(true);
      setCtx((cur) => {
        const next = cur && ids.includes(cur) ? cur : ids.length === 1 ? ids[0] : null;
        const s = readStored();
        if (s) store({ ...s, contextId: next || undefined });
        return next;
      });

      const listed = await api.get("definitions", "/v1/definitions?state=ACTIVE&limit=100").catch(() => null);
      if (!live) return;
      const defs: DefRow[] = (listed && listed.definitions) || [];
      setDefinitions(defs);
      setDefId((cur) => {
        const next = cur && defs.some((d) => d.id === cur) ? cur : defs.length === 1 ? defs[0].id : null;
        const s = readStored();
        if (s) store({ ...s, definitionId: next || undefined });
        return next;
      });
    })();
    return () => {
      live = false;
    };
  }, [me]);

  const agent = () => {
    if (!me) throw new Error("you are not signed in on this device");
    return me.partyId;
  };
  const project = () => {
    if (!contextId) throw new Error(NO_PROJECT);
    return contextId;
  };

  // POST /v1/enrolments, recorded by the agent who is signed in; a worker
  // with no phone gets that agent as their contact route, and the roster id
  // is its own registry key, added right after (acting for the new party).
  const submitRegistration = async (r: Reg) => {
    const by = agent();
    const ctx = project();
    const routes = [r.phone ? { kind: "phone", value: r.phone } : { kind: "supervisor", value: by }];
    const out = await api.post("parties", "/v1/enrolments", {
      party: { kind: "person", displayName: r.name, contactRoutes: routes },
      enrolledBy: by,
      contextId: ctx,
      // The method is a provenance fact about the enrolment, never a stored
      // judgement: "confidence-check" is w1_4's no-document route, and the
      // worker's assurance stays derived from identityBindings either way.
      method: r.method || "field-visit",
    });
    const partyId: string = out.party.id;
    if (r.rosterId) {
      try {
        actingFor(partyId);
        await api.post("parties", `/v1/parties/${encodeURIComponent(partyId)}/roster-ids`, {
          rosterId: r.rosterId,
          contextId: ctx,
        });
      } finally {
        actingFor(null);
      }
    }
    return partyId;
  };

  const syncQueued = async (i: number): Promise<"consent" | "hold" | null> => {
    const q = queue();
    const r = q[i];
    if (!r) return null;
    setErr(null);
    try {
      r.partyId = await submitRegistration(r);
      q.splice(i, 1);
      setQueue(q);
      pushDone(r);
      setReg(r);
      return "consent";
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        q.splice(i, 1);
        setQueue(q);
        setReg(r);
        return "hold";
      }
      fail(e);
      return null;
    }
  };

  // Supervisor-assisted consent, voice channel — the recording body travels
  // as the artefact. The dev stand-in is text; a device build attaches audio.
  const recordConsent = async () => {
    if (!reg?.partyId) throw new Error("no registration in flight — register the worker first");
    const by = agent();
    const ctx = project();
    try {
      actingFor(reg.partyId);
      const q = new URLSearchParams({
        moment: "enrolment",
        captureMethod: "voice",
        purpose: "hold and fetch evidence of my work",
        capturedBy: by,
        contextId: ctx,
      });
      await api.postRaw(
        "parties",
        `/v1/parties/${encodeURIComponent(reg.partyId)}/consents?` + q,
        `${reg.name} answered yes to the enrolment consent script, read aloud in Kiswahili.`,
        "audio/ogg",
      );
    } finally {
      actingFor(null);
    }
  };

  // The assisted exit: act for the worker, confirm with route=assisted and
  // the signed-in agent's id in assistedByPartyId — never a quieter route.
  const assist = async (claimId: string, partyId?: string) => {
    const by = agent();
    try {
      const pid =
        partyId || (await api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`)).partyId;
      actingFor(pid);
      await api.post("confirmation", `/v1/claims/${encodeURIComponent(claimId)}/confirm`, {
        route: "assisted",
        assistedByPartyId: by,
      });
    } finally {
      actingFor(null);
    }
  };

  const differ = async (claimId: string, partyId: string | undefined, figure: string, reason: string) => {
    const by = agent();
    try {
      const pid =
        partyId || (await api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`)).partyId;
      actingFor(pid);
      await api.post("confirmation", `/v1/claims/${encodeURIComponent(claimId)}/dispute`, {
        raisedByPartyId: pid,
        reason: `The worker says the figure was ${figure}. In their words: ${reason} — recorded on their behalf by ${by} (supervisor-assisted).`,
      });
    } finally {
      actingFor(null);
    }
  };

  // Canonical CSV to the evidence service, provenance in the query string;
  // per-row verdicts joined from the unclear queue.
  const closeRoster = async (csv: string) => {
    const by = agent();
    const ctx = project();
    if (!definitionId)
      throw new Error(
        "choose the work definition this roster counts against — the deployment has activated more than one, and a batch must name the one it was checked against",
      );
    const q = new URLSearchParams({
      contextId: ctx,
      definitionId,
      submittedBy: by,
      sourceClass: "programme-system",
      captureMethod: "digital-capture",
      sourceExposure: "signed-batch",
      systemRef: "field-app-roster",
    });
    const out = await api.postRaw("evidence", "/v1/batches?" + q, csv, "text/csv");
    const b = out.batch;
    const unclear = await api.get("evidence", "/v1/unclear").catch(() => ({ unclear: [] }));
    const unclearRefs = new Set<string>(
      (unclear.unclear || [])
        .filter((u: { resolvedAt?: string }) => !u.resolvedAt)
        .map((u: { rowRef?: string }) => String(u.rowRef || "")),
    );
    const lines = csv.trim().split(/\r?\n/);
    const header = (lines[0] || "").split(",").map((s) => s.trim());
    const wi = header.indexOf("worker_id");
    const ai = header.indexOf("activity");
    const sri = header.indexOf("source_record_ref");
    const rows = lines
      .slice(1)
      .filter((l) => l.trim())
      .map((l, idx) => {
        const c = l.split(",");
        const ref = sri >= 0 ? (c[sri] || "").trim() : "";
        const unclearHit = [...unclearRefs].some((r) => r && (r === ref || r.includes(ref) || ref.includes(r)));
        return {
          label: `${ai >= 0 ? (c[ai] || "").trim() : "row " + (idx + 1)} · ${wi >= 0 ? (c[wi] || "").trim() : ""}`,
          ok: !unclearHit || !b.rowsUnclear,
        };
      });
    setBatch({ rowsTotal: b.rowsTotal, rowsAccepted: b.rowsAccepted, rowsUnclear: b.rowsUnclear, rows });
  };

  const value = useMemo(
    () => ({
      me,
      err,
      fail,
      clearErr: () => setErr(null),
      devLogin: () => devLogin(DEV_AGENT),
      completeEsignet,
      logout,
      contexts,
      contextId,
      setContextId,
      grantsLoaded,
      definitions,
      definitionId,
      setDefinitionId,
      reg,
      setReg,
      batch,
      submitRegistration,
      syncQueued,
      recordConsent,
      assist,
      differ,
      closeRoster,
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [me, err, contexts, contextId, grantsLoaded, definitions, definitionId, reg, batch],
  );

  return <Ctx.Provider value={value}>{props.children}</Ctx.Provider>;
}

// The dev build's shortcut, and the ONE place this app names a fixture
// party: the seeded stack's supervisor. It is rendered only when
// isLocalStack, it works only where that seed exists, and no deployment ever
// reaches it — every other id in this file comes from a real read.
export const DEV_AGENT = FIX.supervisor;
export const devShortcut = isLocalStack;
