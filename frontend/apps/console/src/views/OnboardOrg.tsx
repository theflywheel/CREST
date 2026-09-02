// The G-2 screens after registration stands (g2_5–g2_10): the organisation's
// own standing view — invitations in, wider terms out. Everything here is a
// real read or a real write against the parties service, acting AS the
// organisation the onboarding flow registered (ensureOrgSession): the
// invitation inbox is GET /v1/organisations/{id}/invitations, an answer is
// POST /v1/invitations/{id}/decision, a wider-terms request walks
// POST .../terms-requests → PUT .../documents → POST .../submit →
// POST .../withdraw. Nothing is simulated; refusals (accepting before the
// registry approved you, declining without a reason) are shown as the honest
// answers they are.
//
// Which rule could this break: verification — an invitation acceptance mints
// the partner grant verifiers later walk back to, so every write here goes
// through the service's own gates (terms-as-ceiling, APPROVED-only) and the
// screens render the service's refusal rather than papering over it. Evidence,
// confirmation and payments are untouched.
import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { api } from "@crest/api";
import { Callout, Chip, ErrBar, GridTable, KV, NextBlock, OpenNote, Sidecar, Stat } from "@crest/ui";
import { OnboardFrame, ensureOrgSession, readOnboarding, updateOnboarding } from "./Onboard";

// ── The reference's organisation rail: Your organisation · Status ·
//    Invitations · People. "People" has no G-2 surface — it renders as the
//    named later journey it is, never as a fake door.
const ORG_RAIL: Array<{ label: string; to?: string }> = [
  { label: "Your organisation", to: "/onboard/status" },
  { label: "Status", to: "/onboard/wider" },
  { label: "Invitations", to: "/onboard/invited" },
  { label: "People" },
];

function OrgFrame(props: { active: string; title: string; children: React.ReactNode }) {
  const ob = readOnboarding();
  const loc = useLocation();
  return (
    <div className="console-shell">
      <div className="appbar">
        <span className="mark" />
        <span className="t">CREST Console</span>
        <span className="who">
          <span className="who-label">
            {ob?.contactName ? `${ob.contactName} · Onboarding Authorising Signatory` : "Onboarding Authorising Signatory"}
          </span>
        </span>
      </div>
      <div className="console-body">
        <nav className="sidebar">
          <span className="cap">{ob?.name || "Your organisation"}</span>
          {ORG_RAIL.map((e) =>
            e.to ? (
              <Link key={e.label} to={e.to} className={e.label === props.active || loc.pathname === e.to ? "active" : ""}>
                {e.label}
              </Link>
            ) : (
              <button key={e.label} disabled style={{ cursor: "default", color: "var(--text-3)" }}>
                {e.label} — a later journey
              </button>
            ),
          )}
        </nav>
        <main className="pane">
          <div className="screen" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
            <h2 className="scr-title">{props.title}</h2>
            {props.children}
          </div>
        </main>
      </div>
    </div>
  );
}

const NoApplication = (props: { frame?: "onboard" }) =>
  props.frame === "onboard" ? (
    <OnboardFrame step={4} counter={false} title="No application in progress">
      <p className="body-2">
        <Link to="/onboard">Start one.</Link>
      </p>
    </OnboardFrame>
  ) : (
    <OrgFrame active="" title="No application in progress">
      <p className="body-2">
        <Link to="/onboard">Start one.</Link>
      </p>
    </OrgFrame>
  );

const fmtDay = (iso?: string) =>
  iso
    ? new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "long", year: "numeric" })
    : "—";

