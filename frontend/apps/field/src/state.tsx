// The field app's state and actions, ported 1:1 from apps/enrolment: Naomi's
// session, the registration in flight, the last roster close, and every
// service call the vanilla app made — same endpoints, same acting-for
// discipline (actingFor is always cleared, even on failure).
import { createContext, useContext, useState, type ReactNode } from "react";
import { api, ApiError, loginAs, actingFor, FIX } from "@crest/api";
import { queue, setQueue, pushDone, type Reg } from "./queue";

export type Batch = {
  rowsTotal: number;
  rowsAccepted: number;
  rowsUnclear: number;
  rows: Array<{ label: string; ok: boolean }>;
};

type State = {
  me: { partyId: string; label: string } | null;
  err: string | null;
  fail: (e: unknown) => void;
  clearErr: () => void;
  login: () => Promise<void>;
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

export function FieldProvider(props: { children: ReactNode }) {
  const [me, setMe] = useState<State["me"]>(null);
  const [err, setErr] = useState<string | null>(null);
  const [reg, setReg] = useState<Reg | null>(null);
  const [batch, setBatch] = useState<Batch | null>(null);

  const fail = (e: unknown) => setErr(errText(e));

  const login = async () => {
    await loginAs(FIX.supervisor);
    setMe({ partyId: FIX.supervisor, label: "Naomi" });
  };

  // The same registration the PoC's agent face posts: POST /v1/enrolments; a
  // worker with no phone gets the supervisor contact route, and the roster id
  // is its own registry key, added right after (acting for the new party).
  const submitRegistration = async (r: Reg) => {
    const routes = [r.phone ? { kind: "phone", value: r.phone } : { kind: "supervisor", value: FIX.supervisor }];
    const out = await api.post("parties", "/v1/enrolments", {
      party: { kind: "person", displayName: r.name, contactRoutes: routes },
      enrolledBy: FIX.supervisor,
      contextId: FIX.project,
      method: "field-visit",
    });
    const partyId: string = out.party.id;
    if (r.rosterId) {
      try {
        actingFor(partyId);
        await api.post("parties", `/v1/parties/${encodeURIComponent(partyId)}/roster-ids`, {
          rosterId: r.rosterId,
          contextId: FIX.project,
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

  // The consent the PoC posts: supervisor-assisted, voice channel — the
  // recording body travels as the artefact. The dev stand-in is text; a
  // device build attaches audio.
  const recordConsent = async () => {
    if (!reg?.partyId) throw new Error("no registration in flight — register the worker first");
    try {
      actingFor(reg.partyId);
      const q = new URLSearchParams({
        moment: "enrolment",
        captureMethod: "voice",
        purpose: "hold and fetch evidence of my work",
        capturedBy: FIX.supervisor,
        contextId: FIX.project,
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
  // the supervisor's name in assistedByPartyId — never a quieter route.
  const assist = async (claimId: string, partyId?: string) => {
    try {
      const pid =
        partyId || (await api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`)).partyId;
      actingFor(pid);
      await api.post("confirmation", `/v1/claims/${encodeURIComponent(claimId)}/confirm`, {
        route: "assisted",
        assistedByPartyId: FIX.supervisor,
      });
    } finally {
      actingFor(null);
    }
  };

  const differ = async (claimId: string, partyId: string | undefined, figure: string, reason: string) => {
    try {
      const pid =
        partyId || (await api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`)).partyId;
      actingFor(pid);
      await api.post("confirmation", `/v1/claims/${encodeURIComponent(claimId)}/dispute`, {
        raisedByPartyId: pid,
        reason: `The worker says the figure was ${figure}. In their words: ${reason} — recorded on their behalf by ${FIX.supervisor} (supervisor-assisted).`,
      });
    } finally {
      actingFor(null);
    }
  };

  // The batch the PoC's supervisor face posts: canonical CSV to the evidence
  // service, provenance in the query string; per-row verdicts joined from the
  // unclear queue.
  const closeRoster = async (csv: string) => {
    const q = new URLSearchParams({
      contextId: FIX.project,
      definitionId: FIX.definition,
      submittedBy: FIX.supervisor,
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

  return (
    <Ctx.Provider
      value={{
        me,
        err,
        fail,
        clearErr: () => setErr(null),
        login,
        reg,
        setReg,
        batch,
        submitRegistration,
        syncQueued,
        recordConsent,
        assist,
        differ,
        closeRoster,
      }}
    >
      {props.children}
    </Ctx.Provider>
  );
}
