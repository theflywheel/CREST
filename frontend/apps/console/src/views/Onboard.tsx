// Programme onboarding (#155 phase D): the G-2 journey as real calls, rendered
// as the reference's DESKTOP console window (g2_1 frame): teal appbar, a
// sidebar of the flow's steps (Register → Terms → Certificates → Done),
// progress bar with the step counter right-aligned, two-column form grid.
//
// An organisation applies for itself, reads the published terms, accepts a
// specific version — and where the deployment configured
// REGISTRY_ORG_APPROVAL=on-terms-acceptance, is approved in the same
// transaction. No seeded party, no persona, no hidden admin step.
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, loginAs } from "@crest/api";
import { Callout, Chip, ErrBar, GridTable, KV, Sidecar } from "@crest/ui";

const OKEY = "crest.console.onboarding";

export type OrgProfile = {
  country: string;
  kind: string;
  sector: string;
  contactName: string;
};

export function readOnboarding(): ({ orgId: string; name: string } & Partial<OrgProfile>) | null {
  try {
    const raw = sessionStorage.getItem(OKEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}
function storeOnboarding(v: { orgId: string; name: string } & Partial<OrgProfile>) {
  try {
    sessionStorage.setItem(OKEY, JSON.stringify(v));
  } catch {
    /* only costs the reload convenience */
  }
}
export function updateOnboarding(patch: Record<string, unknown>) {
  const cur = readOnboarding();
  if (cur) storeOnboarding({ ...cur, ...(patch as object) } as any);
}

// The organisation's own session. The onboarding flow is anonymous until the
// application exists; the screens after it (invitations, terms requests) act
// IN the organisation's name, and the services rightly refuse an asserted
// party id. So the flow authenticates the same way a first login does: mint a
// token from the deployment's issuer and self-bind, which the never-yet-bound
// organisation accepts as bootstrap (#102). Idempotent per orgId.
let orgSessionFor: string | null = null;
export async function ensureOrgSession(orgId: string): Promise<void> {
  if (orgSessionFor === orgId) return;
  await loginAs(orgId);
  orgSessionFor = orgId;
}

/* The reference's four-step flow. Since the G-2 screens wave, every step has
   a surface — Certificates is the pre-live checks screen (g2_12). */
const STEPS: ReadonlyArray<{ key: string; label: string; gap?: boolean }> = [
  { key: "register", label: "Register" },
  { key: "terms", label: "Terms" },
  { key: "certificates", label: "Certificates" },
  { key: "done", label: "Done" },
];

export function OnboardFrame(props: {
  step: number; // 1-based position in STEPS
  title: string;
  who?: string;
  counter?: boolean; // the reference omits the counter on some frames (g2_5)
  children: React.ReactNode;
}) {
  const active = STEPS[props.step - 1];
  return (
    <div className="console-shell">
      <div className="appbar">
        <span className="mark" />
        <span className="t">CREST Console · Programme onboarding</span>
        <span className="who">
          <span className="who-label">
            {props.who ? `${props.who} · Onboarding Authorising Signatory` : "Onboarding Authorising Signatory"}
          </span>
        </span>
      </div>
      <div className="console-body">
        <div className="sidebar">
          <div className="cap">Registration</div>
          {STEPS.map((s, i) => (
            <button
              key={s.key}
              className={s.key === active.key ? "active" : ""}
              disabled
              style={{ cursor: "default" }}
            >
              {i + 1} · {s.label}
            </button>
          ))}
        </div>
        <main className="pane">
          <div className="screen">
            <div>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline", gap: 12 }}>
                <span className="eyebrow">Onboarding an organisation · G-2</span>
                {props.counter === false ? null : (
                  <span className="eyebrow" id="stepcounter">
                    Registration · {props.step} of {STEPS.length}
                  </span>
                )}
              </div>
              <div style={{ height: 3, background: "var(--generic-bg)", borderRadius: 2, marginTop: 8 }}>
                <div
                  style={{
                    height: 3,
                    width: `${(props.step / STEPS.length) * 100}%`,
                    background: "var(--p1)",
                    borderRadius: 2,
                  }}
                />
              </div>
            </div>
            <h2 className="scr-title">{props.title}</h2>
            {props.children}
          </div>
        </main>
      </div>
    </div>
  );
}

const F = (props: { label: string; hint?: string; children: React.ReactNode }) => (
  <label className="field">
    <span className="eyebrow" style={{ color: "var(--text-2)" }}>{props.label}</span>
    {props.children}
    {props.hint ? <span className="muted">{props.hint}</span> : null}
  </label>
);

/* g2_1 — the six-field identity form: legal name, country, work email,
   contact person, kind, sector. The kind is the one answer that branches
   later terms in the reference. Registration documents are DELIBERATELY not
   asked on this screen — the reference's own callout says so, and they come
   later, only once terms need them.

   Since #168 the whole form persists: country/kind/sector/contact person
   ride the Party's generic attributes map (L1 holds the map, this console's
   key/value choices are L2 vocabulary), and the status screen reads them
   back from GET /v1/organisations/{id}/registration — the registry, never
   this browser. */
export function OnboardApply() {
  const nav = useNavigate();
  const whyRef = useRef<HTMLDivElement>(null);
  const [name, setName] = useState("");
  const [country, setCountry] = useState("KE");
  const [email, setEmail] = useState("");
  const [contactName, setContactName] = useState("");
  const [kind, setKind] = useState("delivery");
  const [sector, setSector] = useState("health");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    setErr("");
    try {
      const out = await api.post("parties", "/v1/organisations", {
        displayName: name.trim(),
        kind: "organisation",
        contactRoutes: [{ kind: "email", value: email.trim() }],
        // Registry facts since #168: the Party primitive carries a generic
        // attributes map (L1); these key/value choices are this console's
        // vocabulary (L2). The status screen reads them back from the
        // registration endpoint — never from this browser.
        attributes: {
          kind,
          sector,
          country,
          contactPerson: contactName.trim(),
        },
      });
      storeOnboarding({
        orgId: out.party.id,
        name: out.party.displayName,
        contactName: contactName.trim(),
      });
      nav("/onboard/terms");
    } catch (e: any) {
      setErr(String(e?.message || e));
      setBusy(false);
    }
  };
  return (
    <OnboardFrame step={1} title="Register your organisation">
      <p className="body-2" style={{ maxWidth: 640 }}>
        About two minutes. Nothing on this screen needs anyone's approval — an application creates your
        organisation's record and nothing else: it grants no authority, publishes nothing to the registry log, and
        can be walked away from. Authority arrives only with approval.
      </p>
      <div className="pane-cols">
        <form id="orgapplyform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          <div className="form-grid">
            <F label="Legal name">
              <input required name="orgname" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Lakeside Health Trust" />
            </F>
            <F label="Country">
              <select name="country" value={country} onChange={(e) => setCountry(e.target.value)}>
                <option value="KE">Kenya</option>
                <option value="UG">Uganda</option>
                <option value="TZ">Tanzania</option>
                <option value="IN">India</option>
                <option value="OTHER">Other</option>
              </select>
            </F>
            <F label="Work email" hint="Your domain is taken from this.">
              <input required name="workemail" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="p.okello@example.org" />
            </F>
            <F label="Contact person">
              <input required name="contactname" value={contactName} onChange={(e) => setContactName(e.target.value)} placeholder="Who signs for this application" />
            </F>
          </div>
          <F label="What kind of organisation are you?" hint="Also available: Payment · Verifying institution.">
            <select name="orgkind" value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="delivery">Delivery — we do the work, or manage people who do</option>
              <option value="payment">Payment — we move the money</option>
              <option value="verifying">Verifying institution — we check credentials</option>
            </select>
          </F>
          <F
            label="What sector do you work in?"
            hint="A directory entry, so a project can find you. It carries no access consequence at all."
          >
            <select name="orgsector" value={sector} onChange={(e) => setSector(e.target.value)}>
              <option value="health">Health &amp; community health</option>
              <option value="agriculture">Agriculture</option>
              <option value="education">Education</option>
              <option value="finance">Financial services</option>
              <option value="other">Other</option>
            </select>
          </F>
          {err ? <ErrBar>{err}</ErrBar> : null}
          <div className="btn-row" style={{ maxWidth: 420 }}>
            <button
              type="button"
              className="btn secondary"
              onClick={() => whyRef.current?.scrollIntoView({ behavior: "smooth", block: "center" })}
            >
              Why so little?
            </button>
            <button className="btn dominant" disabled={busy}>
              {busy ? "Applying…" : "Continue"}
            </button>
          </div>
        </form>
        <div>
          <div className="card">
            <span className="eyebrow">Why the last field matters</span>
            <p className="body-2" style={{ marginTop: 6 }}>
              The kind is the one answer that branches. A delivery organisation and a bank are shown entirely
              different sets of terms on the next screen, because they are doing entirely different things here.
            </p>
          </div>
          <div ref={whyRef} style={{ marginTop: 12 }}>
            <Sidecar ok>
              <b>What is deliberately not asked.</b> Registration documents, certificates and letterheads are not
              asked for here. They come later, and only once you ask for terms that need them.
            </Sidecar>
          </div>
          <div style={{ marginTop: 12 }}>
            <Sidecar>
              What is stored, honestly: everything on this screen persists in the registry — the legal name, the work
              email, and the country/kind/sector/contact person as the organisation's self-declared attributes
              (#168). They are applicant-facing facts on your registration, never published to the public log, and
              never anything resembling an identity document.
            </Sidecar>
          </div>
        </div>
      </div>
      <p className="muted">
        <Link to="/">Back to the console</Link>
      </p>
    </OnboardFrame>
  );
}

/* g2_11 — the terms, by exact version */
// g2_11's own presentation rule: terms read in the organisation's language,
// not in permission slugs. The map is UI copy (L3) over the deployment's
// function vocabulary — the slugs stay the record; these words are how the
// record is read. A slug with no entry falls back to itself rather than hide.
const PLAIN_PERMISSION: Record<string, string> = {
  "submit-work-evidence": "Send records of work done",
  "specify-definition": "Say what work is and what counts as done",
  "ratify-definition": "Approve a work definition somebody else drafted",
  "attest-work": "Attest that work happened, so it can be counted",
  "act-for-party": "Act for a worker who cannot act for themselves",
  "resolve-unclear-evidence": "Review what could not be checked automatically",
  "issue-credentials": "Issue a credential in your own organisation\u2019s name",
};
// What no set of terms grants — true of the platform, not of this version.
const NEVER_GRANTED = [
  "Move money, or hand it to a bank \u2014 only the payments application touches a rail",
  "Vouch for another organisation, which no terms grant",
];
function ableAndNot(t: any, all: any[]) {
  const held: string[] = t.permissions || [];
  const able = held.map((p) => PLAIN_PERMISSION[p] || p);
  const elsewhere = new Set(
    all.filter((o) => !(o.id === t.id && o.version === t.version)).flatMap((o) => o.permissions || []),
  );
  const notAble = Object.keys(PLAIN_PERMISSION)
    .filter((p) => !held.includes(p))
    .map((p) => PLAIN_PERMISSION[p] + (elsewhere.has(p) ? " \u2014 that needs wider terms" : ""))
    .concat(NEVER_GRANTED);
  return { able, notAble };
}

export function OnboardTerms() {
  const nav = useNavigate();
  const ob = readOnboarding();
  const [terms, setTerms] = useState<any[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    api.get("parties", "/v1/terms").then((d: any) => setTerms(d.terms), (e: any) => setErr(String(e?.message || e)));
  }, []);
  if (!ob)
    return (
      <OnboardFrame step={2} title="No application in progress">
        <p className="body-2">
          <Link to="/onboard">Start one.</Link>
        </p>
      </OnboardFrame>
    );
  const t = terms?.[0];
  const accept = async () => {
    setBusy(true);
    setErr("");
    try {
      await api.post("parties", `/v1/organisations/${ob.orgId}/terms-acceptance`, {
        termsId: t.id,
        termsVersion: t.version,
        acceptedBy: ob.orgId,
      });
      nav("/onboard/status");
    } catch (e: any) {
      setErr(String(e?.message || e));
      setBusy(false);
    }
  };
  // g2_11's own primary: "Request these terms" carries the acceptance into
  // the checks screen (g2_12). At registration time, accepting the published
  // version IS the request — so this records the acceptance (idempotently
  // tolerant of one already recorded) and walks on to what is checked.
  const requestThese = async () => {
    setBusy(true);
    setErr("");
    try {
      try {
        await api.post("parties", `/v1/organisations/${ob.orgId}/terms-acceptance`, {
          termsId: t.id,
          termsVersion: t.version,
          acceptedBy: ob.orgId,
        });
      } catch (e: any) {
        // An acceptance already on record is not a failure of this walk; a
        // decided application refusing re-acceptance likewise. Anything else is.
        if (!(e && (e.status === 409 || /already/i.test(String(e.message))))) throw e;
      }
      nav("/onboard/checks");
    } catch (e: any) {
      setErr(String(e?.message || e));
      setBusy(false);
    }
  };
  return (
    <OnboardFrame step={2} title="Standard delivery terms" who={ob.contactName}>
      <div className="pane-cols">
        <div>
          <p className="body-2">
            Acceptance names a specific published version — "whatever was current" is not a fact anyone can check
            later, and this is precisely the fact verifiers walk back to.
          </p>
          {err ? <ErrBar>{err}</ErrBar> : null}
          {terms === null ? (
            <p className="muted">Reading the published terms…</p>
          ) : !t ? (
            <p className="body-2">
              This deployment has published no terms yet — the operator publishes them; nothing to accept until then.
            </p>
          ) : (
            <div className="card" style={{ maxWidth: 720 }}>
              <div style={{ font: "500 15px/1.4 Roboto, system-ui, sans-serif" }}>{t.name}</div>
              <p className="muted" style={{ margin: "4px 0 12px" }}>
                Version {t.version}, published{" "}
                {new Date(t.publishedAt).toLocaleDateString(undefined, { year: "numeric", month: "long" })}
              </p>
              {(() => {
                const { able, notAble } = ableAndNot(t, terms || []);
                const rows = Math.max(able.length, notAble.length);
                return (
                  <GridTable cols="1fr 1fr" head={["You would be able to", "You would not be able to"]}>
                    {Array.from({ length: rows }, (_, i) => (
                      <div className="g-row" key={i}>
                        <span>{able[i] || ""}</span>
                        <span className="muted">{notAble[i] || ""}</span>
                      </div>
                    ))}
                  </GridTable>
                );
              })()}
              <div className="btn-row" style={{ marginTop: 10 }}>
                <button className="btn secondary" id="acceptterms" disabled={busy} onClick={accept}>
                  {busy ? "Recording acceptance…" : `Accept v${t.version} for ${ob.name}`}
                </button>
                <button className="btn dominant" id="requestterms" disabled={busy} onClick={requestThese}>
                  Request these terms
                </button>
              </div>
            </div>
          )}
          {terms && terms.length > 1 ? (
            <div style={{ marginTop: 12 }}>
              <Callout kind="teal" title="Other sets you could ask for instead">
                {terms.slice(1).map((o: any, i: number) => (
                  <span key={o.id}>
                    {i > 0 ? " · " : ""}
                    <b>{o.name}</b> v{o.version} — {(o.permissions || []).join(", ")}
                  </span>
                ))}
                {" — asking for a wider set later goes through a reviewed request, never a quiet swap."}
              </Callout>
            </div>
          ) : null}
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <Callout kind="teal" title="Why this cannot shift under you">
            These terms are fixed. If they are revised, a new version is published and you would choose whether to
            move — nobody changes the ones you are on underneath you.
          </Callout>
          <Callout kind="grey" title="The same object, two words">
            What these screens call a <b>set of terms</b> is the object the PRD calls a <b>policy</b>. Terms is the
            word an organisation reads; policy is the word the registry uses. One object, one version number, one
            approval.
          </Callout>
          <Sidecar>
            Where this deployment configured approval-on-terms-acceptance, approval happens in the same transaction as
            the acceptance and is recorded as a policy decision — a decider is always named, even when the decider is a
            rule. Under the manual model, the instance operator decides.
          </Sidecar>
        </div>
      </div>
    </OnboardFrame>
  );
}

/* g2_12 — what is checked before this is live. The Certificates step of the
   registration rail. There is no automated checker in this codebase and this
   screen fakes none: a check is a RECORDED VERDICT with a named owner (a
   party, or a named policy), read back from the registry. At registration
   time under this deployment's approval model there may be no open
   terms-request to hang verdicts on — the screen says which world it is in
   rather than inventing rows. */
export function OnboardChecks() {
  const nav = useNavigate();
  const ob = readOnboarding();
  const [reg, setReg] = useState<any | null>(null);
  const [request, setRequest] = useState<any | null>(null);
  const [checks, setChecks] = useState<any[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const load = async () => {
    if (!ob) return;
    try {
      await ensureOrgSession(ob.orgId);
      const r = await api.get("parties", `/v1/organisations/${ob.orgId}/registration`);
      setReg(r);
      const list = await api.get("parties", `/v1/organisations/${ob.orgId}/terms-requests`);
      const newest = (list.requests || [])[0] || null;
      if (newest) {
        const whole = await api.get("parties", `/v1/terms-requests/${newest.id}`);
        setRequest(whole.request);
        setChecks(whole.checks || []);
      } else {
        setRequest(null);
        setChecks([]);
      }
    } catch (e: any) {
      setErr(String(e?.message || e));
    }
  };
  useEffect(() => {
    load();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!ob)
    return (
      <OnboardFrame step={3} title="No application in progress">
        <p className="body-2">
          <Link to="/onboard">Start one.</Link>
        </p>
      </OnboardFrame>
    );
  const submit = async () => {
    setBusy(true);
    setErr("");
    try {
      if (request && request.state === "DRAFT") {
        await api.post("parties", `/v1/terms-requests/${request.id}/submit`);
      }
      // With no open request, the acceptance recorded on the terms screen IS
      // the submitted registration request — nothing further to post; the
      // status screen shows where it stands.
      nav("/onboard/status");
    } catch (e: any) {
      setErr(String(e?.message || e));
      setBusy(false);
    }
  };
  return (
    <OnboardFrame step={3} title="What is checked before this is live" who={ob.contactName}>
      <p className="body-2">
        Requesting {reg?.termsId ? `terms ${reg.termsId} v${reg.termsVersion}` : "the published terms"} · {ob.name}
      </p>
      {err ? <ErrBar>{err}</ErrBar> : null}
      <div className="pane-cols">
        <div>
          {checks === null ? (
            <p className="muted">Reading the recorded verdicts…</p>
          ) : (
            (() => {
              // The reference's four questions (g2_12), asked always; each
              // status is read from a record that exists — a verdict the
              // reviewer recorded, or the contact the applicant declared —
              // and says "not recorded" where none does. No status here is
              // ever simulated; there is no automated checker in this
              // codebase and the register row stays honest about that.
              const verdict = (name: string) => (checks as any[]).find((c) => (c.name || "").includes(name));
              const vChip = (c: any) =>
                c ? (
                  <Chip sm kind={c.outcome === "PASS" ? "ok" : "err"}>
                    {c.outcome === "PASS" ? "confirmed" : "failed"}
                  </Chip>
                ) : (
                  <Chip sm kind="plain">
                    not recorded
                  </Chip>
                );
              const contact = reg?.attributes?.contactPerson;
              const rows: Array<[string, string, React.ReactNode]> = [
                [
                  "Your registration number",
                  "Checked against the business register for bodies like yours — recorded by the reviewer, never simulated",
                  vChip(verdict("register")),
                ],
                [
                  "Your organisation\u2019s certificate",
                  "So the systems you connect can be recognised as yours — a declared reference, read by a person",
                  vChip(verdict("certificate")),
                ],
                [
                  "A named data contact",
                  "Required before you hold anybody\u2019s identity number or photograph",
                  contact ? (
                    <Chip sm kind="ok">
                      named · {String(contact)}
                    </Chip>
                  ) : (
                    <Chip sm kind="warn">
                      needed
                    </Chip>
                  ),
                ],
                [
                  "If the register cannot confirm you",
                  "A certificate or gazette reference, read by a person instead",
                  vChip(verdict("fallback") || verdict("gazette")),
                ],
              ];
              const named = new Set(["register", "certificate", "fallback", "gazette"]);
              const extra = (checks as any[]).filter((c) => ![...named].some((n) => (c.name || "").includes(n)));
              return (
                <>
                  <GridTable cols="1.2fr 2fr 0.9fr" head={["What", "Why it is asked", "Status"]}>
                    {rows.map(([what, why, status]) => (
                      <div className="g-row" key={what}>
                        <span>{what}</span>
                        <span className="muted">{why}</span>
                        <span>{status}</span>
                      </div>
                    ))}
                    {extra.map((c: any) => (
                      <div className="g-row" key={c.id || c.name}>
                        <span>{c.name}</span>
                        <span className="muted">
                          {c.ownerKind === "policy" ? "policy " : ""}
                          <span className="mono">{c.owner}</span>
                          {c.note ? ` — ${c.note}` : ""}
                        </span>
                        <span>
                          <Chip sm kind={c.outcome === "PASS" ? "ok" : "err"}>
                            {c.outcome}
                          </Chip>
                        </span>
                      </div>
                    ))}
                  </GridTable>
                  {!checks.length ? (
                    <Sidecar>
                      "Not recorded" is the honest status: a check is a verdict with a named owner — a person, or a
                      named policy such as a business-register adapter — recorded while a request sits under review,
                      and none exists on this application yet.
                      {request ? ` The open request is ${request.id} (${request.state}).` : ""}
                    </Sidecar>
                  ) : null}
                </>
              );
            })()
          )}
        </div>
        <Callout kind="teal" title="Read the third row">
          The third row is the one organisations miss. You will hold names, phone numbers and identity numbers from
          the first day a project accepts you — naming who answers for that data was deferred at registration, not
          waived, and this is where it comes due.
        </Callout>
      </div>
      <div className="btn-row" style={{ maxWidth: 480 }}>
        <button className="btn secondary" onClick={() => nav("/onboard/terms")}>
          Save and return
        </button>
        <button className="btn dominant" id="submitchecks" disabled={busy} onClick={submit}>
          Submit the request
        </button>
      </div>
    </OnboardFrame>
  );
}

/* g2_4 / g2_13 — where the application stands */
export function OnboardStatus() {
  const nav = useNavigate();
  const ob = readOnboarding();
  const [reg, setReg] = useState<any | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (ob) api.get("parties", `/v1/organisations/${ob.orgId}/registration`).then(setReg, (e: any) => setErr(String(e?.message || e)));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!ob)
    return (
      <OnboardFrame step={4} title="No application in progress">
        <p className="body-2">
          <Link to="/onboard">Start one.</Link>
        </p>
      </OnboardFrame>
    );
  const state = reg?.state || "…";
  return (
    <OnboardFrame step={4} title={ob.name} who={ob.contactName}>
      {err ? <ErrBar>{err}</ErrBar> : null}
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        <Chip kind={state === "APPROVED" ? "ok" : state === "REJECTED" ? "err" : "warn"}>{state}</Chip>
      </div>
      <div className="pane-cols">
        <div>
          {reg ? (
            <KV
              rows={[
                ["Organisation", <span className="mono">{ob.orgId}</span>],
                [
                  "Country · kind · sector",
                  reg.attributes
                    ? `${reg.attributes.country || "—"} · ${reg.attributes.kind || "—"} · ${reg.attributes.sector || "—"} (served by the registry)`
                    : "—",
                ],
                ["Contact", (reg.attributes && reg.attributes.contactPerson) || "—"],
                ["Applied", reg.appliedAt || "—"],
                ["Terms accepted", reg.acceptedAt ? `${reg.termsId} v${reg.termsVersion}` : "not yet"],
                ["Decided by", reg.decidedBy || "—"],
                ["Reason", reg.reason || "—"],
              ]}
            />
          ) : null}
        </div>
        <div>
          {state === "APPROVED" ? (
            <Sidecar ok>
              Approved — the organisation is now published to the registry log, and its authority can be granted and
              checked. From here: define work in the console's admin view, attach a payment setup, and enrol workers
              through the field door.
            </Sidecar>
          ) : (
            <Sidecar>
              An application that is not yet decided grants nothing. Where approval is manual, the instance operator
              decides — never the organisation itself. What is checked before this is live is on the Certificates
              step: recorded verdicts with named owners, never a simulation.
            </Sidecar>
          )}
        </div>
      </div>
      <div className="btn-row" style={{ maxWidth: 560 }}>
        <button className="btn secondary" onClick={() => nav("/onboard/status")}>
          View your listing
        </button>
        <button className="btn secondary" onClick={() => nav("/onboard/standalone")}>
          Invite your people
        </button>
        <button className="btn dominant" id="onboard-done" onClick={() => nav("/onboard/standalone")}>
          Done
        </button>
      </div>
      <p className="muted">
        Inviting people is a later journey — both middle buttons follow the reference to the organisation's standing
        view. <Link to="/">To the console</Link>
      </p>
    </OnboardFrame>
  );
}