// ── g2_5 · This registration stands alone ──────────────────────────────────
// The registration rail's frame (the reference draws this inside the
// Register → Terms → Certificates → Done flow, after Done, with no counter).
// The invitation count is a real read of the organisation's inbox.
export function OnboardStandalone() {
  const nav = useNavigate();
  const ob = readOnboarding();
  const [inv, setInv] = useState<any[] | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!ob) return;
    ensureOrgSession(ob.orgId)
      .then(() => api.get("parties", `/v1/organisations/${ob.orgId}/invitations`))
      .then((d: any) => setInv(d.invitations || []), (e: any) => setErr(String(e?.message || e)));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!ob) return <NoApplication frame="onboard" />;
  const waiting = (inv || []).filter((i) => i.state === "SENT");
  return (
    <OnboardFrame step={4} counter={false} title="This registration stands alone" who={ob.contactName}>
      {err ? <ErrBar>{err}</ErrBar> : null}
      <div className="pane-cols">
        <Callout kind="green" title="If a project already invited you">
          The invitation was waiting for a registered organisation to attach to. It is here now. Nothing had to happen
          in a particular order.
          {inv === null ? "" : waiting.length ? ` ${waiting.length} waiting in your inbox.` : ""}
        </Callout>
        <Callout kind="teal" title="If none has">
          You are listed under your sector, in your country, on the terms you accepted. A configurator looking for a
          partner will find you. Sitting here uninvited is normal, not stalled.
        </Callout>
      </div>
      <OpenNote>
        <b style={{ display: "block", marginBottom: 6, textTransform: "uppercase", fontSize: 10, letterSpacing: ".9px" }}>
          One decision above these screens is still open
        </b>
        <b>Still being decided:</b> whether organisations register themselves at all. Three ways are on the table —
        the deployment creates them administratively; they register and request terms, which is what these screens
        draw; or an already-recognised organisation vouches for them. What settles it is whether an unfamiliar
        verifier needs to know <b>which organisation</b> stood behind the work, or only <b>which country</b>.
      </OpenNote>
      <div className="btn-row" style={{ maxWidth: 560 }}>
        <button className="btn secondary" onClick={() => nav("/onboard/wider")}>
          Apply for a status
        </button>
        <button className="btn secondary" id="to-invitations" onClick={() => nav("/onboard/invited")}>
          Accept an invitation
        </button>
        <button className="btn dominant" onClick={() => nav("/onboard")}>
          Done
        </button>
      </div>
    </OnboardFrame>
  );
}

