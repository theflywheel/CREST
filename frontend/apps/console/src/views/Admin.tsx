// Define work (J4), Payment set up (J5), Organisation view (J1), Instance
// view (J2) and the Funder portfolio (J11), ported 1:1 from
// apps/console/views-admin.js. Real data where a service answers, the
// reference's own honesty labels where none does.
import { api, FIX } from "@crest/api";
import { Chip, OpenNote, NextBlock } from "@crest/ui";
import { useNavigate } from "react-router-dom";
import {
  short, money, when, Mono, MonoShort, KVR, Title, Lede, Empty, Tbl,
  CardTitled, TierChip, ILLUSTRATIVE, SIMULATED, useLoad, LoadFrame,
} from "../ui";
import { useConsole } from "../state";
import type { ReactNode } from "react";

type LinkedRecord = {
  id: string;
  type: string;
  version: number;
  state: string;
  payload?: { ratePerOutcomeUnit: { amountMinor: number; currency: string }; payerPartyId: string; effectiveFrom?: string };
};

async function loadDefinition() {
  const [d, worker, lr] = await Promise.all([
    api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}`),
    api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}/faces/worker`).catch(() => null),
    api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}/linked-records`).catch(() => ({ linkedRecords: [] })),
  ]);
  return { d, worker, lr: (lr.linkedRecords || []) as LinkedRecord[] };
}

// The registry has no GET /v1/definitions list (POST only), so the registry
// screen shows the one definition the fixture world seeds, read for real.
export function DefineWork() {
  const s = useConsole();
  const r = useLoad(loadDefinition);
  return (
    <LoadFrame r={r}>
      {({ d, worker, lr }) => {
        const wkr = (worker && worker.worker) || (d.faces && d.faces.worker) || {};
        const plat = (d.faces && d.faces.platform) || {};
        const pays = lr.find((x) => x.type === "payment-setup");
        const tierRows = (d.tierMap || []).map(
          (rule: { tier: number; sourceClassIn?: string[]; captureMethodIn?: string[]; minIdentityAssurance?: string; requiresFields?: string[] }) =>
            [
              "Tier " + rule.tier,
              <>
                <TierChip t={rule.tier} /> source in {(rule.sourceClassIn || []).join(" / ")}; captured by{" "}
                {(rule.captureMethodIn || []).join(" / ")}; identity ≥ {rule.minIdentityAssurance || "any"}
                {(rule.requiresFields || []).length ? "; needs " + rule.requiresFields!.join(", ") : ""}
              </>,
            ] as [ReactNode, ReactNode],
        );
        const steps: Array<[string, ReactNode]> = [
          ["Scope", <>
            <KVR rows={[
              ["activity", <>{d.activity?.label || ""} <Mono>{d.activity?.code || ""}</Mono></>],
              ["skill code", <Mono>{d.skillCode || ""}</Mono>],
              ["project", <Mono>{FIX.project}</Mono>],
            ]} />
            <p className="muted" style={{ marginTop: 8 }}>The skill code is the part of the record that travels; the activity code is this deployment's own word for it.</p>
          </>],
          ["Counting basis", <>
            <KVR rows={[["one unit is", d.outcomeUnit || ""]]} />
            <p className="muted" style={{ marginTop: 8 }}>Everything downstream — the rate, the credential, the funnel — counts in this unit.</p>
          </>],
          ["What is counted", <>
            <KVR rows={[
              ["required fields", (plat.requiredFields || []).length ? (plat.requiredFields as string[]).map((f, i) => <span key={f}>{i ? " · " : ""}<Mono>{f}</Mono></span>) : "—"],
              ["record schema", <Mono>{plat.schemaRef || ""}</Mono>],
            ]} />
            <p className="muted" style={{ marginTop: 8 }}>A record providing only the mandatory core is still valid Tier-1-capable evidence; these fields only decide how strong.</p>
          </>],
          ["Parties", <KVR rows={[
            ["authored by", <MonoShort id={d.authoredByPartyId || ""} />],
            ["ratified by", <><MonoShort id={d.ratifiedByPartyId || ""} /> — two people by construction</>],
            ["who may attest", (d.authorisedAttesterFunctions || []).length ? (d.authorisedAttesterFunctions as string[]).map((f) => <Chip key={f} kind="info">{f}</Chip>) : "—"],
          ]} />],
          ["Evidence tiers", <>
            <KVR rows={tierRows} />
            <p className="muted" style={{ marginTop: 8 }}>Read top to bottom; the first rule whose conditions all hold wins. The floor has no requirements at all — that is what keeps the weakest worker payable.</p>
          </>],
          ["Source", <>
            <KVR rows={[
              ["source systems", (plat.sourceSystems || []).length ? (plat.sourceSystems as string[]).map((f, i) => <span key={f}>{i ? " · " : ""}<Mono>{f}</Mono></span>) : "—"],
              ["worker tier ceiling", wkr.tierCeiling != null ? String(wkr.tierCeiling) : "—"],
            ]} />
            <div style={{ marginTop: 10 }}>
              <OpenNote>Adaptor mapping (p3_24–p3_28) and extension fields (p3_14) have no write side. Authoring writes are not built; this shows the signed definition v{d.version} as the wizard would have captured it.</OpenNote>
            </div>
          </>],
          ["Template — the worker's own words", <>
            <div className="consent-quote">{wkr.summary || ""}</div>
            <div style={{ marginTop: 10 }}>
              {(wkr.evidenceInPlainLanguage || []).length ? (
                (wkr.evidenceInPlainLanguage as string[]).map((l) => (
                  <div key={l} style={{ display: "flex", gap: 8, marginBottom: 6 }}>
                    <Chip kind="ok">counts</Chip>
                    <span className="body-2">{l}</span>
                  </div>
                ))
              ) : (
                <div className="muted">The definition carries no worker-language evidence list.</div>
              )}
            </div>
          </>],
          ["Validation", <KVR rows={[
            ["a row missing a required field", "lands in the unclear queue with a reason — never a silent drop"],
            ["who decides whose work it was", "the custodian, holding resolve-unclear-evidence — never the submitter"],
          ]} />],
          ["Payment", pays ? (
            <>
              <KVR rows={[
                ["rate per " + (d.outcomeUnit || "unit"), money(pays.payload!.ratePerOutcomeUnit.amountMinor, pays.payload!.ratePerOutcomeUnit.currency)],
                ["payer", <MonoShort id={pays.payload!.payerPartyId} />],
                ["effective from", when(pays.payload!.effectiveFrom)],
                ["record", <><Mono>{pays.id}</Mono> · v{pays.version} · {pays.state}</>],
              ]} />
              <p className="muted" style={{ marginTop: 8 }}>Attached by reference — the definition is complete without it. See Payment set up for the full journey.</p>
            </>
          ) : (
            <Empty>No payment-setup record is attached. The work is recognised, and recognition is a use of its own.</Empty>
          )],
          ["Ratify", <KVR rows={[
            ["the fact", "two linked signed records: the definition, and its ratification — author and ratifier are different parties or the service refuses (409)"],
            ["authored by", <MonoShort id={d.authoredByPartyId || ""} />],
            ["ratified by", <MonoShort id={d.ratifiedByPartyId || ""} />],
            ["activated", when(d.activatedAt)],
          ]} />],
        ];
        const idx = Math.min(Math.max(s.wizStep || 0, 0), steps.length - 1);
        return (
          <>
            <Title t="Define work — the wizard, read against the real definition" extra={<Chip kind={d.state === "ACTIVE" ? "ok" : "info"}>{"v" + d.version + " · " + d.state}</Chip>} />
            <Lede>
              The design draws a 28-screen authoring journey. This walkthrough shows each step with the values the
              seeded definition actually carries — real data, authoring labeled.
            </Lede>
            <CardTitled t="Definition registry">
              <Tbl
                heads={["Definition", "Activity", "Version", "State"]}
                rows={[[<Mono>{d.id}</Mono>, d.activity?.label || "", "v" + d.version, <Chip kind={d.state === "ACTIVE" ? "ok" : "info"}>{d.state}</Chip>]]}
              />
              <p className="muted" style={{ marginTop: 8 }}>
                The definitions service has no list endpoint (POST-only registry, checked against its OpenAPI); this is
                the one definition the fixture world seeds, read for real.
              </p>
            </CardTitled>
            <OpenNote>Authoring writes are not built; this shows the signed definition v{d.version} as the wizard would have captured it.</OpenNote>
            <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
              {steps.map(([name], i) => (
                <button key={name} className={"btn" + (i === idx ? "" : " secondary")} style={{ width: "auto", padding: "9px 14px", fontSize: 12.5 }} onClick={() => s.setWizStep(i)}>
                  {i + 1} · {name}
                </button>
              ))}
            </div>
            <CardTitled t={"Step " + (idx + 1) + " of " + steps.length + " — " + steps[idx][0]}>{steps[idx][1]}</CardTitled>
          </>
        );
      }}
    </LoadFrame>
  );
}

export function PaySetup() {
  const r = useLoad(loadDefinition);
  return (
    <LoadFrame r={r}>
      {({ d, worker, lr }) => {
        const wkr = (worker && worker.worker) || {};
        const pays = lr.find((x) => x.type === "payment-setup");
        return (
          <>
            <Title t="Payment set up" />
            <Lede>
              The rate is a versioned linked record keyed to the definition — it can change without touching what the
              work is, and an old version is never overwritten.
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
  const r = useLoad(async () => {
    const [org, overdue] = await Promise.all([
      api.get("parties", `/v1/parties/${encodeURIComponent(FIX.org)}`),
      api.get("parties", "/v1/authorizations/overdue").catch(() => ({ authorizations: [] })),
    ]);
    const checks: Array<[string, string | null, string]> = [
      ["attest-work", null, "may this organisation attest work at all (instance-wide)"],
      ["attest-work", FIX.project, "…and specifically on PRJ-118"],
      ["submit-work-evidence", FIX.project, "may it submit evidence on PRJ-118"],
    ];
    const permits = await Promise.all(
      checks.map(([fn, ctx]) => {
        const q = new URLSearchParams({ partyId: FIX.org, function: fn });
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
                ["party", <Mono>{p.id || FIX.org}</Mono>],
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

export function Portfolio() {
  const nav = useNavigate();
  const r = useLoad(async () => {
    const [claims, instr] = await Promise.all([
      api.get("evidence", "/v1/claims").catch(() => ({ claims: [] })),
      api.get("payments", "/v1/instructions").catch(() => ({ instructions: [] })),
    ]);
    return { claims: claims.claims || [], list: instr.instructions || [] };
  });
  return (
    <LoadFrame r={r}>
      {({ claims, list }) => {
        const released = list.filter((i: { state?: string }) => i.state === "RELEASED" || i.state === "SETTLED");
        const paid = released.reduce((sum: number, i: { amountMinor?: number }) => sum + (i.amountMinor || 0), 0);
        const cur = (released[0] || list[0] || {}).currency || "";
        return (
          <>
            <Title t="Portfolio" />
            <Lede>
              One project exists in this deployment; one row. Every count is live; the allocation column is not, and
              says so.
            </Lede>
            <Tbl
              heads={["Project", "Claims", "Instructions", "Paid to workers", "Held", "Allocated vs paid", ""]}
              rows={[[
                <><Mono>PRJ-118</Mono> · Riverside bednet campaign 2026</>,
                String(claims.length),
                String(list.length),
                money(paid, cur),
                String(list.filter((i: { state?: string }) => i.state === "HELD").length),
                ILLUSTRATIVE,
                <button className="btn secondary" style={{ width: "auto", padding: "7px 12px" }} onClick={() => nav("/status")}>
                  Open
                </button>,
              ]]}
            />
            <OpenNote>
              Allocated-vs-paid needs a funding ledger no service holds; the paid column is real (summed released
              instructions), the allocation is not shown rather than invented.
            </OpenNote>
          </>
        );
      }}
    </LoadFrame>
  );
}
