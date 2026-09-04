// Payment set up (J5), Organisation view (J1), Instance view (J2) and the
// Funder portfolio (J11), ported 1:1 from apps/console/views-admin.js. Real
// data where a service answers, the reference's own honesty labels where none
// does.
//
// Define work (J4) used to live here as a read-only walkthrough of the seeded
// definition, labelled "authoring writes are not built". They are now: the
// real P-3 wizard is views/Define.tsx, which writes drafts and submits
// versions, so the read-only stand-in has been removed rather than left
// alongside as a second screen answering the same question differently.
import { useState } from "react";
import { api, FIX } from "@crest/api";
import { Callout, Chip, OpenNote, NextBlock } from "@crest/ui";
import { useConsole } from "../state";
import {
  short, money, when, Mono, MonoShort, KVR, Stat, Title, Lede, Empty, Tbl,
  CardTitled, ILLUSTRATIVE, SIMULATED, useLoad, LoadFrame,
} from "../ui";

type LinkedRecord = {
  id: string;
  type: string;
  version: number;
  state: string;
  payload?: { ratePerOutcomeUnit: { amountMinor: number; currency: string }; payerPartyId: string; effectiveFrom?: string };
};

async function loadDefinition(defId: string) {
  const [d, worker, lr] = await Promise.all([
    api.get("definitions", `/v1/definitions/${encodeURIComponent(defId)}`),
    api.get("definitions", `/v1/definitions/${encodeURIComponent(defId)}/faces/worker`).catch(() => null),
    api.get("definitions", `/v1/definitions/${encodeURIComponent(defId)}/linked-records`).catch(() => ({ linkedRecords: [] })),
  ]);
  return { d, worker, lr: (lr.linkedRecords || []) as LinkedRecord[] };
}

export function PaySetup() {
  const s = useConsole();
  const r = useLoad<Awaited<ReturnType<typeof loadDefinition>>>(
    () => (s.definitionId ? loadDefinition(s.definitionId) : new Promise(() => {})),
    [s.definitionId],
  );
  if (!s.definitionId) {
    return (
      <>
        <Title t="Payment set up" />
        <Empty>Payment attaches to a work definition, and none exists yet.</Empty>
      </>
    );
  }
  return (
    <LoadFrame r={r}>
      {({ d, worker, lr }) => {
        const wkr = (worker && worker.worker) || {};
        const pays = lr.find((x) => x.type === "payment-setup");
        return (
          <>
            <Title t="Payment set up" />
            <Lede>
              Payment set up is two separate halves: how much, and how it reaches the worker. They can be one person
              or two, and neither blocks the credential — work already done is already validated. The rate is a
              versioned linked record keyed to the definition; an old version is never overwritten.
            </Lede>
            {pays ? (
              <CardTitled t="The rate (f1_3 / f1_4)">
                <KVR rows={[
                  ["rate per " + (d.outcomeUnit || "unit"), <span className="big-stat">{money(pays.payload!.ratePerOutcomeUnit.amountMinor, pays.payload!.ratePerOutcomeUnit.currency)}</span>],
                  ["payer", <MonoShort id={pays.payload!.payerPartyId} />],
                  ["effective from", when(pays.payload!.effectiveFrom)],
                  ["record", <><Mono>{pays.id}</Mono> · v{pays.version} · {pays.state}</>],
                ]} />
              </CardTitled>
            ) : (
              <Empty>No payment-setup record exists for this definition.</Empty>
            )}
            <CardTitled t="What the worker will see">
              <div className="consent-quote">
                {wkr.summary || ""}
                <br />
                <br />
                {pays ? (
                  <strong>
                    One {d.outcomeUnit || "unit"} pays {money(pays.payload!.ratePerOutcomeUnit.amountMinor, pays.payload!.ratePerOutcomeUnit.currency)}
                    {" "}— set separately from the definition, versioned, never overwritten.
                  </strong>
                ) : (
                  "No rate is attached — this work is recognised, and recognition is a use of its own."
                )}
              </div>
              <p className="muted" style={{ marginTop: 8 }}>
                Rendered from the definition's worker face — the same read the worker's own app makes.
              </p>
            </CardTitled>
            <CardTitled t="Payment rail (f2)">
              <div style={{ display: "flex", gap: 8, marginBottom: 10 }}>
                {ILLUSTRATIVE}
                {SIMULATED}
              </div>
              <KVR rows={[
                ["rail", "mobile-money · MockPay sandbox"],
                ["connection test", <Chip kind="ok">Simulated: 200 OK · 412ms</Chip>],
                ["settlement report", "daily, by instruction id"],
              ]} />
              <p className="muted" style={{ marginTop: 8 }}>
                A rail today is a URL in deployment configuration — the payments service raises instructions and records
                their release; carrying them to a real rail is deployment wiring, not an API this console can call.
                These screens are drawn with the reference's own labels.
              </p>
            </CardTitled>
            <NextBlock
              happened="Nothing — this page reads the rate record and the worker face; it changes nothing."
              who="The payments service, per instruction, the moment a confirmation window exits."
              when="Immediately at exit — all four exits release, a dispute included."
              told="The instruction appears in Payments with its state, and a held one names its owner."
              ifnot="if an instruction does not appear after an exit, the gap is the payments service's to explain — trace the claim."
            />
          </>
        );
      }}
    </LoadFrame>
  );
}