// ── g2_6 · You need wider terms to put your name to something ──────────────
// The left column is the organisation's REAL accepted permissions, read back
// from its registration; the right column is each other published set's
// additional grants. Requesting opens a DRAFT terms-request — the service
// refuses it until the registry approved the organisation, and that refusal
// is shown, not smoothed over.
export function OnboardWider() {
  const nav = useNavigate();
  const ob = readOnboarding();
  const [reg, setReg] = useState<any | null>(null);
  const [terms, setTerms] = useState<any[] | null>(null);
  const [pick, setPick] = useState<string>("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!ob) return;
    ensureOrgSession(ob.orgId)
      .then(() =>
        Promise.all([
          api.get("parties", `/v1/organisations/${ob.orgId}/registration`),
          api.get("parties", "/v1/terms"),
        ]),
      )
      .then(([r, t]: any[]) => {
        setReg(r);
        setTerms(t.terms || []);
      }, (e: any) => setErr(String(e?.message || e)));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!ob) return <NoApplication />;
  const held = terms?.find((t: any) => t.id === reg?.termsId && t.version === reg?.termsVersion);
  const others = (terms || []).filter((t: any) => !(t.id === reg?.termsId && t.version === reg?.termsVersion));
  const picked = others.find((t: any) => `${t.id}@${t.version}` === pick) || others[0];
  const request = async () => {
    if (!picked) {
      setErr("No other published set exists to ask for — the operator publishes terms sets; nothing to request yet.");
      return;
    }
    setBusy(true);
    setErr("");
    try {
      const out = await api.post("parties", `/v1/organisations/${ob.orgId}/terms-requests`, {
        termsId: picked.id,
        termsVersion: picked.version,
      });
      updateOnboarding({ requestId: out.id });
      nav("/onboard/documents");
    } catch (e: any) {
      setErr(String(e?.message || e));
      setBusy(false);
    }
  };
  return (
    <OrgFrame active="Status" title="You need wider terms to put your name to something">
      <p className="body-2">
        You have been registered since {fmtDay(reg?.appliedAt)}. Everything below is optional until it isn't.
      </p>
      {err ? <ErrBar>{err}</ErrBar> : null}
      <div className="pane-cols">
        <div>
          <GridTable cols="1fr 1fr" head={["What your terms allow today", "What needs wider terms"]}>
            <div className="g-row">
              <span>{held ? (held.permissions || []).join(" · ") : reg?.acceptedAt ? `${reg.termsId} v${reg.termsVersion}` : "No terms accepted yet"}</span>
              <span>
                {others.length
                  ? others
                      .map((t: any) => (t.permissions || []).filter((p: string) => !(held?.permissions || []).includes(p)).join(" · ") || t.name)
                      .join(" · ")
                  : "Whatever a wider published set grants — none is published on this deployment yet"}
              </span>
            </div>
          </GridTable>
          <div style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 12 }}>
            {others.map((t: any) => {
              const key = `${t.id}@${t.version}`;
              const on = picked && `${picked.id}@${picked.version}` === key;
              return (
                <button
                  key={key}
                  type="button"
                  className={"optcard" + (on ? " on" : "")}
                  aria-pressed={!!on}
                  onClick={() => setPick(key)}
                  data-terms={key}
                >
                  <div className="o-head">
                    <span className="o-t">
                      {t.name}, version {t.version}
                    </span>
                    <Chip sm>declares documents at request</Chip>
                  </div>
                  <div className="o-s">Grants: {(t.permissions || []).join(" · ") || "—"}</div>
                </button>
              );
            })}
            {terms !== null && !others.length ? (
              <Sidecar>
                This deployment has published one terms set, and you are on it. Wider sets are published by the
                operator; the request door below is real and will name that refusal if used before one exists.
              </Sidecar>
            ) : null}
          </div>
        </div>
        <Callout kind="green" title="When you can ignore this entirely">
          If you never issue a credential and never pay anybody, you never need either. A body that only submits
          evidence for somebody else to credential can work for years on the terms it has.
        </Callout>
      </div>
      <div className="btn-row" style={{ maxWidth: 440 }}>
        <button className="btn secondary" onClick={() => nav("/onboard/standalone")}>
          Not now
        </button>
        <button className="btn dominant" id="requestwider" disabled={busy} onClick={request}>
          {busy ? "Opening the request…" : "Request wider terms"}
        </button>
      </div>
    </OrgFrame>
  );
}

// Reads the organisation's newest terms-request (or the one the session just
// opened), whole: state, documents, trail, checks.
async function loadNewestRequest(orgId: string, requestId?: string) {
  let id = requestId;
  if (!id) {
    const list = await api.get("parties", `/v1/organisations/${orgId}/terms-requests`);
    id = (list.requests || [])[0]?.id;
  }
  if (!id) return null;
  return api.get("parties", `/v1/terms-requests/${id}`);
}

