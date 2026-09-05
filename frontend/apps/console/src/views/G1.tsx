// G-1 — setting up the instance (g1_1–g1_6) and the admission review the
// operator actually performs (g4_1–g4_3).
//
// The honesty rule of this file: a CREST instance is stood up by compose or
// Railway, not from a console. §3's instance self-description (#70) is
// deliberately derived from the environment and never stored, so a console
// that offered to edit it would be offering to edit something that does not
// take edits. The g1 screens therefore keep the reference's frames and say,
// on their face, that these values are deploy-time L1 configuration — the
// layering test read back at the person most likely to go looking for the
// edit button.
//
// What IS real here, end to end: the admission review. GET /v1/registrations
// is the queue of applications a person still has to look at, the detail is
// the registration read, and Approve/Reject goes through
// POST /v1/organisations/{id}/decision — the decider is the authenticated
// caller, checked by the service (#89), never a name typed into a form.
import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api } from "@crest/api";
import { Callout, Chip, OpenNote } from "@crest/ui";
import {
  when, Mono, MonoShort, KVR, Title, Lede, Tbl, CardTitled, useLoad, LoadFrame,
} from "../ui";
import { useConsole } from "../state";

// The G-1 rail is the reference's own five entries; these buttons walk the
// screens in the reference's order.
function WalkButtons(p: { back?: string; next?: string; nextLabel?: string }) {
  const nav = useNavigate();
  return (
    <div style={{ display: "flex", gap: 8 }}>
      {p.back ? (
        <button className="btn secondary" style={{ width: "auto", padding: "9px 16px" }} onClick={() => nav(p.back!)}>
          Back
        </button>
      ) : null}
      {p.next ? (
        <button id="g1-next" className="btn" style={{ width: "auto", padding: "9px 16px" }} onClick={() => nav(p.next!)}>
          {p.nextLabel || "Continue"}
        </button>
      ) : null}
    </div>
  );
}

const loadInstance = async () => {
  const [instAnswer, issuer] = await Promise.all([
    api.get("parties", "/v1/instance"),
    api.get("verification", "/v1/issuer").catch(() => null),
  ]);
  return { inst: instAnswer.instance, issuer };
};