// There is no GET list of authorizations by party, so grants are shown as
// live permits() answers for the functions this deployment's terms name.
export function Org() {
  // The signed-in party, not the fixture organisation: a real eSignet session
  // must see its own standing record. Fixture sessions bind me.partyId to
  // FIX.org anyway, so the seeded world reads identically.
  const { me, projectId } = useConsole();
  const orgId = me?.partyId || FIX.org;
  const r = useLoad(async () => {
    const [org, overdue] = await Promise.all([
      api.get("parties", `/v1/parties/${encodeURIComponent(orgId)}`),
      api.get("parties", "/v1/authorizations/overdue").catch(() => ({ authorizations: [] })),
    ]);
    const checks: Array<[string, string | null, string]> = [
      ["attest-work", null, "may this organisation attest work at all (instance-wide)"],
      ...(projectId
        ? ([
            ["attest-work", projectId, "…and specifically on " + short(projectId)],
            ["submit-work-evidence", projectId, "may it submit evidence on " + short(projectId)],
          ] as Array<[string, string | null, string]>)
        : []),
    ];
    const permits = await Promise.all(
      checks.map(([fn, ctx]) => {
        const q = new URLSearchParams({ partyId: orgId, function: fn });
        if (ctx) q.set("contextId", ctx);
        return api.get("parties", "/v1/authorizations/permits?" + q).catch(() => null);
      }),
    );
    return { org, overdue, checks, permits };
  });
  return (
    <LoadFrame r={r}>
      {({ org, overdue, checks, permits }) => {
        const p = org.party || org;
        return (
          <>
            <Title t={"Organisation — " + (p.displayName || "…")} />
            <CardTitled t="Profile">
              <KVR rows={[
                ["party", <Mono>{p.id || orgId}</Mono>],
                ["kind", p.kind || ""],
                ["contact routes", (p.contactRoutes || []).length ? (p.contactRoutes as Array<{ kind: string; value?: string }>).map((rt, i) => <span key={i}>{i ? " · " : ""}{rt.kind} <Mono>{rt.value || ""}</Mono></span>) : "—"],
                ["registered", when(p.createdAt)],
              ]} />
            </CardTitled>
            <CardTitled t="Authorizations held — live permits() answers">
              <Tbl
                heads={["Question", "Scope", "Answer", "Overdue for review?"]}
                rows={checks.map(([fn, ctx, label], i) => [
                  label + " (" + fn + ")",
                  ctx ? <MonoShort id={ctx} /> : "instance",
                  permits[i] ? <Chip kind={permits[i].permitted ? "ok" : "err"}>{permits[i].permitted ? "permitted" : "not permitted"}</Chip> : <Chip kind="plain">no answer</Chip>,
                  permits[i] && permits[i].overdue ? <Chip kind="warn">past review-by — still working, by design</Chip> : "—",
                ])}
              />
              <p className="muted" style={{ marginTop: 8 }}>
                The parties service deliberately has no list-my-grants endpoint — only <span className="mono">permits()</span>{" "}
                and the overdue queue — so this table asks the real question the services ask. Overdue never changes the
                answer: flag overdue, keep working.
              </p>
            </CardTitled>
            <CardTitled t="Grants past their review-by date">
              <Tbl
                heads={["Held by", "Functions", "Scope", "Review by", ""]}
                rows={(overdue.authorizations || []).map((a: { partyId?: string; functions?: string[]; scope?: { kind: string; contextId?: string }; reviewBy?: string; period?: { reviewBy?: string } }) => [
                  <MonoShort id={a.partyId} />,
                  (a.functions || []).join(", "),
                  a.scope ? a.scope.kind + (a.scope.contextId ? " · " + short(a.scope.contextId) : "") : "—",
                  when(a.reviewBy || a.period?.reviewBy),
                  <Chip kind="warn">overdue</Chip>,
                ])}
                empty="Nothing is overdue. Passing a review date changes nothing by itself — what it must never be is unseen."
              />
            </CardTitled>
            <CardTitled t="Invitations and terms (g2_6–g2_13)">
              <div style={{ marginBottom: 10 }}>{ILLUSTRATIVE}</div>
              <KVR rows={[
                ["invite an organisation", "an email carrying the terms version to agree to"],
                ["terms on file", <><Mono>crest:terms:01JCREST00000000000000TERM</Mono> v1 — real, seeded via POST /v1/terms</>],
                ["agreement flow", "the invitee's decision recorded against that exact version"],
              ]} />
              <OpenNote>
                There is no terms-catalogue or invitation service; terms exist as versioned records the parties service
                stores, and the invitation flow is drawn here with the reference's own label rather than pretended.
              </OpenNote>
            </CardTitled>
          </>
        );
      }}
    </LoadFrame>
  );
}

