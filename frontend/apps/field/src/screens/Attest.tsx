// Assisted confirmation and evidence intake — NOT J8/W-4 source attestation.
//
// The reference's W-4 flow (w3_1..w3_5) has a supervisor confirm work inside
// the delivery/source platform, whose roster close then reaches CREST as
// ingested evidence. That platform is not CREST's to build, and this file no
// longer pretends to be it (docs/JOURNEY_GAP_ASSESSMENT.md finding 3;
// traceability rows w3_1–w3_5 record the difference). What is here is real
// and CREST-side: assisted W1/W4 confirmation-window exits for workers who
// cannot be reached — every exit releases payment — and the roster-CSV
// evidence intake into CREST's own evidence service.
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api } from "@crest/api";
import { Chip, DisLi, KV, Sidecar, OpenNote, NextBlock } from "@crest/ui";
import { useField, short, when } from "../state";

type Win = { partyId?: string; closesAt?: string; exitRoute?: string };

// deps: what the load reads from the session. The project is discovered
// after sign-in, so a scoped read must run again once it is known.
function useLoad<T>(fn: () => Promise<T>, deps: unknown[] = []): T | undefined {
  const [data, setData] = useState<T>();
  useEffect(() => {
    let live = true;
    fn().then((d) => live && setData(d));
    return () => {
      live = false;
    };
  }, deps); // eslint-disable-line react-hooks/exhaustive-deps
  return data;
}

export function ToConfirm() {
  const nav = useNavigate();
  const s = useField();
  // A review list is always scoped to the project (§9): the substrate refuses
  // an unscoped read rather than exporting everybody's windows.
  const out = useLoad(
    () => api.get("confirmation", "/v1/unreached?contextId=" + encodeURIComponent(s.contextId || "")),
    [s.contextId],
  );
  const list: Array<{ claimId: string; partyId?: string; closesAt?: string }> = out
    ? out.windows || out.unreached || []
    : [];
  return (
    <>
      <div className="scr-title m">Workers waiting on you</div>
      <Chip sm kind="info">assisted confirmation — a CREST window exit, not source attestation</Chip>
      <p className="muted">
        Open windows whose worker could not be told — no phone, or no signal. You are their route; an assisted exit is
        one of the four, recorded as itself with your name on it. Every exit releases payment. A window left past its
        close auto-confirms — the quietest of the four exits — and CREST is told either way.
      </p>
      <OpenNote>
        Honest boundary: the reference's W-4 worklist lives in the delivery platform (DIGIT HCM in the storyboard),
        where a supervisor attests or corrects the source record before it ever reaches CREST. That surface is the
        source system's, not CREST's — this screen is the later, CREST-side moment: exiting a worker's confirmation
        window on their behalf.
      </OpenNote>
      {list.length ? (
        list.map((w) => (
          <div className="card" key={w.claimId}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8 }}>
              <span className="mono">{short(w.claimId)}</span>
              <Chip sm kind="warn">closes {when(w.closesAt)}</Chip>
            </div>
            <span className="muted">
              worker <span className="mono">{short(w.partyId)}</span>
            </span>
            <div className="btn-row" style={{ marginTop: 10 }}>
              <button className="btn dominant" onClick={() => nav("/confirmsee/" + encodeURIComponent(w.claimId))}>
                Confirm what you saw
              </button>
              <button className="btn secondary" onClick={() => nav("/differ/" + encodeURIComponent(w.claimId))}>
                Different figure
              </button>
            </div>
          </div>
        ))
      ) : (
        <div className="card quiet">
          <span className="muted">
            Nobody is waiting on you. When a window opens for a worker who cannot be reached, they appear here.
          </span>
        </div>
      )}
    </>
  );
}