// g1_1 — the stand-up front door. In CREST an instance is stood up at deploy
// time; this frame states what this deployment already IS, read live.
export function G1Setup() {
  const nav = useNavigate();
  const r = useLoad(async () => {
    const base = await loadInstance();
    // The fourth step of the reference's wizard, answered from the record:
    // has any organisation been admitted? Operator-only read; a refusal
    // renders as unknown rather than invented.
    const regs = await api.get("parties", "/v1/registrations").catch(() => null);
    return { ...base, regs: regs ? regs.registrations || regs || [] : null };
  });
  return (
    <LoadFrame r={r}>
      {({ inst, regs }) => {
        const approved = Array.isArray(regs) ? regs.filter((x: any) => x.state === "APPROVED").length : null;
        const undecided = Array.isArray(regs) ? regs.filter((x: any) => x.state !== "APPROVED" && x.state !== "REJECTED").length : null;
        // The reference's four steps, drawn as its timeline — each dot's state
        // derived from the record, never from a wizard's progress variable.
        const steps: Array<{ t: string; sub: React.ReactNode; done: boolean | null }> = [
          {
            t: "Name the instance",
            sub: <>What it covers, and in which languages{inst.name ? <> · named “{inst.name}” at deploy time</> : " · not named — CREST_INSTANCE_* configuration"}</>,
            done: Boolean(inst.name),
          },
          {
            t: "Set the consent rules",
            sub: "Before a single worker registers · the floor is enforced by the infrastructure; scripts and templates are programme configuration (#59)",
            done: true,
          },
          {
            t: "Appoint an Instance Administrator",
            sub: <>So organisations can be admitted{inst.operatorPartyId ? <> · <MonoShort id={inst.operatorPartyId} /></> : " · not appointed — CREST_OPERATOR_PARTY_ID names the party"}</>,
            done: Boolean(inst.operatorPartyId),
          },
          {
            t: "Admit the first organisation",
            sub: <>
              Which registers itself, then asks for terms
              {approved === null
                ? " · the queue answers the operator only"
                : approved > 0
                  ? ` · ${approved} admitted${undecided ? ` · ${undecided} waiting on your decision` : ""}`
                  : undecided
                    ? ` · none yet — ${undecided} waiting on your decision in the queue`
                    : " · none yet — the door is open"}
            </>,
            done: approved === null ? null : approved > 0,
          },
        ];
        const dot = (done: boolean | null) =>
          done ? "var(--ok, #00703C)" : done === null ? "#B9B9B9" : "#C84C0E";
        return (
          <>
            <Title t={"Let\u2019s set up CREST for " + (inst.name || "this deployment")} />
            <Lede>
              Four steps to a working deployment — your state or country's own installation of CREST. There is no
              wizard here: each step is deploy-time configuration, and the dots show what this deployment already
              decided, read live from <span className="mono">GET /v1/instance</span>.
            </Lede>
            <div style={{ margin: "6px 0 2px" }}>
              {steps.map((st, i) => (
                <div key={i} style={{ display: "flex", gap: 14 }}>
                  <div style={{ display: "flex", flexDirection: "column", alignItems: "center", width: 12 }}>
                    <span style={{ flex: "none", width: 11, height: 11, borderRadius: "50%", background: dot(st.done), marginTop: 5 }} />
                    {i < steps.length - 1 ? (
                      <span style={{ flex: 1, width: 2, minHeight: 26, background: st.done ? dot(st.done) : "#DDDDDD", opacity: st.done ? 0.55 : 1 }} />
                    ) : null}
                  </div>
                  <div style={{ paddingBottom: i < steps.length - 1 ? 18 : 0 }}>
                    <div style={{ font: "500 14px/1.35 Roboto, system-ui, sans-serif" }}>{st.t}</div>
                    <div className="muted" style={{ marginTop: 2 }}>{st.sub}</div>
                  </div>
                </div>
              ))}
            </div>
            <Callout kind="teal">
              You are not creating an organisation. An instance is the deployment itself — a country's or a state's
              installation. Personal data collected here never leaves it.
            </Callout>
            <div style={{ borderTop: "1px solid var(--line, #E4E4E4)", marginTop: 4, paddingTop: 14, display: "flex", justifyContent: "flex-end", gap: 10 }}>
              <button
                className="btn secondary"
                disabled
                title="Drawn in the reference and not built: nothing imports a deployment's identity, because an identity that could be imported could collide on the append-only log."
              >
                Import from another instance
              </button>
              <button className="btn dominant" id="g1-begin" onClick={() => nav("/instance/covers")}>
                Begin
              </button>
            </div>
            <p className="muted" style={{ margin: 0 }}>
              "Import from another instance" is deliberately inert — an importable identity could collide on the
              append-only log.
            </p>
          </>
        );
      }}
    </LoadFrame>
  );
}

// g1_2 — what this instance covers. The reference's form layout, read-only by
// design: the values are deploy-time L1 configuration (the layering test), so
// each "field" is a read — real where the self-description publishes it,
// honestly named as unpublished configuration where it does not (#70).
function CoverField(p: { label: string; value: React.ReactNode; caption?: React.ReactNode; muted?: boolean; wide?: boolean }) {
  return (
    <div style={{ gridColumn: p.wide ? "1 / -1" : undefined }}>
      <div className="muted" style={{ font: "500 10.5px/1 Roboto, system-ui, sans-serif", letterSpacing: ".9px", textTransform: "uppercase", marginBottom: 6 }}>
        {p.label}
      </div>
      <div
        style={{
          border: "1px solid var(--line, #DDDDDD)",
          borderRadius: 8,
          padding: "10px 12px",
          background: "var(--card, transparent)",
          font: "400 13.5px/1.4 Roboto, system-ui, sans-serif",
          ...(p.muted ? { color: "var(--muted, #787878)", fontStyle: "italic" } : {}),
        }}
      >
        {p.value}
      </div>
      {p.caption ? <div className="muted" style={{ marginTop: 5 }}>{p.caption}</div> : null}
    </div>
  );
}