export function Instance() {
  const r = useLoad(async () => {
    const issuer = await api.get("verification", "/v1/issuer").catch(() => null);
    const instAnswer = await api.get("parties", "/v1/instance").catch(() => null);
    const names = ["parties", "definitions", "evidence", "confirmation", "verification", "payments"] as const;
    const health = await Promise.all(
      names.map((n) =>
        api.get(n, "/healthz").then(
          () => ({ n, ok: true }),
          () => ({ n, ok: false }),
        ),
      ),
    );
    return { issuer, inst: (instAnswer || {}).instance || null, health, count: names.length };
  });
  return (
    <LoadFrame r={r}>
      {({ issuer, inst, health, count }) => {
        const reg = (inst || {}).registry || {};
        return (
          <>
            <Title t="Instance — the deployment itself" />
            <CardTitled t="Instance facts">
              {inst ? (
                <KVR rows={[
                  ["instance", <Mono>{inst.instanceId || "—"}</Mono>],
                  ["name", inst.name || "—"],
                  ["operator", inst.operatorPartyId ? <Mono>{inst.operatorPartyId}</Mono> : "not configured"],
                  ["issuer (per the instance)", inst.issuerId ? <Mono>{inst.issuerId}</Mono> : "not configured"],
                  ["registry", reg.url ? <><Mono>{reg.url}</Mono>{reg.namespace ? <> · <Mono>{reg.namespace}</Mono></> : null}</> : "Postgres fallback — no external registry"],
                  ["transparency log", reg.transparent ? "yes — an append-only log a reader can watch" : "no — answers rest on this deployment's word"],
                ]} />
              ) : (
                <KVR rows={[["instance", "the parties service did not answer /v1/instance — a deployment that has not been told who it is answers 503 here"]]} />
              )}
              <KVR rows={[
                ["issuer (per the verification service)", issuer ? <Mono>{issuer.id || issuer.issuer || JSON.stringify(issuer).slice(0, 60)}</Mono> : "the verification service did not answer /v1/issuer"],
                ["services", String(count) + " CREST services behind this console"],
              ]} />
              <p className="muted" style={{ marginTop: 8 }}>
                GET /v1/instance is the deployment's public self-description (#70) — every field is configuration or
                derived from it, read live rather than stored, which is exactly where the layering test puts it.
              </p>
            </CardTitled>
            <CardTitled t="The services behind all of it — live health sweep">
              <div className="stats" style={{ flexWrap: "wrap" }}>
                {health.map((x) => (
                  <div className="stat" style={{ minWidth: 150 }} key={x.n}>
                    <div className="n" style={{ fontSize: 18 }}>
                      {x.ok ? <Chip kind="ok">healthy</Chip> : <Chip kind="err">unreachable</Chip>}
                    </div>
                    <div className="l">
                      {x.n}
                      <br />
                      <span className="mono">/healthz</span>
                    </div>
                  </div>
                ))}
              </div>
            </CardTitled>
            <CardTitled t="Consent floor">
              <KVR rows={[
                ["the floor", "enrolment consent is captured per programme; withdrawing stops new evidence collection and never touches what was already paid"],
                ["message templates", "deployment configuration, per the #59 decision — two deployments wording the ask differently are both CREST"],
              ]} />
              <div style={{ marginTop: 10 }}>
                <OpenNote>
                  Editing consent scripts and message templates from this screen is not built; templates are deployment
                  config (#59). What is real: every captured consent is a record with an artefact the worker can hear
                  back.
                </OpenNote>
              </div>
            </CardTitled>
            <CardTitled t="Admission queue (g4_1–g4_3)">
              <p className="body-2">
                The queue is real now: <span className="mono">GET /v1/registrations</span> lists every application a
                person still has to look at, and the decision goes through{" "}
                <span className="mono">POST /v1/organisations/{"{id}"}/decision</span> with the authenticated caller
                as decider. Open <span className="mono">#/admissions</span> under the G-1 rail's Organisations entry.
              </p>
            </CardTitled>
          </>
        );
      }}
    </LoadFrame>
  );
}

// v4_1 / v4_2 — the funder's portfolio and the one-project drill beneath it.
// One project exists in this deployment, so the portfolio is one row; the
// drill (v4_2) opens under it on the same route, grouping the project's real
// claims by definition and its real instructions by state. Nothing on either
// frame names a worker — the reference's point, and the read's.
type PClaim = { id?: string; partyId?: string; unitId?: string; state?: string };
type PUnit = { id: string; geography?: string; definition?: { id?: string; version?: number }; outcome?: { unit?: string } };
type PInstr = { claimId?: string; amountMinor?: number; currency?: string; state?: string; held?: { ownerPartyId?: string } };
type PDef = { activity?: { label?: string }; outcomeUnit?: string } | null;

// The drill's two tables come from the units behind the project's claims —
// a unit carries its geography and its definition, a claim only its unit.
// Bounded so a large project cannot fan out into thousands of reads.
const DRILL_UNIT_CAP = 200;

export function Portfolio() {
  const s = useConsole();
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const r = useLoad(async () => {
    const [claims, instr] = await Promise.all([
      api.get("evidence", "/v1/claims").catch(() => ({ claims: [] })),
      api.get("payments", "/v1/instructions").catch(() => ({ instructions: [] })),
    ]);
    const cl = (claims.claims || []) as PClaim[];
    const unitIds = Array.from(new Set(cl.map((c) => c.unitId).filter((x): x is string => !!x))).slice(0, DRILL_UNIT_CAP);
    const units = await Promise.all(unitIds.map((id) =>
      api.get("evidence", `/v1/units/${encodeURIComponent(id)}`).then((u) => (u.unit || u) as PUnit).catch(() => null)));
    const unitById: Record<string, PUnit> = {};
    for (const u of units) if (u) unitById[u.id] = u;
    const defIds = Array.from(new Set(Object.values(unitById).map((u) => u.definition?.id).filter((x): x is string => !!x)));
    const defs = await Promise.all(defIds.map((id) =>
      api.get("definitions", `/v1/definitions/${encodeURIComponent(id)}`).then((d) => [id, (d.definition || d) as PDef] as const).catch(() => [id, null] as const)));
    return {
      claims: cl,
      list: (instr.instructions || []) as PInstr[],
      unitById,
      truncated: cl.length > DRILL_UNIT_CAP,
      defs: Object.fromEntries(defs) as Record<string, PDef>,
    };
  });
  return (
    <LoadFrame r={r}>
      {({ claims, list, unitById, truncated, defs }) => {
        const sum = (xs: PInstr[]) => xs.reduce((t, i) => t + (i.amountMinor || 0), 0);
        const paidList = list.filter((i) => i.state === "SETTLED");
        const inFlight = list.filter((i) => i.state === "RELEASED");
        const held = list.filter((i) => i.state === "HELD");
        const stuck = held.filter((i) => !(i.held?.ownerPartyId || "").trim());
        const unpaid = [...inFlight, ...held];
        const cur = (list[0] || {}).currency || "";
        const project = s.projectId ? short(s.projectId) : "this project";
        const unitOf = (c: PClaim) => (c.unitId ? unitById[c.unitId] : undefined);
        const groupBy = (key: (c: PClaim) => string) =>
          claims.reduce<Record<string, PClaim[]>>((m, c) => {
            const k = key(c);
            (m[k] = m[k] || []).push(c);
            return m;
          }, {});
        const byDef = groupBy((c) => unitOf(c)?.definition?.id || "unknown");
        const byPlace = groupBy((c) => unitOf(c)?.geography || "unspecified");
        const instrByClaim = new Map(list.map((i) => [i.claimId, i]));
        const moneyOf = (cs: PClaim[], pick: (i: PInstr) => boolean) =>
          money(sum(cs.map((c) => instrByClaim.get(c.id)).filter((i): i is PInstr => !!i && pick(i))), cur);
        const statusOf = (cs: PClaim[]) => {
          const st = cs.reduce<Record<string, number>>((m, c) => {
            const i = instrByClaim.get(c.id);
            const k = i ? (i.state === "SETTLED" ? "paid" : i.state === "HELD" ? "held" : "unpaid") : "no instruction yet";
            m[k] = (m[k] || 0) + 1;
            return m;
          }, {});
          return Object.entries(st).map(([k, n]) => k + " · " + n).join(" · ");
        };
        const exportCsv = () => {
          const lines = ["project,claims,instructions,paid,unpaid,stuck", [project, claims.length, list.length, sum(paidList), sum(unpaid), stuck.length].join(",")];
          const url = URL.createObjectURL(new Blob([lines.join("\n")], { type: "text/csv" }));
          const a = document.createElement("a");
          a.href = url;
          a.download = "portfolio.csv";
          a.click();
          URL.revokeObjectURL(url);
        };
        return (
          <>
            <Title t="Portfolio" />
            <Lede>
              One project exists in this deployment; one row. Every count is live; the allocation column is not, and
              says so.
            </Lede>
            <div className="stats">
              <Stat n="—" label="allocated" owner="no funding ledger exists; not invented" />
              <Stat n={claims.length} label="validated work" owner="claims on record" />
              <Stat n={money(sum(paidList), cur)} label="confirmed paid" owner={paidList.length + " instruction(s) settled"} />
              <Stat n={money(sum(unpaid), cur)} label="validated, unpaid" owner="the number that matters" />
            </div>
            <Tbl
              heads={["Project", "Allocated", "Verified", "Paid", "Unpaid", "Stuck", ""]}
              rows={[[
                <>{project} · {s.definitionId && defs[s.definitionId]?.activity?.label ? defs[s.definitionId]!.activity!.label : "this deployment's project"}</>,
                ILLUSTRATIVE,
                String(claims.length),
                money(sum(paidList), cur),
                money(sum(unpaid), cur),
                String(stuck.length),
                <button className="btn secondary" data-act="open-project" style={{ width: "auto", padding: "7px 12px" }} onClick={() => setOpen(true)}>
                  Open
                </button>,
              ]]}
            />
            <Callout kind="teal" title="Not the largest number, the worst ratio">
              Ranked by unpaid, the biggest project looks worst. Ranked by the proportion of its records that are
              stuck, a smaller one can be the worst project in the portfolio — and that is the read a funder needs.
              The Stuck column counts held payments nobody is holding, so the ratio is there to rank by once a second
              project exists.
            </Callout>
            <OpenNote>
              Allocated-vs-paid needs a funding ledger no service holds; the paid column is real (summed settled
              instructions), the allocation is not shown rather than invented.
            </OpenNote>
            {open ? null : (
              <div className="btn-row">
                <button className="btn secondary" onClick={exportCsv}>Export</button>
                <button className="btn dominant" data-act="open-project" onClick={() => setOpen(true)}>Open {project}</button>
              </div>
            )}
            {open ? (
              <>
                <Title t={project + " — where the work happened, and what was delivered"} />
                <Lede>
                  {money(sum(list), cur)} instructed · {money(sum(paidList), cur)} paid · {money(sum(unpaid), cur)} validated
                  and unpaid · {stuck.length} stuck.
                </Lede>
                <CardTitled t="Where the work happened">
                  <Tbl
                    heads={["Place", "Workers", "Validated records", "Paid", "Unpaid"]}
                    rows={Object.entries(byPlace)
                      .sort((a, b) => b[1].length - a[1].length)
                      .map(([place, cs]) => [
                        place === "unspecified" ? <em>unspecified</em> : place,
                        String(new Set(cs.map((c) => c.partyId)).size),
                        String(cs.length),
                        moneyOf(cs, (i) => i.state === "SETTLED"),
                        moneyOf(cs, (i) => i.state !== "SETTLED"),
                      ])}
                    empty="No claim has been recorded against this project yet."
                  />
                  <p className="muted" style={{ marginTop: 8 }}>
                    The place is the geography each work record arrived with — the source's own word, not a registry
                    attribute.{truncated ? " Only the first " + DRILL_UNIT_CAP + " records are grouped here." : ""}
                  </p>
                </CardTitled>
                <CardTitled t={"What was delivered, on " + project}>
                  <Tbl
                    heads={["Work definition", "Unit", "Records", "Tier", "Status"]}
                    rows={Object.entries(byDef).map(([defId, cs]) => {
                      const d = defs[defId];
                      return [
                        d?.activity?.label || (defId === "unknown" ? <em>unit not readable</em> : short(defId)),
                        unitOf(cs[0])?.outcome?.unit || d?.outcomeUnit || "—",
                        String(cs.length),
                        "—",
                        statusOf(cs),
                      ];
                    })}
                    empty="No claim has been recorded against this project yet."
                  />
                </CardTitled>
                <Callout kind="teal" title="Follow the tier column">
                  A Tier 2 definition routes every record to a validator. When a project names one validator for many
                  places, the money is not blocked by a payment fault — it is blocked by a staffing decision made at
                  setup. The tier column here reads "—" because no per-definition tier read exists yet; the Status
                  column is real.
                </Callout>
                <Callout kind="green" title="What is deliberately absent">
                  Nothing on this screen names a worker, and nothing needs to. Geography, service and date answer the
                  assurance question on their own.
                </Callout>
                {note ? <OpenNote>{note}</OpenNote> : null}
                <NextBlock
                  happened="You reviewed the portfolio"
                  who="The projects you raised queries against"
                  when="Projects are expected to answer within 5 working days"
                  told="Their answer appears against your query"
                  ifnot="An unanswered query is not escalated anywhere. You would have to contact the project directly."
                />
                <div className="btn-row">
                  <button className="btn secondary" onClick={() => setOpen(false)}>Back</button>
                  <button
                    className="btn dominant"
                    data-act="query"
                    onClick={() => setNote("Not backed. An assurance query — a funder's question recorded against a project, with its answer — has no object and no endpoint yet.")}
                  >
                    Raise an assurance query
                  </button>
                  <button
                    className="btn secondary"
                    data-act="individual"
                    onClick={() => setNote("Deliberately absent from this view. Individual records name a worker; the project's own Proof screen (#/trace) reads one claim's trail, and anything wider needs the worker's consent through the disclosure flow.")}
                  >
                    Individual records
                  </button>
                </div>
              </>
            ) : null}
          </>
        );
      }}
    </LoadFrame>
  );
}