// ── g2_7 · What we need to see ─────────────────────────────────────────────
// Qualification documents are DECLARED, never uploaded: {kind, ref, hash} —
// a reference into the deployment's own document custody. This screen has no
// file input on purpose, and never will: raw identity documents (anyone's)
// do not pass through CREST.
export function OnboardDocuments() {
  const nav = useNavigate();
  const ob = readOnboarding() as any;
  const [req, setReq] = useState<any | null>(null);
  const [rows, setRows] = useState<Array<{ kind: string; ref: string; hash: string }>>([
    { kind: "", ref: "", hash: "" },
  ]);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState("");
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!ob) return;
    ensureOrgSession(ob.orgId)
      .then(() => loadNewestRequest(ob.orgId, ob.requestId))
      .then((whole: any) => {
        if (!whole) return;
        setReq(whole.request);
        if ((whole.request.documents || []).length)
          setRows(whole.request.documents.map((d: any) => ({ kind: d.kind, ref: d.ref, hash: d.hash || "" })));
      }, (e: any) => setErr(String(e?.message || e)));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!ob) return <NoApplication />;
  const docs = () =>
    rows
      .filter((r) => r.kind.trim() && r.ref.trim())
      .map((r) => ({ kind: r.kind.trim(), ref: r.ref.trim(), ...(r.hash.trim() ? { hash: r.hash.trim() } : {}) }));
  const save = async () => {
    if (!req) return setErr("No open request — open one from the Status screen first.");
    setBusy(true);
    setErr("");
    setSaved("");
    try {
      const out = await api.put("parties", `/v1/terms-requests/${req.id}/documents`, { documents: docs() });
      setReq(out);
      setSaved(`Draft saved — ${(out.documents || []).length} document reference(s) on request ${out.id}.`);
    } catch (e: any) {
      setErr(String(e?.message || e));
    }
    setBusy(false);
  };
  const submit = async () => {
    if (!req) return setErr("No open request — open one from the Status screen first.");
    setBusy(true);
    setErr("");
    try {
      await api.put("parties", `/v1/terms-requests/${req.id}/documents`, { documents: docs() });
      await api.post("parties", `/v1/terms-requests/${req.id}/submit`);
      nav("/onboard/review");
    } catch (e: any) {
      setErr(String(e?.message || e));
      setBusy(false);
    }
  };
  const edit = (i: number, k: "kind" | "ref" | "hash", v: string) =>
    setRows(rows.map((r, j) => (j === i ? { ...r, [k]: v } : r)));
  return (
    <OrgFrame active="Status" title="What we need to see">
      <p className="body-2">
        Requesting {req ? `${req.termsId} v${req.termsVersion}` : "wider terms"} · {ob.name}
      </p>
      {err ? <ErrBar>{err}</ErrBar> : null}
      {saved ? <Sidecar ok>{saved}</Sidecar> : null}
      <div className="pane-cols">
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <GridTable cols="1fr 1.4fr 1fr" head={["What (kind)", "Reference in your custody", "Hash, optional"]}>
            {rows.map((r, i) => (
              <div className="g-row" key={i}>
                <span>
                  <input
                    name={`dockind${i}`}
                    placeholder="e.g. registration-certificate"
                    value={r.kind}
                    onChange={(e) => edit(i, "kind", e.target.value)}
                  />
                </span>
                <span>
                  <input
                    name={`docref${i}`}
                    placeholder="where your custody keeps it"
                    value={r.ref}
                    onChange={(e) => edit(i, "ref", e.target.value)}
                  />
                </span>
                <span>
                  <input
                    name={`dochash${i}`}
                    placeholder="pins what was seen"
                    value={r.hash}
                    onChange={(e) => edit(i, "hash", e.target.value)}
                  />
                </span>
              </div>
            ))}
          </GridTable>
          <button
            type="button"
            className="btn secondary inline"
            onClick={() => setRows([...rows, { kind: "", ref: "", hash: "" }])}
          >
            Add a document reference
          </button>
          <Sidecar>
            Declared, never uploaded: a document here is a kind from this deployment's own taxonomy, a reference into
            its own document custody, and optionally a hash. The registry stores no document content — a raw
            certificate, identity paper or letterhead never passes through this screen.
          </Sidecar>
        </div>
        <Callout kind="teal" title="Read the last row">
          The last row is the one people miss. You already hold worker records — names, phone numbers, identity
          numbers — because registering workers was in what you could do from day one. Naming who answers for that
          data was deferred, not waived, and this is where it comes due.
        </Callout>
      </div>
      <div className="btn-row" style={{ maxWidth: 400 }}>
        <button className="btn secondary" id="savedraft" disabled={busy} onClick={save}>
          Save draft
        </button>
        <button className="btn dominant" id="submitrequest" disabled={busy} onClick={submit}>
          {busy ? "Working…" : "Submit"}
        </button>
      </div>
    </OrgFrame>
  );
}