export function G1Covers() {
  const r = useLoad(loadInstance);
  return (
    <LoadFrame r={r}>
      {({ inst }) => (
        <>
          <Title t="What this instance covers" />
          <Lede>
            A name, a jurisdiction, and the languages a worker can be spoken to in. All three are hard to change later
            — and here none is editable at all: they are deploy-time configuration, read back rather than offered as
            an edit that would change nothing. The identity block itself is on the Self-description view.
          </Lede>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "16px 20px", maxWidth: 780 }}>
            <CoverField label="Instance name" value={inst.name || "—"} caption={inst.instanceId || inst.id} />
            <CoverField label="Jurisdiction" value="deploy-time configuration — not published" muted />
            <CoverField
              label="Identity anchor"
              value="the deployment's OIDC issuer (§4.1) — which anchor is not published"
              muted
              caption="Which external system identity binds to. Not operated by CREST."
            />
            <CoverField
              label="Data residency"
              value="where the deployment's stores run — placement, not an API fact"
              muted
              caption="Personal data never leaves this instance."
            />
            <CoverField
              label="Worker-facing languages"
              value="programme configuration (L2) — the worker faces carry the words"
              muted
              wide
              caption="A worker who cannot be addressed in a language they read is a worker who cannot meaningfully consent."
            />
          </div>
          <Callout kind="grey">
            Changing the identity anchor after workers exist would orphan every identity bound to the old one. There is
            no migration path designed for this.
          </Callout>
          <OpenNote>
            The four muted fields are the reference's remaining decisions. They are real decisions of a real
            deployment, but <span className="mono">GET /v1/instance</span> (#70) does not publish them, so this screen
            names the gap instead of inventing values — and the reference's "Save and continue" has nothing to save,
            so the button walks on.
          </OpenNote>
          <div style={{ borderTop: "1px solid var(--line, #E4E4E4)", marginTop: 4, paddingTop: 14, display: "flex", justifyContent: "flex-end", gap: 10 }}>
            <WalkButtons back="/instance/setup" next="/instance/consent" />
          </div>
        </>
      )}
    </LoadFrame>
  );
}

// g1_3 — consent rules, before the first worker. The reference's checkbox
// frame, each box's state derived from what the infrastructure actually
// enforces — the three enforced rules render checked and locked (a floor you
// could untick would not be a floor), the fourth renders off because the
// mechanism it would switch does not exist.
function ConsentRule(p: { on: boolean; locked?: boolean; title: string; sub: React.ReactNode }) {
  return (
    <div style={{ display: "flex", gap: 12, alignItems: "flex-start", marginBottom: 18 }}>
      <span
        aria-checked={p.on}
        role="checkbox"
        title={p.locked ? "Enforced by the infrastructure — not a setting" : undefined}
        style={{
          flex: "none", width: 18, height: 18, borderRadius: 4, marginTop: 1,
          display: "inline-flex", alignItems: "center", justifyContent: "center",
          border: p.on ? "none" : "2px solid #B9B9B9",
          background: p.on ? "#C84C0E" : "transparent",
          color: "#fff", font: "700 13px/1 Roboto, system-ui, sans-serif",
        }}
      >
        {p.on ? "\u2713" : ""}
      </span>
      <div>
        <div style={{ font: "500 14px/1.35 Roboto, system-ui, sans-serif" }}>{p.title}</div>
        <div className="muted" style={{ marginTop: 2 }}>{p.sub}</div>
      </div>
    </div>
  );
}

