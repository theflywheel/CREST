// J3 Phase B — the screens the project backend (#173) unblocked.
//
// Every one of these reads its data back from the service and writes through
// it: n2 (where do you want to work), n4 (the handover's receiving side, with
// a decline that records who and why), p2_1/p2_3/p2_5 (composition), p2_6
// (roles on this project), p2_7 (activation), p2_8 (the finance code),
// p2_10 (the support owner), p2_17/p2_18 (the partner directory and a grant
// that ends). Nothing on these screens is held in the browser except the
// text you are typing and which project you chose.
//
// Two L1/L2 rules this file obeys and must keep obeying:
//
//   * Gate names, role functions, composition choice names and their values
//     are all L2 vocabulary with no enum anywhere in CREST. So the UI renders
//     whatever the service returns and never offers a taxonomy of its own —
//     where a deployment has declared no vocabulary, the screen says so and
//     takes a typed answer rather than inventing a list.
//   * Authority is split (finding F2): viewing a handed-over project works in
//     any ownership state, configuring it requires ACCEPTED. A 409
//     handover_not_accepted is rendered as the boundary it is — the project
//     stays visible and readable, and the screen says what is missing.
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError } from "@crest/api";
import { Callout, Chip, OpenNote, RefField, StepCounter as StepBar } from "@crest/ui";
import {
  Card, CardTitled, KVR, Lede, LoadFrame, Mono, MonoShort, Stat, Title, Tbl, useLoad, when, short,
} from "../ui";
import { errText, useConsole } from "../state";

// ── shapes, as the service returns them ────────────────────────────────────
type Ownership = {
  partyId?: string; namedByPartyId?: string; namedAt?: string;
  state?: "PENDING" | "ACCEPTED" | "DECLINED"; decidedAt?: string; reason?: string;
};
type Condition = { name: string; satisfied?: boolean; satisfiedAt?: string; because?: string };
type Project = {
  id: string; name?: string; kind?: string; state?: string;
  ownerPartyId?: string; ownership?: Ownership;
  configuration?: Record<string, unknown>;
  activationConditions?: Condition[];
  records?: Array<{ kind?: string; payload?: unknown; recordedBy?: string; recordedAt?: string }>;
};
type Role = {
  grantId?: string; partyId?: string; displayName?: string; partyKind?: string;
  functions?: string[]; grantedByPartyId?: string; authorityPartyId?: string;
  grantedAt?: string; from?: string; until?: string; state?: string;
};

const PROJECTS = "/v1/projects";
const proj = (id: string, tail = "") => `${PROJECTS}/${encodeURIComponent(id)}${tail}`;

// The one thing every write on these screens can hit, rendered as a boundary
// rather than an error: the handover this actor was named for is not accepted.
function WriteRefusal(props: { err: unknown; owner?: string }) {
  const e = props.err;
  const notAccepted = e instanceof ApiError && e.status === 409 && /handover_not_accepted/.test(String(e.code || e.message));
  if (!notAccepted) return <div className="errbar">{errText(e)}</div>;
  return (
    <Callout kind="green" title="You can read this, and not yet change it">
      You were named this project's Configurator, and naming is a proposal rather than an assignment. Until the
      handover is accepted, everything here stays readable and nothing here is writable — answer the handover first,
      and the owning organisation{props.owner ? <> (<MonoShort id={props.owner} />)</> : null} can re-hand it if the
      answer was no.
    </Callout>
  );
}

const ownershipChip = (o?: Ownership) =>
  !o || !o.state ? (
    <Chip kind="plain">nobody has been handed this</Chip>
  ) : o.state === "ACCEPTED" ? (
    <Chip kind="ok">accepted</Chip>
  ) : o.state === "DECLINED" ? (
    <Chip kind="err">declined</Chip>
  ) : (
    <Chip kind="warn">waiting on an answer</Chip>
  );

