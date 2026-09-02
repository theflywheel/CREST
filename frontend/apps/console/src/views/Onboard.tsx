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
import { api } from "@crest/api";
import { Chip, ErrBar, KV, Sidecar } from "@crest/ui";

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

/* The reference's four-step flow. Certificates (g2_12) has no surface yet —
   it renders in the rail as the named gap it is, never as a fake step. */
const STEPS: ReadonlyArray<{ key: string; label: string; gap?: boolean }> = [
  { key: "register", label: "Register" },
  { key: "terms", label: "Terms" },
  { key: "certificates", label: "Certificates", gap: true },
  { key: "done", label: "Done" },
];

function OnboardFrame(props: {
  step: number; // 1-based position in STEPS
  title: string;
  who?: string;
  children: React.ReactNode;
}) {
  const active = STEPS[props.step - 1];
  return (
    <div className="console-shell">
      <div className="appbar">
        <span className="mark" />
        <span className="t">CREST Console · Programme onboarding</span>
        <span className="who">
          <span className="who-label">{props.who || "Onboarding Authorising Signatory"}</span>
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
              style={{ cursor: "default", ...(s.gap ? { color: "var(--text-3)" } : null) }}
            >
              {i + 1} · {s.label}
              {s.gap ? " — not built" : ""}
            </button>
          ))}
        </div>
        <main className="pane">
          <div className="screen">
            <div>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline", gap: 12 }}>
                <span className="eyebrow">Onboarding an organisation · G-2</span>
                <span className="eyebrow" id="stepcounter">
                  Registration · {props.step} of {STEPS.length}
                </span>
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
  return (
    <OnboardFrame step={2} title="The terms, by exact version" who={ob.contactName}>
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
            <div className="card" style={{ maxWidth: 560 }}>
              <KV
                rows={[
                  ["Terms", t.name],
                  ["Version", String(t.version)],
                  ["Grants, on approval", (t.permissions || []).join(" · ") || "—"],
                ]}
              />
              <div className="btn-row" style={{ marginTop: 10 }}>
                <button className="btn" id="acceptterms" disabled={busy} onClick={accept}>
                  {busy ? "Recording acceptance…" : `Accept v${t.version} for ${ob.name}`}
                </button>
              </div>
            </div>
          )}
        </div>
        <Sidecar>
          Where this deployment configured approval-on-terms-acceptance, approval happens in the same transaction as
          the acceptance and is recorded as a policy decision — a decider is always named, even when the decider is a
          rule. Under the manual model, the instance operator decides.
        </Sidecar>
      </div>
    </OnboardFrame>
  );
}

/* g2_4 / g2_13 — where the application stands */
export function OnboardStatus() {
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
              decides — never the organisation itself. The certificate check the rail names (g2_12) is not built;
              nothing here pretends it ran.
            </Sidecar>
          )}
        </div>
      </div>
      <p className="muted">
        <Link to="/">To the console</Link>
      </p>
    </OnboardFrame>
  );
}