export function G1Consent() {
  return (
    <>
      <Title t="Consent rules, before the first worker" />
      <Lede>
        These are set once for the instance and inherited by every project. A project can be stricter; it cannot be
        looser. Here they are not settings at all: the checked rules are the floor the infrastructure itself enforces
        — which is why they cannot be unticked — and the words of the ask are deployment configuration (#59).
      </Lede>
      <div style={{ maxWidth: 760 }}>
        <ConsentRule
          on
          locked
          title="A worker may withdraw consent at any time"
          sub="Withdrawal stops future disclosure and new evidence collection. It does not delete credentials already issued, and never unwinds a payment already made."
        />
        <ConsentRule
          on
          locked
          title="A worker may see every verification of their own record"
          sub="Who checked, when, and what they were shown — every check reaches the worker's own audit trail (settled at G1, #9)."
        />
        <ConsentRule
          on
          locked
          title="Disclosure is per share, never standing"
          sub="A verifier gets one scoped view, not an open account — presentation consent is captured per share (#189)."
        />
        <ConsentRule
          on={false}
          title="Projects may request a broader default"
          sub="Off — and not a switch: no mechanism exists for a project to widen disclosure below the instance rule, so there is nothing to turn on."
        />
        <div style={{ marginTop: 6 }}>
          <div className="muted" style={{ font: "500 10.5px/1 Roboto, system-ui, sans-serif", letterSpacing: ".9px", textTransform: "uppercase", marginBottom: 6 }}>
            Data protection / consent officer
          </div>
          <div style={{ border: "1px solid var(--line, #DDDDDD)", borderRadius: 8, padding: "10px 12px", font: "400 13.5px/1.4 Roboto, system-ui, sans-serif", color: "var(--muted, #787878)", fontStyle: "italic" }}>
            Not yet appointed — no record names one; the appointment is recordless in this deployment
          </div>
          <div className="muted" style={{ marginTop: 5 }}>
            This role owns these rules from here on. Instance Administrator sets them; Data Protection / Consent
            Officer maintains them.
          </div>
        </div>
      </div>
      <TermsPublisher />
      <Callout kind="teal">
        Set before the first worker registers, deliberately. Retro-fitting consent onto records already collected under
        a different rule has no clean answer.
      </Callout>
      <OpenNote>
        Every captured consent has an artefact the worker can hear back —{" "}
        <span className="mono">GET /v1/consents/{"{id}"}/artefact</span>. Editing consent scripts and message templates
        from a console is not built; templates are deployment configuration (#59).
      </OpenNote>
      <div style={{ borderTop: "1px solid var(--line, #E4E4E4)", marginTop: 4, paddingTop: 14, display: "flex", justifyContent: "flex-end" }}>
        <WalkButtons back="/instance/covers" next="/instance/invite" />
      </div>
    </>
  );
}

// g1_5 — inviting the first organisation. The reference's frame, the
// primitive's honest refusal: see the OpenNote.
export function G1Invite() {
  return (
    <>
      <Title t="Inviting the first organisation" />
      <Lede>
        The reference draws the instance sending a named person an invitation to bring the first organisation in.
        This deployment's invitation primitive (#182) is a different act: a <em>project's</em> offer of a scoped
        grant to an organisation that already exists as a party, acceptable only once its registration is approved
        (Blueprint §15 J1). An instance-level "come and exist" invitation has no primitive — and g2_5 records the
        decision above it as still open: administrative creation, self-registration, or vouching.
      </Lede>
      <CardTitled t="What the reference asks for">
        <KVR rows={[
          ["Organisation", "e.g. Ministry of Health"],
          ["Category", "e.g. Delivery organisation"],
          ["Signatory", "a named person"],
          ["Role", "their role in the organisation"],
          ["Work email", "where the invitation would go"],
        ]} />
        <OpenNote>
          No Send button, on purpose. Sending would need an invitation object this deployment does not have, and
          faking the send would settle an open design decision by accident. What is real today: the organisation
          registers itself at the open door (<span className="mono">#/onboard</span>), and its application lands in
          this instance's admission queue for a person to look at. The gap is recorded as design finding #185.
        </OpenNote>
      </CardTitled>
      <WalkButtons back="/instance/consent" next="/instance/services" />
    </>
  );
}

// g1_6 — the services behind all of it: a real /healthz sweep.
export function G1Services() {
  const r = useLoad(async () => {
    const names = ["parties", "definitions", "evidence", "confirmation", "verification", "payments"] as const;
    const health = await Promise.all(
      names.map((n) => api.get(n, "/healthz").then(() => ({ n, ok: true }), () => ({ n, ok: false }))),
    );
    return { health };
  });
  return (
    <LoadFrame r={r}>
      {({ health }) => (
        <>
          <Title t="The services behind all of it" />
          <Lede>
            Six service doors, each answering its own <span className="mono">/healthz</span> live — this sweep is a
            real read, not a status page. Services, not roles: what a deployment runs is infrastructure; who may do
            what is granted in the registry and read back.
          </Lede>
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
          <WalkButtons back="/instance/invite" next="/admissions" nextLabel="Done — awaiting the organisation" />
        </>
      )}
    </LoadFrame>
  );
}

// People & roles — the reference rail's fifth entry, honestly unbuilt at the
// instance level.
export function G1People() {
  return (
    <>
      <Title t="People & roles" />
      <Lede>
        Instance-level people administration has no surface here. A role in CREST is an authorization granted in the
        registry and read back — the project console's People &amp; roles screens are where grants are made, and{" "}
        <span className="mono">GET /v1/authorizations</span> is where they are read. Nothing at the instance level
        grants anything.
      </Lede>
    </>
  );
}

// ── g4_1 — the admission queue, real ───────────────────────────────────────

const loadQueue = async () => {
  const [regs, terms] = await Promise.all([
    api.get("parties", "/v1/registrations"),
    api.get("parties", "/v1/terms-requests").catch(() => ({ requests: [] })),
  ]);
  return {
    regs: (regs.registrations || []) as Array<{
      partyId: string; state: string; displayName?: string; appliedAt?: string;
      decidedBy?: string; decidedAt?: string; attributes?: Record<string, unknown>;
    }>,
    terms: (terms.requests || []) as Array<{
      id: string; partyId: string; termsId: string; termsVersion: number; state: string; submittedAt?: string;
    }>,
  };
};

export function Admissions() {
  const nav = useNavigate();
  const r = useLoad(loadQueue);
  return (
    <LoadFrame r={r}>
      {({ regs, terms }) => {
        const pending = regs.filter((x) => x.state === "APPLIED" || x.state === "TERMS_ACCEPTED");
        const decided = regs.filter((x) => x.state === "APPROVED" || x.state === "REJECTED");
        return (
          <>
            <Title t="Requests a person has to look at" extra={<Chip kind={pending.length ? "warn" : "ok"}>{pending.length + " waiting"}</Chip>} />
            <Lede>
              Registrations from <span className="mono">GET /v1/registrations</span>, terms requests from{" "}
              <span className="mono">GET /v1/terms-requests</span> — both live reads of what still needs a named
              person's decision.
            </Lede>
            <CardTitled t="Organisations awaiting a decision">
              <Tbl
                heads={["Organisation", "State", "Applied", ""]}
                rows={pending.map((x) => [
                  <>{x.displayName || "—"} <MonoShort id={x.partyId} /></>,
                  <Chip kind={x.state === "TERMS_ACCEPTED" ? "info" : "plain"}>{x.state}</Chip>,
                  when(x.appliedAt),
                  <button className="btn secondary" data-open={x.partyId} style={{ width: "auto", padding: "7px 12px" }}
                    onClick={() => nav("/admissions/" + encodeURIComponent(x.partyId))}>
                    Open this request
                  </button>,
                ])}
                empty="Nothing is waiting. An empty queue is a normal state, not a broken one."
              />
            </CardTitled>
            <CardTitled t="Terms requests sent for review">
              <Tbl
                heads={["Request", "Organisation", "Terms", "Submitted"]}
                rows={terms.map((t) => [
                  <MonoShort id={t.id} />,
                  <MonoShort id={t.partyId} />,
                  <><Mono>{t.termsId}</Mono> v{t.termsVersion}</>,
                  when(t.submittedAt),
                ])}
                empty="No terms request is under review."
              />
              <p className="muted" style={{ marginTop: 8 }}>
                A terms request is decided at <span className="mono">POST /v1/terms-requests/{"{id}"}/decision</span>;
                the applicant's own review screen (<span className="mono">#/onboard/review</span>) shows the verdicts
                and the decision back. A reviewer detail for these rows is not built here yet — the queue is.
              </p>
            </CardTitled>
            <Callout kind="teal" title="What is not here">
              Verifiers are not in this queue and never will be. Any third party can verify a credential without
              onboarding — the credential's signature carries the trust, not the verifier's status.
            </Callout>
            <Callout kind="green" title="Why the queue is short">
              Requests where the register confirms the organisation are approved automatically and never reach this
              screen. Registration itself is not in the queue at all, because an organisation is listed and findable
              the moment it registers.
            </Callout>
            <p className="muted">
              This deployment runs <span className="mono">REGISTRY_ORG_APPROVAL=manual</span> unless configured
              otherwise, so every registration takes a named person's decision; under{" "}
              <span className="mono">on-terms-acceptance</span> the rows above would carry{" "}
              <span className="mono">crest:policy:on-terms-acceptance</span> as their decider instead.
            </p>
            <CardTitled t="Already decided">
              <Tbl
                heads={["Organisation", "State", "Decided by", "When"]}
                rows={decided.map((x) => [
                  <>{x.displayName || "—"} <MonoShort id={x.partyId} /></>,
                  <Chip kind={x.state === "APPROVED" ? "ok" : "err"}>{x.state}</Chip>,
                  x.decidedBy ? <MonoShort id={x.decidedBy} /> : "—",
                  when(x.decidedAt),
                ])}
                empty="No decision has been made yet."
              />
            </CardTitled>
            <WalkButtons back="/instance/services" />
          </>
        );
      }}
    </LoadFrame>
  );
}

// ── g4_2 + g4_3 — one request, and the whole decision ──────────────────────

export function AdmissionDetail() {
  const { pid = "" } = useParams();
  const s = useConsole();
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [n, bump] = useState(0);
  const r = useLoad(
    () => api.get("parties", `/v1/organisations/${encodeURIComponent(pid)}/registration`),
    [pid, n],
  );
  const decide = async (approve: boolean) => {
    s.clearErr();
    setBusy(true);
    try {
      // The decider is this session's authenticated party — named in the body,
      // checked by the service against the bearer token (#89). Every approval
      // carries a decider; a rejection carries a reason as well.
      await api.post("parties", `/v1/organisations/${encodeURIComponent(pid)}/decision`, {
        approve,
        decidedBy: s.me?.partyId,
        reason: reason.trim(),
      });
      bump((x) => x + 1);
    } catch (e) {
      s.fail(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <LoadFrame r={r}>
      {(reg) => {
        const a = (reg.attributes || {}) as Record<string, string>;
        const open = reg.state === "APPLIED" || reg.state === "TERMS_ACCEPTED";
        return (
          <>
            <Title t={reg.displayName || "Organisation under review"} extra={<Chip kind={reg.state === "APPROVED" ? "ok" : reg.state === "REJECTED" ? "err" : "info"}>{reg.state}</Chip>} />
            <CardTitled t="What was declared">
              <KVR rows={[
                ["Legal entity", (a.kind ? a.kind + " organisation" : "—") + (a.country ? " · " + a.country : "")],
                ["Signatory", a.contactPerson || "—"],
                ["Sector", a.sector || "—"],
                ["Party", <Mono>{reg.partyId}</Mono>],
                ["Applied", when(reg.appliedAt)],
                ["Terms requested", reg.termsId ? <><Mono>{reg.termsId}</Mono> v{reg.termsVersion} — accepted {when(reg.acceptedAt)}</> : "no terms accepted yet"],
              ]} />
            </CardTitled>
            <Callout kind="teal" title="What this does not prove">
              A register confirms that a body exists. Neither it nor the domain check establishes that the person
              submitting this speaks for that body, and nothing in this deployment does.
            </Callout>
            {open ? (
              <CardTitled t="Approving the request">
                <p className="body-2" style={{ marginBottom: 10 }}>
                  Approval is the whole decision: it publishes the organisation to the registry in the same
                  transaction, under your name as decider. It is refused until terms are accepted, and an
                  organisation can never decide its own application. A rejection requires a reason — a closed door
                  with no explanation is the dead end this system refuses to leave anyone at.
                </p>
                <label className="muted" htmlFor="decide-reason">Reason (required to reject, recorded either way)</label>
                <input id="decide-reason" name="decidereason" value={reason} onChange={(e) => setReason(e.target.value)}
                  placeholder="e.g. confirmed against the state register" style={{ margin: "6px 0 10px" }} />
                <div style={{ display: "flex", gap: 8 }}>
                  <button id="approve-registration" className="btn" style={{ width: "auto", padding: "9px 16px" }} disabled={busy} onClick={() => decide(true)}>
                    Approve and publish
                  </button>
                  <button id="reject-registration" className="btn secondary" style={{ width: "auto", padding: "9px 16px" }} disabled={busy} onClick={() => decide(false)}>
                    Reject, with the reason
                  </button>
                </div>
                <p className="muted" style={{ marginTop: 8 }}>
                  The reference's button says "Approve and issue the key". Sender keys are not built in this
                  deployment; what approval issues is the public registry fact.
                </p>
              </CardTitled>
            ) : (
              <CardTitled t="The decision">
                <KVR rows={[
                  ["decided", reg.state],
                  ["decided by", reg.decidedBy ? <MonoShort id={reg.decidedBy} /> : "—"],
                  ["when", when(reg.decidedAt)],
                  ["reason", reg.reason || "—"],
                ]} />
              </CardTitled>
            )}
            <Callout kind="green" title="If it is ever withdrawn">
              A withdrawn key is never reinstated — a new one is issued instead. There is no middle state in which an
              organisation is technically able to send but not permitted to.
            </Callout>
            <Callout kind="grey" title="What happens next">
              Who acts next: nobody, necessarily. An approved organisation sits in the directory until a project
              configurator goes looking for a partner like it. That may be tomorrow or never, and either is a normal
              state.
            </Callout>
            <WalkButtons back="/admissions" />
          </>
        );
      }}
    </LoadFrame>
  );
}

// Terms an organisation can be on. On a clean deployment there are none, and
// nothing can be admitted until the operator publishes a set: an admission
// is an acceptance of a named version, and "whatever was current" is not a
// fact a verifier can walk back to. The permissions are opaque strings to
// the core; the ones offered here are the functions this console's own
// surfaces grant, and a deployment may type any other.
const TERMS_PERMISSIONS = [
  "submit-work-evidence",
  "specify-definition",
  "ratify-definition",
  "act-for-party",
  "register-workers",
  "submit-evidence",
  "resolve-unclear-evidence",
];

function TermsPublisher() {
  const [gen, setGen] = useState(0);
  const [name, setName] = useState("");
  const [perms, setPerms] = useState<string[]>(["submit-work-evidence"]);
  const [extra, setExtra] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const r = useLoad(async () => (await api.get("parties", "/v1/terms")).terms || [], [gen]);
  const toggle = (p: string) => setPerms((cur) => (cur.includes(p) ? cur.filter((x) => x !== p) : [...cur, p]));
  const publish = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    setErr("");
    try {
      const all = Array.from(new Set([...perms, ...extra.split(",").map((s) => s.trim()).filter(Boolean)]));
      await api.post("parties", "/v1/terms", { name: name.trim(), permissions: all });
      setName("");
      setGen((g) => g + 1);
    } catch (e) {
      setErr(String((e as Error)?.message || e));
    }
    setBusy(false);
  };
  return (
    <LoadFrame r={r}>
      {(terms: Array<{ id: string; name?: string; version?: number; permissions?: string[]; publishedAt?: string }>) => (
        <CardTitled t="Terms an organisation can be on">
          <p className="body-2">
            An organisation is admitted onto a named version of a set of terms, and the functions its people can be
            granted are capped by that set. Published, never edited: a revision is a new version.
          </p>
          <Tbl
            heads={["Terms", "Version", "Permissions", "Published"]}
            rows={terms.map((t) => [
              <span data-terms-row={t.id}>{t.name || t.id}</span>,
              String(t.version ?? ""),
              (t.permissions || []).join(", "),
              when(t.publishedAt),
            ])}
            empty="No terms are published yet. Nothing can be admitted until a set exists — this deployment has not said what an organisation may be trusted with."
          />
          <form data-terms-form onSubmit={publish} style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 12, maxWidth: 640 }}>
            <label className="field">
              <span className="eyebrow">Name</span>
              <input required value={name} onChange={(e) => setName(e.target.value)} data-field="terms-name" placeholder="Standard delivery" />
            </label>
            <div>
              <span className="eyebrow">Permissions this set allows</span>
              <div style={{ display: "flex", flexWrap: "wrap", gap: "6px 14px", marginTop: 6 }}>
                {TERMS_PERMISSIONS.map((p) => (
                  <label key={p} className="body-2" style={{ display: "flex", gap: 6, alignItems: "center" }}>
                    <input type="checkbox" checked={perms.includes(p)} onChange={() => toggle(p)} data-perm={p} />
                    <span className="mono">{p}</span>
                  </label>
                ))}
              </div>
            </div>
            <label className="field">
              <span className="eyebrow">Other permissions, comma-separated</span>
              <input value={extra} onChange={(e) => setExtra(e.target.value)} data-field="terms-extra" placeholder="the deployment's own function names" />
            </label>
            {err ? <div className="errbar">{err}</div> : null}
            <div>
              <button className="btn dominant" type="submit" disabled={busy || perms.length + extra.trim().length === 0} data-act="publish-terms">
                {busy ? "Publishing…" : "Publish these terms"}
              </button>
            </div>
          </form>
          <p className="muted" style={{ marginTop: 8 }}>
            Only the instance operator can publish — the registry checks the caller against the published
            self-description, and refuses anybody else.
          </p>
        </CardTitled>
      )}
    </LoadFrame>
  );
}
