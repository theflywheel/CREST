// The field app's state and actions. The session is the agent who is
// actually signed in — eSignet on any deployment, the dev shortcut only on
// the local stack — and every act this door records carries THAT party's id.
// Nothing here names a fixture party, a fixture project or a fixture
// definition: the project comes from the grants this agent holds, and the
// definition from the definitions the deployment has activated.
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { api, ApiError, loginAs, actingFor, setSession, whoAmI, isLocalStack, FIX } from "@crest/api";
import { lockQueue, queue, removeConsentAudio, saveConsentAudio, setQueue, pushDone, type Reg, unlockQueue } from "./queue";

export type Batch = {
  rowsTotal: number;
  rowsAccepted: number;
  rowsUnclear: number;
  rows: Array<{ label: string; ok: boolean }>;
};

export type DefRow = { id: string; state?: string; activity?: { label?: string; code?: string }; version?: number };
export type SourceRow = {
  systemRef: string;
  adapterRef?: string;
  sourceClass?: string;
  captureMethod?: string;
  sourceExposure?: string;
  contextId?: string;
  state?: string;
};

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
  sources: SourceRow[];
  sourceSystemRef: string | null;
  setSourceSystemRef: (id: string) => void;
  reg: Reg | null;
  setReg: (r: Reg | null) => void;
  batch: Batch | null;
  submitRegistration: (reg: Reg) => Promise<string>;
  retainSubmitted: (reg: Reg) => Promise<void>;
  addRosterId: (reg: Reg) => Promise<Reg>;
  syncQueued: (i: number) => Promise<"consent" | "hold" | null>;
  recordConsent: (audio: Blob) => Promise<void>;
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
type Stored = { token: string; partyId: string; label: string; contextId?: string; definitionId?: string; sourceSystemRef?: string };
const syncInFlight = new Set<string>();

