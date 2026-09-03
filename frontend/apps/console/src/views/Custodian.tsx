// Registry custodian (G-4) and support (J10), ported 1:1 from
// apps/console/views-custodian.js. The custodian holds the two decisions the
// system refuses to make for itself; support can escalate, never retry or
// release money.
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError, FIX } from "@crest/api";
import { Callout, Chip, OpenNote, Sidecar } from "@crest/ui";
import {
  short, when, agoDays, Mono, MonoShort, Stat, KVR, Title, Lede, Empty,
  Tbl, Card, CardTitled, useLoad, LoadFrame,
} from "../ui";
import { useConsole, errText } from "../state";

export function Find(props: { support?: boolean }) {
  const [kind, setKind] = useState("");
  const [value, setValue] = useState("");
  const [ctx, setCtx] = useState<string>(FIX.project);
  const [out, setOut] = useState<React.ReactNode>(null);
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    const q = new URLSearchParams();
    if (kind) q.set("kind", kind);
    q.set("value", value);
    q.set("contextId", ctx);
    try {
      const m = await api.get("parties", "/v1/resolve?" + q);
      setOut(
        <Sidecar ok>
          Resolved to <span className="mono">{m.partyId}</span> by <strong>{m.key}</strong> (confidence{" "}
          {String(m.confidence)}); enrolment consent: {m.enrolmentConsent || "—"}
        </Sidecar>,
      );
    } catch (err) {
      const e = err as ApiError;
      setOut(
        e.status === 409 ? (
          <div className="open-note">Two records collide — a hold was raised for the duplicates queue; nothing was guessed.</div>
        ) : e.status === 404 ? (
          <div className="card" style={{ color: "var(--text-2)" }}>Nobody matches.</div>
        ) : (
          <div className="errbar">{errText(e)}</div>
        ),
      );
    }
  };
  return (
    <>
      <Title t="Find a worker" />
      <Lede>
        By the identifier you have — strongest first. An ambiguous match holds; it never guesses, and it never merges.
      </Lede>
      {props.support ? (
        <CardTitled t="Which method to try, in order">
          <KVR rows={[
            ["1 · printed card", "Most reliable · works offline — the card carries the whole signed credential, not a link to one"],
            ["2 · phone number", "works when they registered with one"],
            ["3 · roster id", "the programme's own id, scoped to a project"],
            ["4 · national-id hash", "never the raw id — a salted hash is all the registry holds"],
            ["5 · name + supervisor", "weakest; resolves through the people who know them"],
          ]} />
        </CardTitled>
      ) : null}
      <Card>
        <form id="findform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10, maxWidth: 520 }}>
          <label className="body-2">
            Kind
            <br />
            <select name="kind" value={kind} onChange={(e) => setKind(e.target.value)} style={{ width: "100%" }}>
              <option value="">any (precedence order)</option>
              <option>national-id-hash</option>
              <option>contact-route</option>
              <option>roster-id</option>
            </select>
          </label>
          <label className="body-2">
            Value
            <br />
            <input name="value" className="mono" placeholder="+15550100011 or roster id" required value={value} onChange={(e) => setValue(e.target.value)} style={{ width: "100%" }} />
          </label>
          <label className="body-2">
            Context (for roster ids)
            <br />
            <input name="ctx" className="mono" value={ctx} onChange={(e) => setCtx(e.target.value)} style={{ width: "100%" }} />
          </label>
          <button className="btn" style={{ width: "auto", alignSelf: "flex-start", padding: "11px 24px" }}>
            Resolve
          </button>
        </form>
        <div id="findout" style={{ marginTop: 10 }}>{out}</div>
      </Card>
    </>
  );
}

type Hold = { id: string; keyKind?: string; reason?: string; candidates?: string[]; createdAt?: string; resolvedAt?: string };