// ── n2 · Where do you want to work? ────────────────────────────────────────
export function Where() {
  const s = useConsole();
  const nav = useNavigate();
  const me = s.me!.partyId;
  const r = useLoad(async () => {
    const [owned, configuring, org] = await Promise.all([
      api.get("parties", `${PROJECTS}?ownerPartyId=${encodeURIComponent(me)}`).catch(() => ({ projects: [] })),
      api.get("parties", `${PROJECTS}?configuratorPartyId=${encodeURIComponent(me)}`).catch(() => ({ projects: [] })),
      api.get("parties", `/v1/parties/${encodeURIComponent(me)}`).catch(() => null),
    ]);
    return {
      owned: (owned.projects || []) as Project[],
      configuring: (configuring.projects || []) as Project[],
      org: org && (org.party || org),
    };
  }, [me]);
  return (
    <LoadFrame r={r}>
      {({ owned, configuring, org }) => {
        const open = (p: Project, asOwner: boolean) => {
          s.setProjectId(p.id);
          if (!asOwner && p.ownership?.state !== "ACCEPTED") nav("/handover");
          else nav(asOwner ? "/org" : "/status");
        };
        return (
          <>
            <Title t="Where do you want to work?" />
            <Lede>
              Everything below is a context the registry says you hold a role in. Nothing else appears here, and
              nothing here was chosen by this browser.
            </Lede>
            {org ? (
              <Card>
                <span className="eyebrow">Organisation</span>
                <div style={{ font: "500 14px/1.4 Roboto", marginTop: 4 }}>{String(org.displayName || "")}</div>
                <div className="muted">You hold: Org Admin</div>
                <div style={{ height: 10 }} />
                <button className="btn inline" onClick={() => nav("/org")}>
                  Open standing configuration
                </button>
              </Card>
            ) : null}
            {owned.map((p) => (
              <Card key={"o" + p.id}>
                <span className="eyebrow">Project · owned by your organisation</span>
                <div style={{ font: "500 14px/1.4 Roboto", marginTop: 4 }}>{p.name || short(p.id)}</div>
                <div className="muted">
                  {p.state} · handover {p.ownership?.state ? p.ownership.state.toLowerCase() : "not started"}
                </div>
                <div style={{ height: 10 }} />
                <button className="btn inline" data-context={p.id} onClick={() => open(p, true)}>
                  Open project setup
                </button>
              </Card>
            ))}
            {configuring.map((p) => (
              <Card hi key={"c" + p.id}>
                <span className="eyebrow">Project · you were named its Configurator</span>
                <div style={{ font: "500 14px/1.4 Roboto", marginTop: 4 }}>{p.name || short(p.id)}</div>
                <div className="muted">You hold: Project Configurator · {ownershipChip(p.ownership)}</div>
                <div style={{ height: 10 }} />
                <button className="btn inline" data-context={p.id} onClick={() => open(p, false)}>
                  Open project setup
                </button>
              </Card>
            ))}
            {owned.length + configuring.length === 0 ? (
              <Card>
                <div style={{ font: "500 14px/1.4 Roboto" }}>No project has been handed to you</div>
                <p className="muted">
                  An empty list is a true answer: it means a role still has to be granted. The organisation that can
                  grant one is {org?.displayName ? String(org.displayName) : "your organisation's admin"}
                  {(org?.contactRoutes || []).length
                    ? " · " +
                      (org.contactRoutes as Array<{ kind: string; value?: string }>)
                        .map((rt) => rt.kind + " " + (rt.value || ""))
                        .join(" · ")
                    : ""}
                  .
                </p>
              </Card>
            ) : null}
            <Callout kind="green" title="What this screen never does">
              It never lists a context you hold no role in, and never shows a project before somebody handed it to
              you. An empty list is a true answer: it means a role still has to be granted.
            </Callout>
          </>
        );
      }}
    </LoadFrame>
  );
}