// ── g2_8 · Sent for review ─────────────────────────────────────────────────
// The promoted terminal screen: the real request read whole, and Withdraw is
// the service's own POST — a decided request refuses it, and the refusal is
// shown.
export function OnboardReview() {
  const nav = useNavigate();
  const ob = readOnboarding() as any;
  const [req, setReq] = useState<any | null>(null);
  const [checks, setChecks] = useState<any[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const load = () =>
    loadNewestRequest(ob.orgId, ob.requestId).then((whole: any) => {
      if (whole) {
        setReq(whole.request);
        setChecks(whole.checks || []);
      }
    }, (e: any) => setErr(String(e?.message || e)));
  useEffect(() => {
    if (ob) ensureOrgSession(ob.orgId).then(load, (e: any) => setErr(String(e?.message || e)));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!ob) return <NoApplication />;
  const withdraw = async () => {
    if (!req) return;
    setBusy(true);
    setErr("");
    try {
      await api.post("parties", `/v1/terms-requests/${req.id}/withdraw`, {});
      await load();
    } catch (e: any) {
      setErr(String(e?.message || e));
    }
    setBusy(false);
  };
  return (
    <OrgFrame active="Status" title="Sent for review">
      <p className="body-2">
        {req
          ? `Request ${req.id} · ${req.termsId} v${req.termsVersion} · ${req.submittedAt ? `submitted ${fmtDay(req.submittedAt)}` : req.state.toLowerCase()}`
          : "No request on record yet."}
      </p>
      {err ? <ErrBar>{err}</ErrBar> : null}
      {req ? (
        <>
          <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
            <Stat n={<span className="mono" style={{ fontSize: 16 }}>{req.id.slice(-9)}</span>} label="your reference" />
            <Stat n="Wider terms" label="requested" />
            <Stat n={String((req.documents || []).length)} label="documents declared" />
            <Stat
              n={req.state === "SUBMITTED" ? "Open" : req.state}
              label="status"
            />
          </div>
          <div className="pane-cols">
            <NextBlock
              happened={
                <>
                  Your request was {req.state.toLowerCase()} · <b className="mono">{req.id}</b>
                </>
              }
              who={
                req.decidedBy
                  ? `Decided by ${req.decidedBy}`
                  : "The deployment operator's reviewer — a named decider is recorded on the decision, and it is never you"
              }
              when="No formal commitment exists — the review is a person's judgement, not a timer"
              told="It appears on this screen; the state above is the registry's own answer"
              ifnot="Nothing chases it. Contact the deployment operator directly — their details are on your organisation page."
            />
            <Callout kind="teal" title="What is not blocked">
              You lose nothing while this is open. Registering workers, submitting evidence and joining projects all
              carry on. The only things you still cannot do are the ones these wider terms would grant.
            </Callout>
          </div>
          {checks.length ? (
            <KV
              rows={checks.map((c: any) => [
                c.name,
                `${c.outcome} — ${c.ownerKind === "policy" ? "policy " : ""}${c.owner}${c.note ? ` (${c.note})` : ""}`,
              ])}
            />
          ) : null}
        </>
      ) : (
        <Sidecar>
          Nothing has been sent for review from this organisation. A request opens on the Status screen and lands here
          when submitted.
        </Sidecar>
      )}
      <div className="btn-row" style={{ maxWidth: 480 }}>
        <button className="btn secondary" id="withdrawrequest" disabled={busy || !req} onClick={withdraw}>
          Withdraw
        </button>
        <button className="btn dominant" onClick={() => nav("/onboard/invited")}>
          Back to your organisation
        </button>
      </div>
    </OrgFrame>
  );
}

// ── g2_9 · A project has invited you ───────────────────────────────────────
export function OnboardInvited() {
  const nav = useNavigate();
  const ob = readOnboarding() as any;
  const [inv, setInv] = useState<any[] | null>(null);
  const [reason, setReason] = useState("");
  const [question, setQuestion] = useState("");
  const [asked, setAsked] = useState("");
  const [refusal, setRefusal] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const load = () =>
    api
      .get("parties", `/v1/organisations/${ob.orgId}/invitations`)
      .then((d: any) => setInv(d.invitations || []), (e: any) => setErr(String(e?.message || e)));
  useEffect(() => {
    if (ob) ensureOrgSession(ob.orgId).then(load, (e: any) => setErr(String(e?.message || e)));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!ob) return <NoApplication />;
  const open = (inv || []).find((i: any) => i.state === "SENT");
  const decided = (inv || []).filter((i: any) => i.state !== "SENT");
  const decide = async (decision: "accepted" | "declined") => {
    if (!open) return;
    if (decision === "declined" && !reason.trim()) {
      setErr("Declining records a reason — a refusal with no explanation is a dead end, and this system does not leave people at dead ends.");
      return;
    }
    setBusy(true);
    setErr("");
    setRefusal("");
    try {
      const out = await api.post("parties", `/v1/invitations/${open.id}/decision`, {
        decision,
        ...(decision === "declined" ? { reason: reason.trim() } : {}),
      });
      if (decision === "accepted") {
        updateOnboarding({ invitationId: out.id });
        nav("/onboard/project");
        return;
      }
      setReason("");
      await load();
    } catch (e: any) {
      // "Not yet" is an answer, not an error bar: an undecided registration
      // holds the acceptance without expiring the offer, and the screen says
      // exactly that in the service's own words.
      if (e?.status === 409 && decision === "accepted") setRefusal(String(e.message || e));
      else setErr(String(e?.message || e));
    }
    setBusy(false);
  };
  const ask = async () => {
    if (!open || !question.trim()) return;
    setBusy(true);
    setErr("");
    try {
      await api.post("parties", `/v1/invitations/${open.id}/questions`, {
        askedBy: ob.orgId,
        text: question.trim(),
      });
      setAsked(`Question recorded on the offer's trail: "${question.trim()}"`);
      setQuestion("");
    } catch (e: any) {
      setErr(String(e?.message || e));
    }
    setBusy(false);
  };
  return (
    <OrgFrame active="Invitations" title="A project has invited you">
      <p className="body-2">
        {inv === null
          ? "Reading your inbox…"
          : open
            ? "One waiting. You did not apply for this and did not need to."
            : "Nothing waiting. An invitation can arrive before or after your registration is decided."}
      </p>
      {err ? <ErrBar>{err}</ErrBar> : null}
      {refusal ? (
        <OpenNote>
          <b>Not yet, not no ·</b> {refusal} The offer does not expire with the wait — it is still here once the
          registry decides.
        </OpenNote>
      ) : null}
      {asked ? <Sidecar ok>{asked}</Sidecar> : null}
      {inv && inv.length ? (
        <GridTable cols="1.2fr 1fr 2fr 0.8fr" head={["Project", "From", "What you could do, and until when", "Sent"]}>
          {[...(open ? [open] : []), ...decided].map((i: any) => (
            <div className="g-row" key={i.id}>
              <span className="mono">{i.contextId}</span>
              <span className="mono">{i.invitedBy}</span>
              <span>
                {(i.functions || []).join(" · ")} · {fmtDay(i.period?.start)} to {fmtDay(i.period?.end)}
              </span>
              <span>
                {i.state === "SENT" ? (
                  fmtDay(i.invitedAt)
                ) : (
                  <Chip sm kind={i.state === "ACCEPTED" ? "ok" : "warn"}>
                    {i.state.toLowerCase()}
                    {i.reason ? ` — ${i.reason}` : ""}
                  </Chip>
                )}
              </span>
            </div>
          ))}
        </GridTable>
      ) : null}
      <div className="pane-cols">
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <label className="field">
            <span className="eyebrow" style={{ color: "var(--text-2)" }}>If declining — the reason, recorded with the answer</span>
            <input name="declineinvreason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="required to decline" />
          </label>
          <label className="field">
            <span className="eyebrow" style={{ color: "var(--text-2)" }}>Or ask a question on the record</span>
            <input name="invquestion" value={question} onChange={(e) => setQuestion(e.target.value)} placeholder="either side can ask while the offer is open" />
          </label>
        </div>
        <Callout kind="green" title="Why you can accept this safely">
          Everything offered here is inside what you could already do. A project can grant less than your terms allow
          and can never grant more, so an invitation cannot surprise you with a capability you never signed up for. It
          can be narrower in three ways at once: fewer functions, fewer places, and a fixed period.
        </Callout>
      </div>
      <div className="btn-row" style={{ maxWidth: 560 }}>
        <button className="btn secondary" id="declineinvitation" disabled={busy || !open} onClick={() => decide("declined")}>
          Decline
        </button>
        <button className="btn secondary" id="askquestion" disabled={busy || !open} onClick={ask}>
          Ask a question
        </button>
        <button className="btn dominant" id="acceptinvitation" disabled={busy || !open} onClick={() => decide("accepted")}>
          Accept
        </button>
      </div>
    </OrgFrame>
  );
}

// ── g2_10 · You are on the project ─────────────────────────────────────────
// The post-acceptance state, read back: the accepted invitation carries the
// grant the acceptance minted (grantId), the functions, and the period whose
// end date is the one clock here that runs out.
export function OnboardProject() {
  const nav = useNavigate();
  const ob = readOnboarding() as any;
  const [inv, setInv] = useState<any | null>(null);
  const [trail, setTrail] = useState<any[]>([]);
  const [showTrail, setShowTrail] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!ob) return;
    ensureOrgSession(ob.orgId)
      .then(async () => {
        let id = ob.invitationId;
        if (!id) {
          const list = await api.get("parties", `/v1/organisations/${ob.orgId}/invitations?state=ACCEPTED`);
          id = (list.invitations || [])[0]?.id;
        }
        if (!id) return;
        const whole = await api.get("parties", `/v1/invitations/${id}`);
        setInv(whole.invitation);
        setTrail(whole.events || []);
      })
      .catch((e: any) => setErr(String(e?.message || e)));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!ob) return <NoApplication />;
  const end = inv?.period?.end;
  const monthsLeft = end ? Math.max(0, Math.round((new Date(end).getTime() - Date.now()) / (30.44 * 24 * 3600e3))) : null;
  return (
    <OrgFrame active="Invitations" title={inv ? `You are on ${inv.contextId}` : "You are on no project yet"}>
      {err ? <ErrBar>{err}</ErrBar> : null}
      {inv ? (
        <>
          <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
            <Stat n={String((inv.functions || []).length)} label="functions live here" />
            <Stat n="0" label="of your people assigned" />
            <Stat n="1" label="project" />
            <Stat n={`To ${fmtDay(end)}`} label={`this grant runs${monthsLeft !== null ? ` · ${monthsLeft} months left` : ""}`} />
          </div>
          <KV
            rows={[
              ["Grant", <span className="mono">{inv.grantId || "—"}</span>],
              ["Functions", (inv.functions || []).join(" · ")],
              ["Accepted by", inv.decidedBy || "—"],
            ]}
          />
          <div className="pane-cols">
            <Callout kind="grey" title="Nothing will work yet">
              {(inv.functions || []).length} function{(inv.functions || []).length === 1 ? " is" : "s are"} live on
              this project and nobody holds them. A function granted to an organisation with no named person is inert
              — no worker can be registered and no evidence can be submitted until somebody is assigned.
              <div style={{ marginTop: 6 }}>
                <b>What to do ·</b> Assign at least a Registering Agent and a Support Agent. Assigning people is a
                later journey — this console does not draw it yet, and pretends nothing.
              </div>
            </Callout>
            <Callout kind="teal" title="Three clocks, and only one of them runs out">
              Your registration, the terms you hold and this project grant are three separate things on three separate
              clocks. The first two do not expire. This grant ends on {fmtDay(end)}, and work validated before then
              stays validated afterwards.
            </Callout>
          </div>
          {showTrail && trail.length ? (
            <KV rows={trail.map((e: any) => [`${e.seq} · ${e.event}`, `${e.actorPartyId}${e.note ? ` — ${e.note}` : ""} · ${fmtDay(e.at)}`])} />
          ) : null}
        </>
      ) : (
        <Sidecar>No accepted invitation on record — accept one on the Invitations screen and this view fills in.</Sidecar>
      )}
      <div className="btn-row" style={{ maxWidth: 440 }}>
        <button className="btn secondary" id="viewproject" disabled={!inv} onClick={() => setShowTrail(!showTrail)}>
          View the project
        </button>
        <button className="btn dominant" onClick={() => nav("/onboard/wider")}>
          Assign your people
        </button>
      </div>
      <p className="muted">
        "View the project" shows the offer's own recorded trail; "Assign your people" follows the reference to the
        status view — people assignment is a later journey.
      </p>
    </OrgFrame>
  );
}