export function ConfirmSaw() {
  const s = useField();
  const nav = useNavigate();
  const claimId = decodeURIComponent(useParams().claimId || "");
  const win = useLoad<Win | null>(() =>
    api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`).catch(() => null),
  );
  const doAssist = async () => {
    try {
      await s.assist(claimId, win?.partyId);
      nav("/confirmed/" + encodeURIComponent(claimId));
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <>
      <div className="scr-title m">Confirm what you saw</div>
      <div className="card">
        <span className="eyebrow">Claim</span>
        <div className="mono" style={{ wordBreak: "break-all" }}>
          {claimId}
        </div>
        {win ? (
          <KV
            rows={[
              ["worker", <span className="mono">{short(win.partyId)}</span>],
              ["window closes", when(win.closesAt)],
              ...(win.exitRoute ? ([["already exited", win.exitRoute]] as Array<[string, string]>) : []),
            ]}
          />
        ) : null}
      </div>
      <Sidecar>
        You are confirming on the worker's behalf what you saw done. The exit is recorded as assisted — never as if the
        worker pressed the button themselves. You are confirming a figure the programme's system already recorded:
        CREST never asked you for a number and has no screen where you could have entered one.
      </Sidecar>
      {win && !win.exitRoute ? (
        <div className="btn-row">
          <button className="btn" onClick={doAssist}>
            Confirm — it is right
          </button>
        </div>
      ) : win ? (
        <div className="card quiet">
          <span className="muted">
            This window has already exited ({win.exitRoute}); payment was released by that exit.
          </span>
        </div>
      ) : null}
    </>
  );
}

export function AssistedDone() {
  const nav = useNavigate();
  const claimId = decodeURIComponent(useParams().claimId || "");
  return (
    <>
      <div className="card hi">
        <Chip kind="ok">assisted confirmation recorded</Chip>
        <p className="body-2" style={{ marginTop: 8 }}>
          Claim <span className="mono">{short(claimId)}</span> exited by the assisted route.
        </p>
      </div>
      <NextBlock
        happened="The window closed by assisted confirmation — recorded as assisted, with your name on it, never a quieter route."
        who="The system: the credential is issued and the payment instruction raised now."
        when="Immediately — every one of the four exits releases payment."
        told="The worker's record shows the credential; a worker with no phone hears through you on your next visit."
        ifnot="nothing here waits on anyone — the exit itself released the payment."
      />
      <div className="btn-row">
        <button className="btn secondary" onClick={() => nav("/toconfirm")}>
          Back to the worklist
        </button>
      </div>
    </>
  );
}

export function Differ() {
  const s = useField();
  const nav = useNavigate();
  const claimId = decodeURIComponent(useParams().claimId || "");
  const win = useLoad<Win | null>(() =>
    api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`).catch(() => null),
  );
  const [figure, setFigure] = useState("");
  const [reason, setReason] = useState("");
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    try {
      await s.differ(claimId, win?.partyId, figure.trim(), reason.trim());
      nav("/differed/" + encodeURIComponent(claimId));
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <>
      <div className="scr-title m">Record a different figure</div>
      <div className="card">
        <span className="eyebrow">Claim</span>
        <div className="mono" style={{ wordBreak: "break-all" }}>
          {claimId}
        </div>
        <form id="differform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 10 }}>
          <label className="body-2">
            What the worker says the figure was
            <input name="figure" required placeholder="e.g. 9, not 12" value={figure} onChange={(e) => setFigure(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
          </label>
          <label className="body-2">
            In the worker's words, why
            <textarea name="reason" rows={3} required value={reason} onChange={(e) => setReason(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
          </label>
          <button className="btn" type="submit">
            Record the dispute
          </button>
        </form>
      </div>
      <Sidecar>
        A dispute contests the record, never the money — the worker is paid either way, and the underlying record of the
        work is never destroyed. Both figures travel together: the worker's and the recorded one, side by side, for
        whoever answers the contest.
      </Sidecar>
      <OpenNote>
        Honest gap: the L1 dispute endpoint carries the worker's dispute but has no assistedBy field the way
        confirmation does — this screen records it on the worker's behalf and names you inside the reason text. Whether
        dispute needs a first-class assisted marker is a design question for the confirmation service.
      </OpenNote>
    </>
  );
}

export function DifferDone() {
  const nav = useNavigate();
  const claimId = decodeURIComponent(useParams().claimId || "");
  return (
    <>
      <div className="card hi">
        <Chip kind="warn">dispute on the record</Chip>
        <p className="body-2" style={{ marginTop: 8 }}>
          Claim <span className="mono">{short(claimId)}</span> exited by dispute —{" "}
          <strong>and the payment is released anyway</strong>.
        </p>
      </div>
      <NextBlock
        happened="The worker's dispute closed the window. The record is contested; the money is not withheld."
        who="The issuer answers the contest; it stays visible to any verifier until they do."
        when="The contest has no expiry — it stands until answered."
        told="The claim shows as contested in the worker's record and in any verification of the credential."
        ifnot="nothing expires here — an unanswered contest simply stays visible."
      />
      <div className="btn-row">
        <button className="btn secondary" onClick={() => nav("/toconfirm")}>
          Back to the worklist
        </button>
      </div>
    </>
  );
}

const SAMPLE_CSV = `activity,outcome_value,outcome_unit,worker_id_kind,worker_id,period_start,period_end,geography,source_record_ref,household_id,beneficiary_count,supervisor_present
bednet-distribution,12,bednets-distributed,phone,+15550100011,2026-03-02,2026-03-02,ward-7,roster-close-1,HH-101,4,true`;

export function Roster() {
  const s = useField();
  const nav = useNavigate();
  const [csv, setCsv] = useState(SAMPLE_CSV);
  const [file, setFile] = useState<File | null>(null);
  const b = s.batch;
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    try {
      await s.closeRoster(file ? await file.text() : csv);
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <>
      <div className="scr-title m">Evidence intake — the month's tally, row by row</div>
      <p className="muted">
        The file is checked against the definition. A row that does not match becomes somebody named in the unclear
        queue — never a silent drop.
      </p>
      <Chip sm kind="info">CREST's evidence intake — in the reference (w3_4) the roster closes in the delivery platform first</Chip>
      <div className="card">
        <form id="rosterform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          {/* A definition document carries no project reference, so the door
              cannot derive which one this roster counts against. The agent
              names it, from what the deployment has activated. */}
          <label className="body-2">
            Work definition this roster counts against
            {s.definitions.length ? (
              <select
                id="definition-select"
                value={s.definitionId || ""}
                onChange={(e) => s.setDefinitionId(e.target.value)}
                style={{ width: "100%", marginTop: 4, font: "inherit" }}
              >
                <option value="">Choose a definition…</option>
                {s.definitions.map((d) => (
                  <option key={d.id} value={d.id}>
                    {(d.activity && d.activity.label) || d.id}
                    {d.version ? ` · v${d.version}` : ""} · {short(d.id)}
                  </option>
                ))}
              </select>
            ) : (
              <span className="muted" style={{ display: "block", marginTop: 4 }}>
                This deployment has no active work definition, so there is nothing a batch could be checked against.
                A definition is authored and ratified in the console before evidence can be taken in.
              </span>
            )}
          </label>
          <label className="body-2">
            Registered source this roster came from
            {s.sources.length ? (
              <select
                id="source-select"
                value={s.sourceSystemRef || ""}
                onChange={(e) => s.setSourceSystemRef(e.target.value)}
                style={{ width: "100%", marginTop: 4, font: "inherit" }}
              >
                <option value="">Choose a registered source…</option>
                {s.sources.map((source) => (
                  <option key={source.systemRef} value={source.systemRef}>
                    {source.systemRef} · {source.sourceClass || "approved class"} · {source.captureMethod || "approved capture"} · {source.sourceExposure || "approved exposure"}
                  </option>
                ))}
              </select>
            ) : (
              <span className="muted" style={{ display: "block", marginTop: 4 }}>
                No source is registered for this project. Register and approve the source in the console before sending a roster.
              </span>
            )}
          </label>
          <label className="body-2">
            CSV file
            <input type="file" name="file" accept=".csv,text/csv" onChange={(e) => setFile(e.target.files?.[0] || null)} style={{ font: "inherit", marginTop: 4 }} />
          </label>
          <label className="body-2">
            …or paste it
            <textarea name="csv" rows={6} className="mono" value={csv} onChange={(e) => setCsv(e.target.value)} style={{ width: "100%", marginTop: 4, font: "500 11px/1.6 var(--mono)" }} />
          </label>
          <button className="btn" type="submit">
            Close the roster
          </button>
        </form>
      </div>
      {b ? (
        <div className="card">
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            <Chip kind="ok">{b.rowsAccepted} accepted</Chip>
            {b.rowsUnclear ? <Chip kind="warn">{b.rowsUnclear} unclear</Chip> : null}
            <Chip kind="plain">{b.rowsTotal} rows</Chip>
          </div>
          {(b.rows || []).map((r, i) => (
            <div key={i} style={{ display: "flex", justifyContent: "space-between", gap: 8, borderTop: "1px solid var(--generic-bg)", padding: "8px 0 2px", marginTop: 8 }}>
              <span className="body-2">{r.label}</span>
              <Chip sm kind={r.ok ? "ok" : "warn"}>{r.ok ? "accepted" : "unclear"}</Chip>
            </div>
          ))}
          <div className="btn-row" style={{ marginTop: 12 }}>
            <button className="btn" onClick={() => nav("/handoff")}>
              Who holds it next
            </button>
          </div>
        </div>
      ) : null}
    </>
  );
}

export function Handoff() {
  const s = useField();
  const data = useLoad(async () => {
    const [unclear, unreached] = await Promise.all([
      api.get("evidence", "/v1/unclear?contextId=" + encodeURIComponent(s.contextId || "")).catch(() => ({ unclear: [] })),
      api.get("confirmation", "/v1/unreached?contextId=" + encodeURIComponent(s.contextId || "")).catch(() => ({ windows: [], unreached: [] })),
    ]);
    return { unclear, unreached };
  }, [s.contextId]);
  const uc: Array<{ id?: string; rowRef?: string; kind?: string; reason?: string; resolvedAt?: string }> = data
    ? (data.unclear.unclear || []).filter((u: { resolvedAt?: string }) => !u.resolvedAt)
    : [];
  const ur: unknown[] = data ? data.unreached.windows || data.unreached.unreached || [] : [];
  const b = s.batch;
  return (
    <>
      <div className="scr-title m">Who holds it next</div>
      {b ? (
        <div className="card quiet">
          <span className="body-2">
            Your last close: <b>{b.rowsAccepted}</b> accepted, <b>{b.rowsUnclear}</b> unclear of {b.rowsTotal}.
          </span>
        </div>
      ) : null}
      <div className="stats">
        <div className="stat">
          <div className="n">{uc.length}</div>
          <div className="l">unclear rows — the custodian's queue</div>
        </div>
        <div className="stat">
          <div className="n">{ur.length}</div>
          <div className="l">open windows waiting on you</div>
        </div>
      </div>
      {uc.length ? (
        <>
          <span className="eyebrow">In the custodian's queue</span>
          {uc.slice(0, 8).map((u, i) => (
            <div className="card quiet" key={i}>
              <div style={{ display: "flex", justifyContent: "space-between", gap: 8 }}>
                <span className="mono">{u.rowRef || u.id}</span>
                <Chip sm kind="warn">{u.kind}</Chip>
              </div>
              <span className="muted">{u.reason}</span>
            </div>
          ))}
        </>
      ) : null}
      <div className="card">
        <span className="eyebrow">Who holds it next — the chain (w3_5)</span>
        <div className="dis" style={{ marginTop: 8 }}>
          <DisLi on t="The attestor confirms, in the delivery platform" s="Tier 2 evidence, in their own name, inside the system that recorded it" />
          <DisLi on t="Matching / validation engine" s="The matching service, then a validator if it cannot clear" />
          <DisLi on t="Evidence validator" s="Only where the project configured a manual step" />
          <DisLi on t="Credential issued automatically" s="signed by the organisation — releases payment. No person signs it." />
        </div>
        <p className="muted" style={{ marginTop: 8 }}>
          The manual validation step is a per-project configuration choice, not a structural requirement. Many projects
          will have no human here at all.
        </p>
      </div>
      <NextBlock
        happened="Accepted rows became claims; each opens the worker's seven-day window. Unclear rows went to the custodian — a mismatch is somebody named, not a status."
        who="The registry custodian on the unclear rows; the workers (or you, assisted) on the open windows."
        when="Windows close in seven days at the latest; every exit releases payment."
        told="Workers you are the route for reappear under To confirm; the custodian's attributions land as claims like any other."
        ifnot="You will not hear anything if it passes. Silence means it worked. You are told only if it is sent back — and if a row stays unclear, no one is silently unpaid by it: the row waits, named, until the custodian decides whose work it was."
      />
    </>
  );
}