function disputeIdempotencyKey(actor: string, claimId: string): string {
  const storageKey = `crest.field.dispute-key.${encodeURIComponent(actor)}.${encodeURIComponent(claimId)}`;
  try {
    const existing = sessionStorage.getItem(storageKey);
    if (existing) return existing;
    if (typeof globalThis.crypto?.randomUUID !== "function") {
      throw new Error("secure random identity is unavailable");
    }
    const key = `field-dispute:${globalThis.crypto.randomUUID()}`;
    sessionStorage.setItem(storageKey, key);
    return key;
  } catch {
    throw new Error("this browser cannot persist the dispute retry key; enable session storage and try again");
  }
}

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
  if (stored) {
    setSession(stored.token);
    void unlockQueue(stored.token + ":" + stored.partyId).catch(() => undefined);
  }
  const [me, setMe] = useState<Me | null>(stored ? { partyId: stored.partyId, label: stored.label } : null);
  const [err, setErr] = useState<string | null>(null);
  const [reg, setReg] = useState<Reg | null>(null);
  const [batch, setBatch] = useState<Batch | null>(null);
  const [contexts, setContexts] = useState<string[]>([]);
  const [contextId, setCtx] = useState<string | null>(stored?.contextId || null);
  const [grantsLoaded, setGrantsLoaded] = useState(false);
  const [definitions, setDefinitions] = useState<DefRow[]>([]);
  const [definitionId, setDefId] = useState<string | null>(stored?.definitionId || null);
  const [sources, setSources] = useState<SourceRow[]>([]);
  const [sourceSystemRef, setSourceRef] = useState<string | null>(stored?.sourceSystemRef || null);

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
  const setSourceSystemRef = useCallback((id: string) => {
    setSourceRef(id || null);
    const cur = readStored();
    if (cur) store({ ...cur, sourceSystemRef: id || undefined });
  }, []);

  // The dev shortcut, local stack only: mint a token for the party the local
  // stack has bound and sign in as them. The label still comes from the
  // registry, so nothing is asserted here that the deployment does not say.
  const devLogin = useCallback(async (partyId: string) => {
    const t = await loginAs(partyId);
    const who = await whoAmI();
    partyId = who.partyId;
    await unlockQueue(t + ":" + partyId);
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
    await unlockQueue(t + ":" + who.partyId);
    const label = await displayNameOf(who.partyId);
    setMe({ partyId: who.partyId, label });
    store({ token: t, partyId: who.partyId, label });
    setErr(null);
    return "signed" as const;
  }, []);

  const logout = useCallback(() => {
    setSession(null);
    lockQueue();
    store(null);
    setMe(null);
    setCtx(null);
    setDefId(null);
    setContexts([]);
    setGrantsLoaded(false);
    setReg(null);
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

  useEffect(() => {
    if (!contextId) {
      setSources([]);
      setSourceRef(null);
      return;
    }
    let live = true;
    api.get("evidence", "/v1/sources?contextId=" + encodeURIComponent(contextId)).then((out) => {
      if (!live) return;
      const srcs: SourceRow[] = (out && out.sources) || [];
      setSources(srcs);
      setSourceRef((cur) => {
        const next = cur && srcs.some((x) => x.systemRef === cur) ? cur : srcs.length === 1 ? srcs[0].systemRef : null;
        const storedNow = readStored();
        if (storedNow) store({ ...storedNow, sourceSystemRef: next || undefined });
        return next;
      });
    }).catch(() => live && setSources([]));
    return () => { live = false; };
  }, [contextId]);

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
    if (!r.actorId || !r.contextId || !r.operationId) {
      throw new Error("this registration has no trusted actor, project or operation identity and cannot be submitted");
    }
    if (r.actorId !== by || r.contextId !== ctx) {
      throw new Error("this held registration belongs to a different signed-in agent or project; select the original context to sync it");
    }
    const routes = [r.phone ? { kind: "phone", value: r.phone } : { kind: "supervisor", value: by }];
    const out = await api.post("parties", "/v1/enrolments", {
      party: { kind: "person", displayName: r.name, contactRoutes: routes },
      enrolledBy: by,
      contextId: ctx,
      // The method is a provenance fact about the enrolment, never a stored
      // judgement: "confidence-check" is w1_4's no-document route, and the
      // worker's assurance stays derived from identityBindings either way.
      method: r.method || "field-visit",
    }, r.operationId + ":enrolment");
    return out.party.id as string;
  };

  const retainSubmitted = async (r: Reg) => {
    const by = agent();
    const ctx = project();
    if (r.actorId !== by || r.contextId !== ctx || !r.partyId) {
      throw new Error("this registration cannot be retained for consent under the current agent or project");
    }
    const next = { ...r, state: "submitted" as const };
    await setQueue([next, ...queue().filter((item) => item.operationId !== r.operationId)]);
  };

  const addRosterId = async (r: Reg): Promise<Reg> => {
    if (!r.partyId || !r.rosterId || r.rosterState === "done") return r;
    const by = agent();
    const ctx = project();
    if (r.actorId !== by || r.contextId !== ctx) throw new Error("this registration belongs to another agent or project");
    try {
      actingFor(r.partyId);
      await api.post("parties", `/v1/parties/${encodeURIComponent(r.partyId)}/roster-ids`, {
        rosterId: r.rosterId,
        contextId: ctx,
      }, r.operationId + ":roster");
    } finally {
      actingFor(null);
    }
    return { ...r, rosterState: "done" };
  };

  const syncQueued = async (i: number): Promise<"consent" | "hold" | null> => {
    const q = queue();
    const initial = q[i];
    if (!initial) return null;
    if (!initial.operationId || !initial.actorId || !initial.contextId) {
      fail(new Error("this held registration has incomplete identity metadata and was not submitted"));
      return null;
    }
    const operationId = initial.operationId;
    if (syncInFlight.has(operationId)) return null;
    syncInFlight.add(operationId);
    setErr(null);
    try {
      let r = initial;
      if (r.state !== "submitted" || !r.partyId) {
        r = { ...r, partyId: await submitRegistration(r), state: "submitted" };
        // Persist the server result before moving on to roster and consent. A
        // reload after a successful request can then resume without posting
        // the registration a second time.
        const afterSubmit = queue();
        const currentIndex = afterSubmit.findIndex((item) => item.operationId === operationId);
        if (currentIndex < 0) throw new Error("the held registration changed while it was syncing; it was not removed");
        afterSubmit[currentIndex] = r;
        await setQueue(afterSubmit);
      }
      if (r.rosterId && r.rosterState !== "done") {
        r = await addRosterId(r);
        const afterRoster = queue();
        const currentIndex = afterRoster.findIndex((item) => item.operationId === operationId);
        if (currentIndex < 0) throw new Error("the held registration changed while it was syncing; it was not removed");
        afterRoster[currentIndex] = r;
        await setQueue(afterRoster);
      }
      setReg(r);
      return "consent";
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        if (!(e.code || "").startsWith("idempotency_")) {
          setReg(initial);
          return "hold";
        }
        fail(e);
        return null;
      }
      fail(e);
      return null;
    } finally {
      syncInFlight.delete(operationId);
    }
  };

  // Supervisor-assisted consent, voice channel. The body must be the bytes
  // captured from the microphone; a text description is never accepted as a
  // substitute for the worker's answer.
  const recordConsent = async (audio: Blob) => {
    if (!reg?.partyId) throw new Error("no registration in flight — register the worker first");
    if (!(audio instanceof Blob) || audio.size === 0) throw new Error("record a voice answer before saving consent");
    const by = agent();
    const ctx = project();
    if (reg.actorId !== by || reg.contextId !== ctx) {
      throw new Error("this consent belongs to a different signed-in agent or project and was not submitted");
    }
    try {
      await saveConsentAudio({
        operationId: reg.operationId,
        actorId: reg.actorId,
        contextId: reg.contextId,
        partyId: reg.partyId,
        mimeType: audio.type || "audio/ogg",
        audio,
      });
      if (!navigator.onLine) {
        throw new Error("No network is available; the voice recording is held securely and can resume when you reconnect.");
      }
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
        audio,
        audio.type || "audio/ogg",
        reg.operationId + ":consent",
      );
      await pushDone(reg);
      await removeConsentAudio(reg.operationId);
      await setQueue(queue().filter((item) => item.operationId !== reg.operationId));
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
        raisedByPartyId: by,
        reason: `The worker says the figure was ${figure}. In their words: ${reason} — recorded on their behalf by ${by} (supervisor-assisted).`,
      }, disputeIdempotencyKey(by, claimId));
    } finally {
      actingFor(null);
    }
  };

  // Canonical CSV to the evidence service, provenance in the query string;
  // per-row verdicts joined from the unclear queue.
  const closeRoster = async (csv: string) => {
    const by = agent();
    const ctx = project();
    const definition = definitions.find((d) => d.id === definitionId);
    if (!definitionId || !definition?.version)
      throw new Error(
        "choose the work definition this roster counts against — the deployment has activated more than one, and a batch must name the one it was checked against",
      );
    const q = new URLSearchParams({
      contextId: ctx,
      definitionId,
      submittedBy: by,
      definitionVersion: String(definition.version),
      systemRef: sourceSystemRef || "",
    });
    if (!sourceSystemRef) throw new Error("choose a registered source before closing the roster");
    const out = await api.postRaw("evidence", "/v1/batches?" + q, csv, "text/csv");
    const b = out.batch;
    const unclear = await api.get("evidence", "/v1/unclear?contextId=" + encodeURIComponent(ctx)).catch(() => ({ unclear: [] }));
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
      sources,
      sourceSystemRef,
      setSourceSystemRef,
      reg,
      setReg,
      batch,
      submitRegistration,
      retainSubmitted,
      addRosterId,
      syncQueued,
      recordConsent,
      assist,
      differ,
      closeRoster,
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [me, err, contexts, contextId, grantsLoaded, definitions, definitionId, sources, sourceSystemRef, reg, batch],
  );

  return <Ctx.Provider value={value}>{props.children}</Ctx.Provider>;
}

// The dev build's shortcut, and the ONE place this app names a fixture
// party: the seeded stack's supervisor. It is rendered only when
// isLocalStack, it works only where that seed exists, and no deployment ever
// reaches it — every other id in this file comes from a real read.
export const DEV_AGENT = FIX.supervisor;
export const devShortcut = isLocalStack;