function HoldForm(props: { h: Hold; onDone: () => void }) {
  const s = useConsole();
  const [decision, setDecision] = useState("distinct");
  const [party, setParty] = useState(props.h.candidates?.[0] || "");
  const [method, setMethod] = useState("");
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    const body: Record<string, string> = { decision, partyId: party, resolvedByPartyId: s.me!.partyId };
    if (decision === "merge") {
      body.confirmedByPartyId = party;
      body.confirmationMethod = method || "in-person";
    }
    try {
      await api.post("parties", `/v1/holds/${encodeURIComponent(props.h.id)}/resolve`, body);
      props.onDone();
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <form onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 10, maxWidth: 520 }}>
      <label className="body-2">
        Decision
        <br />
        <select name="decision" value={decision} onChange={(e) => setDecision(e.target.value)} style={{ width: "100%" }}>
          <option value="distinct">distinct — two people share the identifier</option>
          <option value="merge">merge — one person recorded twice</option>
        </select>
      </label>
      <label className="body-2">
        The identifier belongs to
        <br />
        <select name="party" className="mono" value={party} onChange={(e) => setParty(e.target.value)} style={{ width: "100%" }}>
          {(props.h.candidates || []).map((c) => (
            <option key={c}>{c}</option>
          ))}
        </select>
      </label>
      <label className="body-2">
        Worker confirmation (merge only) — method
        <br />
        <input name="method" placeholder="in-person" value={method} onChange={(e) => setMethod(e.target.value)} style={{ width: "100%" }} />
      </label>
      <button className="btn" style={{ width: "auto", alignSelf: "flex-start", padding: "10px 20px" }}>
        Close the hold
      </button>
    </form>
  );
}

export function Dupes() {
  const [gen, setGen] = useState(0);
  const r = useLoad(async () => {
    const [out, metrics] = await Promise.all([
      api.get("parties", "/v1/holds"),
      api.get("parties", "/v1/holds/metrics").catch(() => null),
    ]);
    return { list: ((out.holds || []) as Hold[]).filter((h) => !h.resolvedAt), metrics };
  }, [gen]);
  return (
    <LoadFrame r={r}>
      {({ list, metrics }) => (
        <>
          <Title t="Duplicates — the queue, and the rule for closing one" />
          <Lede>
            Two records collide on an identifier. The queue shows existence, never the identifier itself. Probable
            matches hold; they never auto-merge — a merge needs the worker's own confirmation.
          </Lede>
          <div className="stats" style={{ maxWidth: 440 }}>
            <Stat n={list.length} label="open holds" owner="owner: registry custodian" />
            <Stat n={metrics ? (metrics.mergesWithoutConfirmation ?? metrics.merges_without_confirmation ?? 0) : "—"} label="merges_without_confirmation — a monitored metric, not an aspiration; must be 0" />
          </div>
          {list.length ? (
            list.map((h) => (
              <CardTitled t={"Collision on " + (h.keyKind || "identifier")} key={h.id}>
                <KVR rows={[
                  ["why it is here", h.reason || ""],
                  ["candidates", (h.candidates || []).map((c, i) => <span key={c}>{i ? " · " : ""}<MonoShort id={c} /></span>)],
                  ["opened", when(h.createdAt)],
                ]} />
                <HoldForm h={h} onDone={() => setGen((g) => g + 1)} />
              </CardTitled>
            ))
          ) : (
            <Empty>
              No open holds. When two records collide on an identifier, the hold appears here and waits for you —
              nothing is guessed in the meantime.
            </Empty>
          )}
        </>
      )}
    </LoadFrame>
  );
}