// ── n4 · Ministry of Health handed you a project ───────────────────────────
export function Handover() {
  const s = useConsole();
  const nav = useNavigate();
  const [gen, setGen] = useState(0);
  const [reason, setReason] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const r = useLoad(async () => {
    const [p, own, owner] = await Promise.all([
      api.get("parties", proj(s.projectId)),
      api.get("parties", proj(s.projectId, "/ownership")).catch(() => null),
      null,
    ]);
    const project = (p.project || p) as Project;
    const org = project.ownerPartyId
      ? await api.get("parties", `/v1/parties/${encodeURIComponent(project.ownerPartyId)}`).catch(() => null)
      : owner;
    return { project, own, org: org && (org.party || org) };
  }, [s.projectId, gen]);
  const decide = async (decision: "accepted" | "declined") => {
    setErr(null);
    try {
      await api.post("parties", proj(s.projectId, "/ownership-decision"),
        decision === "declined" ? { decision, reason } : { decision });
      if (decision === "accepted") nav("/compose");
      else setGen((g) => g + 1);
    } catch (e) {
      setErr(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {({ project, own, org }) => {
        const o = project.ownership;
        const cfg = (project.configuration || {}) as Record<string, unknown>;
        const events = (own?.events || []) as Array<{ event: string; partyId?: string; actorPartyId?: string; reason?: string; at?: string }>;
        return (
          <>
            <Title
              t={(org?.displayName ? String(org.displayName) : "Your organisation") + " handed you a project"}
              extra={ownershipChip(o)}
            />
            <p className="muted" style={{ fontSize: 13 }}>
              {project.name || short(project.id)}
              {cfg.coverage ? " · " + String(cfg.coverage) : ""}
            </p>
            <Lede>
              {o?.namedByPartyId ? <MonoShort id={o.namedByPartyId} /> : "The Org Admin"} created this project and named
              you its Configurator. Creating a project is not configuring one — what arrives is a name, a coverage area
              and an owner. Everything that decides how it runs is still unanswered, and it is yours to answer.
            </Lede>
            <div className="pane-cols">
              <div>
                <CardTitled t="What arrived, already decided">
                  <KVR
                    rows={[
                      ["project name", project.name || "—"],
                      ["coverage", cfg.coverage ? String(cfg.coverage) : "not recorded on the project"],
                      ["configurator", o?.partyId ? <MonoShort id={o.partyId} /> : "nobody named"],
                      ["named by", o?.namedByPartyId ? <MonoShort id={o.namedByPartyId} /> : "—"],
                      ["named at", when(o?.namedAt)],
                      ["organisation", org?.displayName ? String(org.displayName) : <MonoShort id={project.ownerPartyId || ""} />],
                      ["project state", project.state || "—"],
                    ]}
                  />
                </CardTitled>
                <CardTitled t="Every answer this handover has ever had">
                  <Tbl
                    heads={["#", "Event", "Who", "Why", "When"]}
                    rows={events.map((e) => [
                      String((e as { seq?: number }).seq ?? ""),
                      <Chip kind={e.event === "DECLINED" ? "err" : e.event === "ACCEPTED" ? "ok" : "info"}>{e.event}</Chip>,
                      <MonoShort id={e.actorPartyId || e.partyId || ""} />,
                      e.reason || "—",
                      when(e.at),
                    ])}
                    empty="Nothing has been handed over yet."
                  />
                </CardTitled>
              </div>
            </div>
            {err ? <WriteRefusal err={err} owner={project.ownerPartyId} /> : null}
            {o?.state === "PENDING" ? (
              <>
                <RefField label="If you are handing it back, say why" hint="Required on a decline — recorded against your name, and readable afterwards">
                  <input name="declinereason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="the reason this is not yours" />
                </RefField>
                <div className="btn-row" style={{ maxWidth: 560 }}>
                  <button className="btn secondary" id="decline-handover" onClick={() => decide("declined")}>
                    Not mine — hand it back
                  </button>
                  <button className="btn dominant" id="accept-handover" onClick={() => decide("accepted")}>
                    Continue to setup
                  </button>
                </div>
              </>
            ) : o?.state === "DECLINED" ? (
              <Callout kind="teal" title="This handover was declined">
                {o.reason ? <>The reason recorded was: “{o.reason}”. </> : null}
                The project and every record keyed to it are exactly where they were. It is back in the Org Admin's
                queue, and only the owning organisation can hand it on again.
              </Callout>
            ) : o?.state === "ACCEPTED" ? (
              <div className="btn-row" style={{ maxWidth: 560 }}>
                <button className="btn dominant" onClick={() => nav("/compose")}>
                  Continue to setup
                </button>
              </div>
            ) : (
              <OpenNote>
                Nobody has been handed this project, so there is nothing to accept or decline. The Org Admin names a
                Configurator from the project's own screen.
              </OpenNote>
            )}
            <Callout kind="green" title="Why handing it back is a real button">
              A named owner who never agreed is worse than no owner: the project looks staffed and is not. Declining
              records who declined and why, and returns the project to the Org Admin's queue rather than deleting
              anything.
            </Callout>
          </>
        );
      }}
    </LoadFrame>
  );
}

// ── p2_1 / p2_3 / p2_5 · composition, answered separately ──────────────────
// Choice names and values are L2 with no enum in CREST. Where the project's
// own configuration declares a vocabulary, this renders it; where it does not,
// it takes a typed answer and says why there is no list.
// The reference's p2_1 five capabilities, in its own words. The five names are
// programme vocabulary (L2) — the infrastructure carries no enum of them; they
// are recorded through the same composition endpoint any deployment's
// vocabulary goes through, one named answer per capability.
const CAPABILITIES = [
  ["register-workers", "Register workers",
    "Run CREST enrollment, or map identifiers from a registry you already have."],
  ["define-work", "Define work", "What counts as done, what proves it, who checks it."],
  ["set-up-payment", "Set up payment",
    "Configure a rate and decide how the money reaches the worker. Off means credentials still issue, with no rate attached."],
  ["validate", "Validate",
    "Check evidence received from another system, or uploaded as a spreadsheet, against the definition before a credential issues. CREST never records the work itself."],
  ["carry-forward", "Carry forward", "A worker keeps their verified history after this project ends."],
] as const;

// One capability row: the reference's orange checkbox, title and sub-line.
function CapabilityRow(props: {
  on: boolean;
  recorded: boolean;
  title: string;
  sub: string;
  onFlip: () => void;
}) {
  return (
    <div style={{ display: "flex", gap: 12, alignItems: "flex-start", padding: "12px 0" }}>
      <button
        onClick={props.onFlip}
        aria-pressed={props.on}
        data-capability={props.title}
        title={props.recorded ? "Recorded — click to flip it" : "Not yet recorded — the project does this until a record narrows it. Click to record an answer."}
        style={{
          width: 18,
          height: 18,
          marginTop: 2,
          flexShrink: 0,
          borderRadius: 4,
          border: props.on ? "none" : "2px solid var(--muted, #8A857E)",
          background: props.on ? "#C84C0E" : "transparent",
          color: "#fff",
          font: "700 13px/18px system-ui",
          cursor: "pointer",
          padding: 0,
        }}
      >
        {props.on ? "✓" : ""}
      </button>
      <div>
        <div style={{ font: "500 15px/1.4 Roboto,system-ui" }}>{props.title}</div>
        <div className="muted" style={{ font: "400 13px/1.5 Roboto,system-ui", marginTop: 2 }}>
          {props.sub}
          {props.recorded ? null : <span> · not yet recorded — on until an answer narrows it</span>}
        </div>
      </div>
    </div>
  );
}

export function Compose() {
  const s = useConsole();
  const nav = useNavigate();
  const [gen, setGen] = useState(0);
  const [err, setErr] = useState<unknown>(null);
  const r = useLoad(async () => {
    const [p, comp] = await Promise.all([
      api.get("parties", proj(s.projectId)),
      api.get("parties", proj(s.projectId, "/composition")).catch(() => ({ choices: [] })),
    ]);
    return {
      project: (p.project || p) as Project,
      choices: (comp.choices || []) as Array<{ kind?: string; payload?: unknown; recordedBy?: string; recordedAt?: string }>,
    };
  }, [s.projectId, gen]);
  const record = async (name: string, v: unknown) => {
    setErr(null);
    try {
      await api.put("parties", proj(s.projectId, `/composition/${encodeURIComponent(name)}`), { value: v });
      setGen((g) => g + 1);
    } catch (e) {
      setErr(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {({ project, choices }) => {
        // A capability is on until a recorded answer narrows it: turning one
        // off narrows what the project does, and an unanswered choice is the
        // absence of a record, not a no.
        const answered = new Map(choices.map((c) => [c.kind, c.payload]));
        return (
          <>
            <Title t="What this project needs from CREST" extra={ownershipChip(project.ownership)} />
            <Lede>
              Five independent choices. None is a degraded version of another, and a project that answers no to most
              of them is still a valid project.
            </Lede>
            {err ? <WriteRefusal err={err} owner={project.ownerPartyId} /> : null}
            <div style={{ maxWidth: 820 }}>
              {CAPABILITIES.map(([key, title, sub]) => {
                const rec = answered.get(key);
                const on = rec === undefined ? true : rec !== "off";
                return (
                  <CapabilityRow
                    key={key}
                    on={on}
                    recorded={rec !== undefined}
                    title={title}
                    sub={sub}
                    onFlip={() => record(key, on ? "off" : "on")}
                  />
                );
              })}
              <div style={{ height: 10 }} />
              <Callout kind="teal" title="">
                A project with a mature worker registry and its own bank relationship can adopt the trust layer alone
                — register off, pay off, validate on. The credential is never contingent on a payment component being
                present, connected or resolved.
              </Callout>
              <div style={{ borderTop: "1px solid var(--line, #E5E1DC)", marginTop: 16, paddingTop: 14, display: "flex", justifyContent: "flex-end", gap: 10 }}>
                <button className="btn secondary" style={{ width: "auto", padding: "10px 22px" }} onClick={() => nav("/handover")}>
                  Back
                </button>
                <button className="btn dominant" style={{ width: "auto", padding: "10px 22px" }} onClick={() => nav("/workers")}>
                  Continue
                </button>
              </div>
            </div>
          </>
        );
      }}
    </LoadFrame>
  );
}

// ── p2_6 · Who owns which half ─────────────────────────────────────────────
// Configuring a project is not staffing it. The grant is an Authorization
// with a grantor and a date; `functions` is the whole role vocabulary and it
// is opaque, so this screen takes the words rather than offering a menu.
export function Owners() {
  const s = useConsole();
  const nav = useNavigate();
  const [gen, setGen] = useState(0);
  const [party, setParty] = useState("");
  const [functions, setFunctions] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const r = useLoad(async () => {
    const out = await api.get("parties", proj(s.projectId, "/roles"));
    return {
      roles: (out.roles || []) as Role[],
      grantableBy: out.grantableBy as string | undefined,
      projectId: out.projectId as string,
    };
  }, [s.projectId, gen]);
  const grant = async () => {
    setErr(null);
    try {
      await api.post("parties", proj(s.projectId, "/roles"), {
        partyId: party.trim(),
        functions: functions.split(",").map((f) => f.trim()).filter(Boolean),
      });
      setParty("");
      setFunctions("");
      setGen((g) => g + 1);
    } catch (e) {
      setErr(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {({ roles, grantableBy }) => (
        <>
          <Title t="Who owns which half" />
          <Lede>
            Configuring a project is not staffing it. Each row below is a grant somebody made, on a date, under a
            named authority — which is what makes an audit trail against a person mean anything.
          </Lede>
          <CardTitled t="Who holds a role on this project">
            <Tbl
              heads={["Holder", "Functions", "Granted by", "Authority", "Since", "Until", "State"]}
              rows={roles.map((g) => [
                <>
                  {g.displayName || <MonoShort id={g.partyId} />}
                  {g.partyKind ? <span className="muted"> · {g.partyKind}</span> : null}
                </>,
                (g.functions || []).join(", "),
                <MonoShort id={g.grantedByPartyId || ""} />,
                <MonoShort id={g.authorityPartyId || ""} />,
                when(g.grantedAt || g.from),
                when(g.until),
                <Chip kind={g.state === "ACTIVE" ? "ok" : g.state === "REVOKED" ? "err" : "warn"}>{g.state}</Chip>,
              ])}
              empty="Nobody holds a role on this project yet. A vacant role is not a blocked project — it narrows what the project can do until somebody holds it."
            />
            <p className="muted" style={{ marginTop: 8 }}>
              Revoked and expired grants are listed with their state rather than filtered out: a console showing only
              live grants cannot answer “who used to be able to do this, and who took it away”.
            </p>
          </CardTitled>
          {err ? <WriteRefusal err={err} owner={grantableBy} /> : null}
          <CardTitled t="Grant a role on this project">
            <form
              id="grantform"
              style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}
              onSubmit={(ev) => {
                ev.preventDefault();
                grant();
              }}
            >
              <RefField label="Person or organisation">
                <input name="grantparty" required value={party} onChange={(e) => setParty(e.target.value)} placeholder="did:crest:party:…" />
              </RefField>
              <RefField label="Functions" hint="Comma separated. This is the role vocabulary, and it is this deployment's, not CREST's">
                <input name="grantfunctions" required value={functions} onChange={(e) => setFunctions(e.target.value)} placeholder="submit-work-evidence" />
              </RefField>
              <button className="btn inline">Grant it</button>
            </form>
            <p className="muted" style={{ marginTop: 8 }}>
              Granting is the owning organisation's act
              {grantableBy ? <> — <MonoShort id={grantableBy} /></> : null}. It is deliberately not open to the named
              configurator, who must not be able to grant themselves anything.
            </p>
          </CardTitled>
          <div className="btn-row" style={{ maxWidth: 560 }}>
            <button className="btn secondary" onClick={() => nav("/compose")}>
              Back
            </button>
            <button className="btn dominant" onClick={() => nav("/partners")}>
              Send invitations
            </button>
          </div>
        </>
      )}
    </LoadFrame>
  );
}

// ── p2_7 · Before it goes live ─────────────────────────────────────────────
// Every condition, satisfied ones included: a list of only the unmet ones
// cannot tell a ready project from one whose gates were never declared. Gate
// names are the deployment's, so nothing here is hardcoded.
export function Activate() {
  const s = useConsole();
  const nav = useNavigate();
  const [gen, setGen] = useState(0);
  const [err, setErr] = useState<unknown>(null);
  const [gate, setGate] = useState("");
  // The gate list is declared as a whole, and the read does not say which
  // conditions are this deployment's gates and which are infrastructure's own
  // (the handover being acknowledged, the project being DRAFT). So this screen
  // sends only the gates declared from it — echoing every condition back would
  // turn an infrastructure condition into a deployment gate somebody could
  // "satisfy" by hand, which is worse than the smaller list. Named as an API
  // gap in docs/journey-traceability.md rather than worked around silently.
  const [declaredHere, setDeclaredHere] = useState<string[]>([]);
  const r = useLoad(async () => {
    const out = await api.get("parties", proj(s.projectId, "/activation"));
    return out as { projectId: string; state?: string; conditions?: Condition[]; unmet?: string[]; activatable?: boolean };
  }, [s.projectId, gen]);
  const act = async (fn: () => Promise<unknown>) => {
    setErr(null);
    try {
      await fn();
      setGen((g) => g + 1);
    } catch (e) {
      setErr(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {(a) => (
        <>
          <Title
            t="Before it goes live"
            extra={<Chip kind={a.state === "ACTIVE" ? "ok" : "warn"}>{a.state || "—"}</Chip>}
          />
          <Lede>
            What a project looks like before it runs: every condition this deployment declared, the ones already
            satisfied included, because “nothing left to show” and “nothing was ever declared” are not the same
            project.
          </Lede>
          <div className="stats">
            <Stat n={(a.conditions || []).length} label="conditions declared" />
            <Stat n={(a.unmet || []).length} label="standing in the way" />
            <Stat n={a.activatable ? "yes" : "no"} label="activatable right now" />
          </div>
          <CardTitled t="Every condition, and what it means">
            <Tbl
              heads={["Condition", "Satisfied", "When", "What it is"]}
              rows={(a.conditions || []).map((c) => [
                <Mono>{c.name}</Mono>,
                c.satisfied ? <Chip kind="ok">yes</Chip> : <Chip kind="warn">not yet</Chip>,
                when(c.satisfiedAt),
                c.because || "—",
              ])}
              empty="This deployment declared no conditions for this project. That is not the same as a project that is ready — nothing was ever asked of it."
            />
          </CardTitled>
          {err ? <WriteRefusal err={err} /> : null}
          <CardTitled t="Declare and satisfy conditions">
            <form
              id="gateform"
              style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}
              onSubmit={(ev) => {
                ev.preventDefault();
                const names = [...declaredHere, gate.trim()].filter(Boolean);
                setDeclaredHere([...new Set(names)]);
                act(() => api.put("parties", proj(s.projectId, "/activation/gates"), { gates: [...new Set(names)] }));
                setGate("");
              }}
            >
              <RefField label="Add a condition" hint="A gate name is this deployment's word; CREST never reads it">
                <input name="gatename" required value={gate} onChange={(e) => setGate(e.target.value)} placeholder="rate-published" />
              </RefField>
              <button className="btn inline">Declare it</button>
            </form>
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 10 }}>
              {(a.conditions || [])
                .filter((c) => !c.satisfied)
                .map((c) => (
                  <button
                    key={c.name}
                    className="btn secondary inline"
                    data-satisfy={c.name}
                    onClick={() =>
                      act(() =>
                        api.post("parties", proj(s.projectId, `/activation/gates/${encodeURIComponent(c.name)}/satisfied`)),
                      )
                    }
                  >
                    Mark “{c.name}” satisfied
                  </button>
                ))}
            </div>
            <p className="muted" style={{ marginTop: 8 }}>
              The satisfaction date is the service's clock, never a time this screen chose, and an undeclared gate is
              refused rather than created — a gate that appears when it is satisfied never gated anything. Declaring
              replaces the gate list, and this screen sends only the gates declared here: the read cannot tell a
              deployment's gate from one of infrastructure's own conditions, so echoing them all back would promote a
              condition nobody declared into a gate somebody could tick.
            </p>
          </CardTitled>
          <div className="btn-row" style={{ maxWidth: 560 }}>
            <button className="btn secondary" onClick={() => nav("/partners")}>
              Back
            </button>
            <button
              className="btn dominant"
              id="activate-project"
              onClick={() => act(() => api.post("parties", proj(s.projectId, "/activation")))}
            >
              Activate project
            </button>
          </div>
          <Callout kind="teal" title="A refusal that names what is missing">
            “Cannot activate” without saying what is missing is a dead end, and this system does not leave people at
            dead ends: a refused activation answers with every condition and which of them are unmet, and they are
            listed above.
          </Callout>
        </>
      )}
    </LoadFrame>
  );
}

// ── p2_8 · Link this project to a finance code ─────────────────────────────
export function FinanceCode() {
  const s = useConsole();
  const nav = useNavigate();
  const [gen, setGen] = useState(0);
  const [system, setSystem] = useState("");
  const [code, setCode] = useState("");
  const [label, setLabel] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const r = useLoad(
    async () => api.get("parties", proj(s.projectId, "/finance-link")).catch(() => null),
    [s.projectId, gen],
  );
  const link = async () => {
    setErr(null);
    try {
      await api.put("parties", proj(s.projectId, "/finance-link"), {
        system: system.trim(),
        code: code.trim(),
        label: label.trim() || undefined,
      });
      setGen((g) => g + 1);
    } catch (e) {
      setErr(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {(rec) => {
        const p = (rec?.payload || {}) as { system?: string; code?: string; label?: string };
        return (
          <>
            <StepBar>Project setup · 5 of 7</StepBar>
            <Title t="Link this project to a finance code" />
            <Lede>
              CREST does not invent account codes. The code arrives from a finance system that already had it, and is
              stored exactly as that system wrote it.
            </Lede>
            <CardTitled t="Linked now">
              <KVR
                rows={[
                  ["system", p.system ? <Mono>{p.system}</Mono> : "nothing linked"],
                  ["code", p.code ? <Mono>{p.code}</Mono> : "—"],
                  ["label", p.label || "—"],
                  ["linked by", rec?.recordedBy ? <MonoShort id={rec.recordedBy} /> : "—"],
                  ["linked at", when(rec?.recordedAt)],
                ]}
              />
            </CardTitled>
            {err ? <WriteRefusal err={err} /> : null}
            <form
              id="financeform"
              style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}
              onSubmit={(ev) => {
                ev.preventDefault();
                link();
              }}
            >
              <RefField label="Finance system" hint="Required: a code with no named source cannot be traced back to whoever minted it">
                <input name="financesystem" required value={system} onChange={(e) => setSystem(e.target.value)} placeholder="IFMIS · Kenya Treasury" />
              </RefField>
              <RefField label="Code">
                <input name="financecode" required value={code} onChange={(e) => setCode(e.target.value)} placeholder="4402-11-A7" />
              </RefField>
              <RefField label="Label">
                <input name="financelabel" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="what finance calls it" />
              </RefField>
              <button className="btn inline">Use this code</button>
            </form>
            <Callout kind="teal" title="Read the last row">
              A code that reads correctly to anyone outside finance and was closed last year is a rejection weeks
              after the work was validated. CREST never generates, formats, validates or reserves a code — only the
              finance system can know whether one is live.
            </Callout>
            <div className="btn-row" style={{ maxWidth: 560 }}>
              <button className="btn secondary" onClick={() => nav("/activate")}>
                Back
              </button>
              <button className="btn dominant" onClick={() => nav("/support")}>
                Continue
              </button>
            </div>
          </>
        );
      }}
    </LoadFrame>
  );
}

// ── p2_10 · Who a worker reaches when something goes wrong ─────────────────
export function SupportOwner() {
  const s = useConsole();
  const nav = useNavigate();
  const [gen, setGen] = useState(0);
  const [partyId, setPartyId] = useState("");
  const [kind, setKind] = useState("phone");
  const [value, setValue] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const r = useLoad(
    async () => api.get("parties", proj(s.projectId, "/support-owner")).catch(() => null),
    [s.projectId, gen],
  );
  const save = async () => {
    setErr(null);
    try {
      await api.put("parties", proj(s.projectId, "/support-owner"), {
        partyId: partyId.trim(),
        contactRoute: value.trim() ? { kind, value: value.trim() } : undefined,
      });
      setGen((g) => g + 1);
    } catch (e) {
      setErr(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {(rec) => {
        const p = (rec?.payload || {}) as {
          partyId?: string; contactRoute?: { kind?: string; value?: string }; escalatesToPartyId?: string;
        };
        return (
          <>
            <StepBar>Project setup · 6 of 7</StepBar>
            <Title t="Who a worker reaches when something goes wrong" />
            <Lede>
              First line sits with the project, because the project is where the answer is. The instance keeps only
              genuine platform faults.
            </Lede>
            <CardTitled t="First line for this project">
              <KVR
                rows={[
                  ["support owner", p.partyId ? <MonoShort id={p.partyId} /> : "nobody named — a worker's question reaches nobody"],
                  ["how they are reached", p.contactRoute?.value ? p.contactRoute.kind + " " + p.contactRoute.value : "no contact route recorded"],
                  ["escalates to", p.escalatesToPartyId ? <MonoShort id={p.escalatesToPartyId} /> : "escalation has not been arranged — which is not the same as unnecessary"],
                  ["set by", rec?.recordedBy ? <MonoShort id={rec.recordedBy} /> : "—"],
                  ["set at", when(rec?.recordedAt)],
                ]}
              />
            </CardTitled>
            {err ? <WriteRefusal err={err} /> : null}
            <form
              id="supportform"
              style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}
              onSubmit={(ev) => {
                ev.preventDefault();
                save();
              }}
            >
              <RefField label="Support owner" hint="Must resolve to a real party: a support owner nobody can name is the dead end that leaves a worker with a missing payment and no explanation">
                <input name="supportparty" required value={partyId} onChange={(e) => setPartyId(e.target.value)} placeholder="did:crest:party:…" />
              </RefField>
              <RefField label="Contact route kind">
                <input name="supportkind" value={kind} onChange={(e) => setKind(e.target.value)} />
              </RefField>
              <RefField label="Contact route">
                <input name="supportvalue" value={value} onChange={(e) => setValue(e.target.value)} placeholder="+254…" />
              </RefField>
              <button className="btn inline">Save the owner</button>
            </form>
            <Callout kind="teal" title="What moved, and why">
              This changed in this version. Support used to sit at instance level, which meant every worker question
              travelled past the people best placed to answer it. First line is now project-scoped and staffed; the
              instance keeps only genuine platform faults.
            </Callout>
            <div className="btn-row" style={{ maxWidth: 560 }}>
              <button className="btn secondary" onClick={() => nav("/finance")}>
                Back
              </button>
              <button className="btn dominant" onClick={() => nav("/status")}>
                Continue
              </button>
            </div>
          </>
        );
      }}
    </LoadFrame>
  );
}

// ── p2_17 / p2_18 · the directory, and a grant that ends ───────────────────
export function Partners() {
  const s = useConsole();
  const nav = useNavigate();
  const [sector, setSector] = useState("");
  const [country, setCountry] = useState("");
  const [permission, setPermission] = useState("");
  const [gen, setGen] = useState(0);
  const [picked, setPicked] = useState<string>("");
  const [functions, setFunctions] = useState("");
  const [until, setUntil] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const [note, setNote] = useState<string>("");
  const r = useLoad(async () => {
    const q = new URLSearchParams();
    if (sector) q.set("sector", sector);
    if (country) q.set("country", country);
    permission.split(",").map((p) => p.trim()).filter(Boolean).forEach((p) => q.append("permission", p));
    const [dir, grants] = await Promise.all([
      api.get("parties", "/v1/partners" + (q.toString() ? "?" + q : "")),
      api.get("parties", proj(s.projectId, "/partner-grants")).catch(() => ({ grants: [] })),
    ]);
    return {
      partners: (dir.partners || []) as Array<{
        partyId: string; displayName?: string; sector?: string; country?: string;
        termsId?: string; termsVersion?: number; permissions?: string[]; approvedAt?: string;
      }>,
      grants: (grants.grants || []) as Role[],
    };
  }, [s.projectId, gen]);
  const grant = async () => {
    setErr(null);
    setNote("");
    try {
      const body: Record<string, unknown> = {
        partyId: picked,
        functions: functions.split(",").map((f) => f.trim()).filter(Boolean),
      };
      // Period is { start, end } (schemas/primitives/common.schema.json):
      // the grant starts now and ends on the date the screen asked for.
      if (until) body.period = { start: new Date().toISOString(), end: new Date(until).toISOString() };
      await api.post("parties", proj(s.projectId, "/partner-grants"), body);
      setNote("Granted. It lapses on its own end date — what ends is permission to do more, never the standing of what was already validated.");
      setGen((g) => g + 1);
    } catch (e) {
      setErr(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {({ partners, grants }) => (
        <>
          <Title t="Find a partner for this project" />
          <Lede>Approved organisations, filtered by what their own terms already allow.</Lede>
          <Callout kind="teal" title="What onboarding already did, once">
            These organisations registered and were approved independently of this project, some of them years ago.
            Nothing about their identity, their documents or their terms is re-examined here, and this project cannot
            change any of it.
          </Callout>
          <form
            id="partnerfilter"
            style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}
            onSubmit={(ev) => {
              ev.preventDefault();
              setGen((g) => g + 1);
            }}
          >
            <RefField label="Sector">
              <input name="sector" value={sector} onChange={(e) => setSector(e.target.value)} placeholder="health" />
            </RefField>
            <RefField label="Country">
              <input name="country" value={country} onChange={(e) => setCountry(e.target.value)} placeholder="KE" />
            </RefField>
            <RefField label="Must be able to" hint="Filters on what their terms allow, so nobody appears here who could not do the work">
              <input name="permission" value={permission} onChange={(e) => setPermission(e.target.value)} placeholder="submit-work-evidence" />
            </RefField>
            <button className="btn inline">Filter</button>
          </form>
          <CardTitled t="Approved organisations">
            <Tbl
              heads={["Organisation", "Sector", "Country", "Terms held", "What their terms allow", "Approved", ""]}
              rows={partners.map((p) => [
                <>
                  {p.displayName || <MonoShort id={p.partyId} />}
                  <br />
                  <MonoShort id={p.partyId} />
                </>,
                p.sector || "—",
                p.country || "—",
                p.termsId ? <Mono>{short(p.termsId) + " v" + (p.termsVersion ?? "?")}</Mono> : "—",
                (p.permissions || []).join(", ") || "—",
                when(p.approvedAt),
                <button className="btn secondary" data-pick={p.partyId} style={{ width: "auto", padding: "7px 12px" }} onClick={() => setPicked(p.partyId)}>
                  Compose the grant
                </button>,
              ])}
              empty="No approved organisation matches. The directory is a join over decisions somebody else already made — an empty answer means nobody has been approved for this, not that the filter is broken."
            />
          </CardTitled>
          <Title t="What a partner may do on this project" />
          {err ? <WriteRefusal err={err} /> : null}
          {note ? <Callout kind="green" title="Recorded">{note}</Callout> : null}
          <form
            id="partnergrantform"
            style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}
            onSubmit={(ev) => {
              ev.preventDefault();
              grant();
            }}
          >
            <RefField label="Partner">
              <input name="grantpartner" required value={picked} onChange={(e) => setPicked(e.target.value)} placeholder="did:crest:party:…" />
            </RefField>
            <RefField label="What they may do" hint="Not the whole project, and never wider than the terms they accepted">
              <input name="grantfns" required value={functions} onChange={(e) => setFunctions(e.target.value)} placeholder="submit-work-evidence" />
            </RefField>
            <RefField label="Valid until" hint="Required: the grant lapses on this date by itself">
              <input name="grantuntil" type="date" value={until} onChange={(e) => setUntil(e.target.value)} />
            </RefField>
            <button className="btn inline">Send the grant</button>
          </form>
          <CardTitled t="Grants standing on this project">
            <Tbl
              heads={["Partner", "May do", "From", "Until", "Terms it rides", "State"]}
              rows={grants.map((g) => [
                g.displayName || <MonoShort id={g.partyId} />,
                (g.functions || []).join(", "),
                when(g.from || g.grantedAt),
                when(g.until),
                (g as { terms?: { id?: string; version?: number } }).terms?.id ? (
                  <Mono>{short(String((g as { terms?: { id?: string; version?: number } }).terms!.id)) + " v" + ((g as { terms?: { version?: number } }).terms!.version ?? "?")}</Mono>
                ) : (
                  "—"
                ),
                <Chip kind={g.state === "ACTIVE" ? "ok" : "warn"}>{g.state}</Chip>,
              ])}
              empty="No partner holds anything on this project yet."
            />
          </CardTitled>
          <Callout kind="teal" title="What happens on the end date">
            The grant lapses by itself. Work validated before then stays validated and every credential already
            issued is untouched, because what ends is permission to do more rather than the standing of what was
            already done.
          </Callout>
          <Callout kind="green" title="What a partner cannot do with this">
            This grant cannot be passed on. If a partner sub-contracts, those bodies are invited by this project
            directly and hold their own grants — the partner cannot bring them in.
          </Callout>
          <OpenNote>
            The reference's next step is an invitation to the partner. There is no invitation service in CREST, so
            nothing is sent from this screen: the grant exists in the registry and the partner is told out of band.
            Named as a gap rather than approximated with an email nobody would receive.
          </OpenNote>
          <div className="btn-row" style={{ maxWidth: 560 }}>
            <button className="btn secondary" onClick={() => nav("/owners")}>
              Back
            </button>
            <button className="btn dominant" onClick={() => nav("/activate")}>
              Continue
            </button>
          </div>
        </>
      )}
    </LoadFrame>
  );
}