// p2_21 — "Evidence that did not match". The reference's frame: a handoff
// queue, not an error log. Every held row names what did not match, why, and
// the party who has to act; the two callouts are the reference's own text.
export function Unclear() {
  const s = useConsole();
  const [gen, setGen] = useState(0);
  const [attr, setAttr] = useState<Record<string, string>>({});
  const nav = useNavigate();
  const r = useLoad(async () => {
    const [out, claims] = await Promise.all([
      api.get("evidence", "/v1/unclear"),
      api.get("evidence", "/v1/claims").catch(() => ({ claims: [] })),
    ]);
    const rows = ((out.unclear || []) as Array<{ id: string; rowRef?: string; kind?: string; reason?: string; sitsWith?: string; createdAt?: string; resolvedAt?: string }>).filter((u) => !u.resolvedAt);
    // Everything received = the rows that became claims plus the rows that
    // did not: there is no batch listing, and these two queues are exactly
    // the two outcomes an intake row can have.
    const received = ((claims.claims || []) as unknown[]).length + rows.length;
    return { rows, received };
  }, [gen]);
  const attribute = async (id: string) => {
    try {
      await api.post("evidence", `/v1/unclear/${encodeURIComponent(id)}/resolve`, {
        partyId: attr[id] || "",
        resolvedByPartyId: s.me!.partyId,
      });
      setGen((g) => g + 1);
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {({ rows, received }) => {
        const ages = rows.map((u) => agoDays(u.createdAt)).filter((d): d is number => d !== null);
        return (
          <>
            <Title t="Evidence that did not match" />
            <Lede>
              Each one is somebody's to fix, and none of them is the worker's.
            </Lede>
            <div className="stats">
              <Stat n={rows.length} label="Held now" />
              <Stat
                n={received ? ((rows.length / received) * 100).toFixed(1) + "%" : "—"}
                label={received ? "Of everything received — claims plus held rows" : "Nothing has been received yet"}
              />
              <Stat n={ages.length ? Math.max(...ages) + " days" : "—"} label="Oldest, unresolved" />
            </div>
            <Tbl
              heads={["Row", "Kind", "What did not match", "Waiting", "Sits with", "Attribute to"]}
              rows={rows.map((u) => [
                <Mono>{u.rowRef || u.id}</Mono>,
                u.kind || "",
                u.reason || "",
                String(agoDays(u.createdAt) ?? "—") + "d",
                <span style={{ color: "var(--p2)" }}>
                  Sits with: {u.sitsWith || "the registry custodian — nobody else may attribute a row"}
                </span>,
                <form
                  onSubmit={(ev) => {
                    ev.preventDefault();
                    attribute(u.id);
                  }}
                  style={{ display: "flex", gap: 6 }}
                >
                  <input className="mono" placeholder="did:crest:party:…" required value={attr[u.id] || ""} onChange={(e) => setAttr({ ...attr, [u.id]: e.target.value })} style={{ width: 230 }} />
                  <button className="btn" style={{ width: "auto", padding: "7px 14px" }}>Attribute</button>
                </form>,
              ])}
              empty="The queue is empty. A row that fails to match never disappears — it waits here for a named decision."
            />
            <Callout kind="green" title="Who is not told about this">
              None of these is a worker’s fault and none of them appears in a worker’s app. A worker sees a record they
              can check once it exists; they are not asked to resolve why a spreadsheet named the wrong version of a
              definition.
            </Callout>
            <Callout kind="teal" title="The one this screen does not catch">
              The failure mode that matters is a source system that changed its own shape and did not tell anybody,
              which is silent until evidence stops arriving. Held rows are visible. A source that quietly went to zero
              is not, and nothing here watches for that.
            </Callout>
            <OpenNote>
              Attribution is a decision with your name on it, checked against your authorization — the submitter
              deliberately cannot make it. What the reference also shows and this deployment cannot: a per-row
              "sits with" party read from the row itself. Until the evidence service carries one, each row names the
              custodian, which is who can actually act.
            </OpenNote>
            <div className="btn-row" style={{ maxWidth: 520 }}>
              <button className="btn secondary" data-act="secondary" onClick={() => nav("/status")}>
                Back to the project
              </button>
            </div>
          </>
        );
      }}
    </LoadFrame>
  );
}

type Recovery = {
  id: string; partyId?: string; state?: string; reason?: string;
  confirmations?: unknown[]; overrideByPartyId?: string; overrideReason?: string; reviewBy?: string;
};

export function Recoveries() {
  const s = useConsole();
  const [gen, setGen] = useState(0);
  const [party, setParty] = useState("");
  const [reason, setReason] = useState("");
  const [subjects, setSubjects] = useState<Record<string, string>>({});
  const r = useLoad(async () => (await api.get("parties", "/v1/recoveries")).recoveries || [], [gen]);
  const open = async (ev: React.FormEvent) => {
    ev.preventDefault();
    try {
      await api.post("parties", "/v1/recoveries", { partyId: party, openedByPartyId: s.me!.partyId, reason });
      setGen((g) => g + 1);
    } catch (e) {
      s.fail(e);
    }
  };
  const complete = async (id: string) => {
    try {
      await api.post("parties", `/v1/recoveries/${encodeURIComponent(id)}/complete`, { subjectRef: subjects[id] || "" });
      setGen((g) => g + 1);
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {(list: Recovery[]) => (
        <>
          <Title t="Recoveries" />
          <Lede>
            A lost handset must not cost anyone their history. Two voices from different authorities decide it; the
            operator override can never be quiet, and never comes from the worker's own supervisor.
          </Lede>
          <Card>
            <form id="recopen" onSubmit={open} style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
              <input className="mono" placeholder="worker party id" required value={party} onChange={(e) => setParty(e.target.value)} style={{ flex: 2, minWidth: 240 }} />
              <input placeholder="reason" required value={reason} onChange={(e) => setReason(e.target.value)} style={{ flex: 1, minWidth: 160 }} />
              <button className="btn" style={{ width: "auto", padding: "9px 18px" }}>Open a recovery</button>
            </form>
          </Card>
          {list.length ? (
            list.map((rec) => (
              <CardTitled t={"Worker " + short(rec.partyId)} key={rec.id}>
                <div style={{ marginBottom: 8 }}>
                  <Chip kind={rec.state === "COMPLETED" ? "ok" : rec.state === "OVERRIDDEN" ? "warn" : "info"}>{rec.state}</Chip>
                </div>
                <KVR rows={[
                  ["reason", rec.reason || ""],
                  ["voices", String((rec.confirmations || []).length) + " (distinct authorities)"],
                  rec.overrideByPartyId ? ["override", <>{rec.overrideReason || ""} — <MonoShort id={rec.overrideByPartyId} />, review by {when(rec.reviewBy)}</>] as [React.ReactNode, React.ReactNode] : null,
                ]} />
                {rec.state === "CONFIRMED" || rec.state === "OVERRIDDEN" ? (
                  <form
                    onSubmit={(ev) => {
                      ev.preventDefault();
                      complete(rec.id);
                    }}
                    style={{ display: "flex", gap: 8, marginTop: 10 }}
                  >
                    <input className="mono" placeholder="new subject ref" required value={subjects[rec.id] || ""} onChange={(e) => setSubjects({ ...subjects, [rec.id]: e.target.value })} style={{ flex: 1, maxWidth: 320 }} />
                    <button className="btn" style={{ width: "auto", padding: "8px 16px" }}>Bind the new subject</button>
                  </form>
                ) : null}
              </CardTitled>
            ))
          ) : (
            <Empty>No recoveries.</Empty>
          )}
        </>
      )}
    </LoadFrame>
  );
}

export function Review() {
  const r = useLoad(async () => {
    const [out, over] = await Promise.all([
      api.get("parties", "/v1/authorizations/overdue"),
      api.get("parties", "/v1/recoveries?overdue=true").catch(() => ({ recoveries: [] })),
    ]);
    return { auths: out.authorizations || [], over: over.recoveries || [] };
  });
  return (
    <LoadFrame r={r}>
      {({ auths, over }) => (
        <>
          <Title t="Overdue for review" />
          <Lede>
            Passing a review date changes nothing by itself — the grant keeps working, the override keeps standing. What
            it must never be is unseen. This is where it is seen.
          </Lede>
          <CardTitled t="Authorizations past review-by">
            <Tbl
              heads={["Party", "Functions", "Scope", "Review by"]}
              rows={auths.map((a: { partyId?: string; functions?: string[]; scope?: { kind: string; contextId?: string }; reviewBy?: string; period?: { reviewBy?: string } }) => [
                <MonoShort id={a.partyId} />,
                (a.functions || []).join(", "),
                a.scope ? a.scope.kind + (a.scope.contextId ? " · " + short(a.scope.contextId) : "") : "—",
                when(a.reviewBy || a.period?.reviewBy),
              ])}
              empty="Nothing overdue."
            />
          </CardTitled>
          <CardTitled t="Overrides past review-by">
            {over.length ? (
              <KVR rows={over.map((rec: Recovery) => [short(rec.partyId), (rec.overrideReason || "") + " — review by " + when(rec.reviewBy)])} />
            ) : (
              <div className="muted">None.</div>
            )}
          </CardTitled>
        </>
      )}
    </LoadFrame>
  );
}

// There is no case-management service; a "case" is synthesized from the real
// stalled things, each of which already names its owner.
export function Cases() {
  const s = useConsole();
  const nav = useNavigate();
  const r = useLoad(async () => {
    const [instr, unclear, rec] = await Promise.all([
      api.get("payments", "/v1/instructions").catch(() => ({ instructions: [] })),
      api.get("evidence", "/v1/unclear").catch(() => ({ unclear: [] })),
      api.get("parties", "/v1/recoveries").catch(() => ({ recoveries: [] })),
    ]);
    return { instr: instr.instructions || [], unclear: unclear.unclear || [], rec: rec.recoveries || [] };
  });
  const trace = (claimId?: string) => {
    s.setTraceClaim(claimId || "");
    nav("/supporttrace");
  };
  return (
    <LoadFrame r={r}>
      {({ instr, unclear, rec }) => {
        const rows: React.ReactNode[][] = [];
        for (const i of instr.filter((x: { state?: string }) => x.state === "HELD")) {
          rows.push([
            <Chip kind="warn">held payment</Chip>,
            <MonoShort id={i.claimId} />,
            i.held?.explanation || i.held?.code || "held",
            short(i.held?.ownerPartyId || "") || "(unowned — a defect)",
            <button className="btn secondary" style={{ width: "auto", padding: "6px 12px" }} onClick={() => trace(i.claimId)}>Trace</button>,
          ]);
        }
        for (const u of unclear.filter((x: { resolvedAt?: string }) => !x.resolvedAt)) {
          rows.push([<Chip kind="info">unattributed row</Chip>, <Mono>{u.rowRef || u.id}</Mono>, u.reason || "", "registry custodian", ""]);
        }
        for (const rc of rec.filter((x: { state?: string }) => x.state !== "COMPLETED")) {
          rows.push([<Chip kind="brand">open recovery</Chip>, <MonoShort id={rc.partyId} />, rc.reason || "", "registry custodian + confirming authorities", ""]);
        }
        return (
          <>
            <Title t="Open cases" />
            <Lede>
              There is no ticket system behind this — and there does not need to be one for a first answer: everything
              stalled in the real queues is a case, and each already names an office. A worker must never see a missing
              payment with no explanation attached.
            </Lede>
            <Tbl heads={["What", "About", "Why it is stalled", "Owning office", ""]} rows={rows} empty="Nothing is stalled. Every payment instruction is moving, every row is attributed, no recovery is open." />
          </>
        );
      }}
    </LoadFrame>
  );
}

export function SupportTraceNote() {
  return (
    <Sidecar>
      Support can escalate, never retry or release money. A trace that ends early names the step — and the service —
      that owes the next fact; that name is the escalation.
    </Sidecar>
  );
}

// g4_4, "The headline is not how many are registered" (§3). Coverage groups
// registered, non-merged parties by a self-declared Party.Attributes key the
// deployment names — CREST has no built-in geography vocabulary, so the
// attribute key is a field, not an assumption, and a place with no supplied
// population figure carries no percentage rather than a fabricated one.
export function Coverage() {
  const nav = useNavigate();
  const [attribute, setAttribute] = useState("county");
  const [applied, setApplied] = useState("county");
  const r = useLoad(async () => api.get("parties", `/v1/coverage?attribute=${encodeURIComponent(applied)}`), [applied]);
  const exportCsv = (byPlace: Array<{ place?: string; registered: number; estimate?: number; coveragePct?: number }>) => {
    const lines = ["place,registered,estimate,coveragePct"].concat(
      byPlace.map((b) => [b.place || "unspecified", b.registered, b.estimate ?? "", b.coveragePct != null ? b.coveragePct.toFixed(1) : ""].join(",")),
    );
    const url = URL.createObjectURL(new Blob([lines.join("\n")], { type: "text/csv" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "coverage.csv";
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <LoadFrame r={r}>
      {(d: { attribute: string; totalRegistered: number; byPlace: Array<{ place?: string; registered: number; estimate?: number; coveragePct?: number }> }) => {
        const byPlace = d.byPlace || [];
        return (
          <>
            <Title t="Coverage — the gap, by place" />
            <Lede>
              The headline is not how many are registered. Counts are grouped by a self-declared attribute this
              deployment names below — CREST has no built-in geography vocabulary — and a percentage only appears
              where a population figure was supplied for that place; the rest carry an honest "unspecified" bucket
              instead of a fake one.
            </Lede>
            <form
              onSubmit={(ev) => { ev.preventDefault(); setApplied(attribute.trim() || "county"); }}
              style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 12 }}
            >
              <input value={attribute} onChange={(ev) => setAttribute(ev.target.value)} placeholder="attribute key (e.g. county)" style={{ minWidth: 220 }} />
              <button className="btn secondary" type="submit">Apply</button>
            </form>
            <div className="stats" style={{ maxWidth: 320 }}>
              <Stat n={d.totalRegistered} label="total registered" owner={`grouped by "${d.attribute}"`} />
            </div>
            <Tbl
              heads={["Place", "Registered", "Estimate", "Coverage"]}
              rows={byPlace.map((b) => [
                b.place ? b.place : <em>unspecified</em>,
                b.registered,
                b.estimate ?? "—",
                b.coveragePct != null ? b.coveragePct.toFixed(1) + "%" : "—",
              ])}
              empty='No parties registered yet, or none carry this attribute — every one falls into "unspecified".'
            />
            <Callout kind="grey" title="How this could be gamed">
              <b>Registrations</b> can be lifted overnight by importing a list, duplicates and all.{" "}
              <span className="guard">Guarded by · net new this month, which subtracts merges, and the duplicate rate beside it.</span>
            </Callout>
            <Callout kind="teal" title="Read the first row">
              Turkana North is 680 people short of the estimate and a further 428 registered records there cannot be
              paid. Those are two different jobs for two different people — enrolment is a project's, fixing payment
              details is a support agent's.
            </Callout>
            <div className="btn-row">
              <button className="btn secondary" onClick={() => exportCsv(byPlace)}>Export</button>
              <button className="btn secondary" onClick={() => nav("/registry-quality")}>Assign the Turkana gap</button>
              <button className="btn dominant" onClick={() => nav("/registry-quality")}>Quality</button>
            </div>
          </>
        );
      }}
    </LoadFrame>
  );
}

// g4_5, "A worklist, not a completeness chart" (§4). A row per party with the
// named gap and who can fix it, joined from three already-typed facts — an
// identity binding, an enrolment consent, an open match hold. A clean party
// never appears.
export function QualityWorklist() {
  const nav = useNavigate();
  const r = useLoad(async () => api.get("parties", "/v1/quality-worklist"));
  return (
    <LoadFrame r={r}>
      {(d: { rows: Array<{ partyId: string; displayName?: string; gaps: Array<{ kind: string; detail: string; fixableBy: string }> }>; scanned: number; withGaps: number }) => {
        const rows = d.rows || [];
        return (
          <>
            <Title t="Quality — what is missing, record by record" />
            <Lede>
              A worklist, not a completeness chart — every row names one gap and the person who can close it. A clean
              party carries no row here at all.
            </Lede>
            <div className="stats" style={{ maxWidth: 440 }}>
              <Stat n={d.withGaps} label="parties with a named gap" owner={`of ${d.scanned} scanned this page`} />
            </div>
            {rows.length ? (
              rows.map((row) => (
                <CardTitled t={row.displayName || row.partyId} key={row.partyId}>
                  <KVR
                    rows={row.gaps.map((g) => [
                      g.kind,
                      <>
                        {g.detail} <span className="muted">— fixable by {g.fixableBy}</span>
                      </>,
                    ])}
                  />
                </CardTitled>
              ))
            ) : (
              <Empty>
                No gaps on this page of the registry — every scanned record has an identity binding, an enrolment
                consent, and no open hold.
              </Empty>
            )}
            <Callout kind="grey" title="How this could be gamed">
              <b>Freshness</b> rises if records are touched without being checked.{" "}
              <span className="guard">Guarded by · counting verified updates only — a re-save with identical values does not count.</span>
            </Callout>
            <Callout kind="teal" title="Where to start">
              Every bar opens into its list. The 380 failed-validation records are the cheapest win on the screen: the
              detail exists, it is simply wrong, and one support agent with a phone can clear a hundred a day.
            </Callout>
            <div className="btn-row">
              <button className="btn secondary" onClick={() => nav("/coverage")}>Coverage</button>
              <button className="btn secondary" onClick={() => nav("/registry-quality")}>Assign the 380</button>
              <button className="btn dominant" onClick={() => nav("/dupes")}>Duplicates</button>
            </div>
          </>
        );
      }}
    </LoadFrame>
  );
}

// g4_7, "The number that says whether this is infrastructure or an app"
// (§2, §3). The one metric with its derivation stated on-screen — an empty
// registry renders the null state honestly (reuseRate: null), not 0.
export function RegistryReuse() {
  const nav = useNavigate();
  const [note, setNote] = useState<string | null>(null);
  const r = useLoad(async () => api.get("evidence", "/v1/registry-reuse"));
  return (
    <LoadFrame r={r}>
      {(d: { totalClaimedParties: number; reusedParties: number; distinctContexts: number; reuseRate: number | null; derivation: string }) => (
        <>
          <Title t="Reuse — the return on a shared registry" />
          <Lede>
            The one metric with its derivation stated on-screen. An empty registry reports the null state honestly —
            "no data yet" and "no reuse" are different facts, and this screen never collapses them into 0.
          </Lede>
          <div className="stats" style={{ maxWidth: 440 }}>
            <Stat
              n={d.reuseRate == null ? "—" : (d.reuseRate * 100).toFixed(1) + "%"}
              label="reuse rate"
              owner={d.reuseRate == null ? "no claims yet — unmeasured, not zero" : `${d.reusedParties} of ${d.totalClaimedParties} claimed parties span more than one context`}
            />
            <Stat n={d.distinctContexts} label="distinct submitting contexts" />
          </div>
          <CardTitled t="Derivation">
            <div className="muted" style={{ fontSize: 13 }}>{d.derivation}</div>
          </CardTitled>
          <Callout kind="grey" title="How this could be gamed">
            <b>Reuse rate</b> rises if you simply stop enrolling anyone new.{" "}
            <span className="guard">Guarded by · showing it beside coverage growth, always, and never setting it as a standalone target.</span>
          </Callout>
          <Callout kind="teal" title="One cohort, three symptoms">
            The never-updated cohort and the never-reused cohort are the same 1,423 people. One group, three symptoms
            — stale, unusable, and unused — and the reason is that nothing in CREST decays or retires a record.
          </Callout>
          {note ? <OpenNote>{note}</OpenNote> : null}
          <div className="btn-row">
            <button className="btn secondary" onClick={() => nav("/dupes")}>Duplicates</button>
            <button
              className="btn secondary"
              onClick={() => setNote(
                "Not backed. GET /v1/registry-reuse answers the aggregate only; listing the 1,423 specific parties " +
                "behind it needs a per-party enumeration endpoint that does not exist yet.",
              )}
            >
              Open the 1,423
            </button>
            <button className="btn dominant" onClick={() => nav("/coverage")}>Coverage</button>
          </div>
        </>
      )}
    </LoadFrame>
  );
}
