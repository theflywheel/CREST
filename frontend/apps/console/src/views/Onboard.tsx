// Programme onboarding (#155 phase D): the g-series journey as real calls.
// An organisation applies for itself, reads the published terms, accepts a
// specific version — and where the deployment configured
// REGISTRY_ORG_APPROVAL=on-terms-acceptance, is approved in the same
// transaction. No seeded party, no persona, no hidden admin step.
import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api } from "@crest/api";
import { Chip, ErrBar, KV, Sidecar } from "@crest/ui";

const OKEY = "crest.console.onboarding";

export type OrgProfile = {
  kind: string;
  sector: string;
  regNumber: string;
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

function OnboardFrame(props: { step: string; title: string; children: React.ReactNode }) {
  return (
    <div className="panel-shell screen">
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        <div style={{ width: 24, height: 24, borderRadius: 5, background: "var(--p1)" }} />
        <div style={{ font: "500 16px/1 Roboto" }}>CREST Console · Programme onboarding</div>
      </div>
      <div className="eyebrow">{props.step}</div>
      <h2 className="scr-title">{props.title}</h2>
      {props.children}
    </div>
  );
}

/* g2_1 — the six-field identity form: name, kind, sector, registration
   number, contact name, contact email. The kind is the one answer that
   branches later terms in the reference.

   Honest gap, recorded rather than patched around (traceability g2_1): the
   Party schema is closed (additionalProperties: false) and POST
   /v1/organisations rejects unknown fields, so kind/sector/registration
   number/contact name cannot be persisted server-side without an L1 schema
   change. They are held client-side for the flow and shown on the status
   screen; the server holds the name and the contact email. */
export function OnboardApply() {
  const nav = useNavigate();
  const [name, setName] = useState("");
  const [kind, setKind] = useState("delivery");
  const [sector, setSector] = useState("health");
  const [regNumber, setRegNumber] = useState("");
  const [contactName, setContactName] = useState("");
  const [email, setEmail] = useState("");
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
      });
      storeOnboarding({
        orgId: out.party.id,
        name: out.party.displayName,
        kind,
        sector,
        regNumber: regNumber.trim(),
        contactName: contactName.trim(),
      });
      nav("/onboard/terms");
    } catch (e: any) {
      setErr(String(e?.message || e));
      setBusy(false);
    }
  };
  const sel = { width: "100%", marginTop: 4, font: "inherit" } as const;
  return (
    <OnboardFrame step="Onboarding · Step 1 of 3" title="Register your organisation">
      <p className="body-2">
        About two minutes, and nothing on this screen needs anyone's approval. An application creates your
        organisation's record and nothing else: it grants no authority, publishes nothing to the registry log, and can
        be walked away from. Authority arrives only with approval.
      </p>
      <form id="orgapplyform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10, maxWidth: 480 }}>
        <label className="body-2">
          Legal name
          <input required name="orgname" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Lakeside Health Trust" style={{ width: "100%", marginTop: 4 }} />
        </label>
        <label className="body-2">
          What kind of organisation are you?
          <select name="orgkind" value={kind} onChange={(e) => setKind(e.target.value)} style={sel}>
            <option value="delivery">Delivery — we do the work, or manage people who do</option>
            <option value="payment">Payment — we move the money</option>
            <option value="verifying">Verifying institution — we check credentials</option>
          </select>
          <span className="muted" style={{ display: "block", marginTop: 2 }}>
            The one answer that branches: a delivery organisation and a bank are shown different terms.
          </span>
        </label>
        <label className="body-2">
          What sector do you work in?
          <select name="orgsector" value={sector} onChange={(e) => setSector(e.target.value)} style={sel}>
            <option value="health">Health &amp; community health</option>
            <option value="agriculture">Agriculture</option>
            <option value="education">Education</option>
            <option value="finance">Financial services</option>
            <option value="other">Other</option>
          </select>
        </label>
        <label className="body-2">
          Registration number
          <input required name="orgreg" value={regNumber} onChange={(e) => setRegNumber(e.target.value)} placeholder="e.g. NGO/2091/2018" style={{ width: "100%", marginTop: 4 }} />
        </label>
        <label className="body-2">
          Contact person
          <input required name="contactname" value={contactName} onChange={(e) => setContactName(e.target.value)} placeholder="Who signs for this application" style={{ width: "100%", marginTop: 4 }} />
        </label>
        <label className="body-2">
          Contact email
          <input required name="contactemail" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="programmes@example.org" style={{ width: "100%", marginTop: 4 }} />
        </label>
        {err ? <ErrBar>{err}</ErrBar> : null}
        <button className="btn" disabled={busy}>
          {busy ? "Applying…" : "Apply"}
        </button>
      </form>
      <Sidecar>
        What is stored where, honestly: the name and the contact email persist in the registry. Kind, sector,
        registration number and contact person are held in this browser for the flow — the registry's Party schema has
        no profile field yet, and inventing one client-side would fake a record. The gap is recorded in
        docs/journey-traceability.md (g2_1).
      </Sidecar>
      <p className="muted">
        <Link to="/">Back to the console</Link>
      </p>
    </OnboardFrame>
  );
}

/* g-2 — the terms, by exact version */
export function OnboardTerms() {
  const nav = useNavigate();
  const ob = readOnboarding();
  const [terms, setTerms] = useState<any[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    api.get("parties", "/v1/terms").then((d: any) => setTerms(d.terms), (e: any) => setErr(String(e?.message || e)));
  }, []);
  if (!ob) return <OnboardFrame step="Onboarding" title="No application in progress"><p className="body-2"><Link to="/onboard">Start one.</Link></p></OnboardFrame>;
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
    <OnboardFrame step="Onboarding · Step 2 of 3" title="The terms, by exact version">
      <p className="body-2">
        Acceptance names a specific published version — "whatever was current" is not a fact anyone can check later,
        and this is precisely the fact verifiers walk back to.
      </p>
      {err ? <ErrBar>{err}</ErrBar> : null}
      {terms === null ? (
        <p className="muted">Reading the published terms…</p>
      ) : !t ? (
        <p className="body-2">This deployment has published no terms yet — the operator publishes them; nothing to accept until then.</p>
      ) : (
        <div className="card" style={{ maxWidth: 560 }}>
          <KV
            rows={[
              ["Terms", t.name],
              ["Version", String(t.version)],
              ["Grants, on approval", (t.permissions || []).join(" · ") || "—"],
            ]}
          />
          <div className="btn-row">
            <button className="btn" id="acceptterms" disabled={busy} onClick={accept}>
              {busy ? "Recording acceptance…" : `Accept v${t.version} for ${ob.name}`}
            </button>
          </div>
        </div>
      )}
      <Sidecar>
        Where this deployment configured approval-on-terms-acceptance, approval happens in the same transaction as the
        acceptance and is recorded as a policy decision — a decider is always named, even when the decider is a rule.
      </Sidecar>
    </OnboardFrame>
  );
}

/* g-3 — where the application stands */
export function OnboardStatus() {
  const ob = readOnboarding();
  const [reg, setReg] = useState<any | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (ob) api.get("parties", `/v1/organisations/${ob.orgId}/registration`).then(setReg, (e: any) => setErr(String(e?.message || e)));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!ob) return <OnboardFrame step="Onboarding" title="No application in progress"><p className="body-2"><Link to="/onboard">Start one.</Link></p></OnboardFrame>;
  const state = reg?.state || "…";
  return (
    <OnboardFrame step="Onboarding · Step 3 of 3" title={ob.name}>
      {err ? <ErrBar>{err}</ErrBar> : null}
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        <Chip kind={state === "APPROVED" ? "ok" : state === "REJECTED" ? "err" : "warn"}>{state}</Chip>
      </div>
      {reg ? (
        <KV
          rows={[
            ["Organisation", <span className="mono">{ob.orgId}</span>],
            ["Kind · sector", ob.kind ? `${ob.kind} · ${ob.sector} (held in this browser — not yet a registry fact)` : "—"],
            ["Registration no.", ob.regNumber || "—"],
            ["Contact", ob.contactName || "—"],
            ["Applied", reg.appliedAt || "—"],
            ["Terms accepted", reg.acceptedAt ? `${reg.termsId} v${reg.termsVersion}` : "not yet"],
            ["Decided by", reg.decidedBy || "—"],
            ["Reason", reg.reason || "—"],
          ]}
        />
      ) : null}
      {state === "APPROVED" ? (
        <Sidecar ok>
          Approved — the organisation is now published to the registry log, and its authority can be granted and
          checked. From here: define work in the console's admin view, attach a payment setup, and enrol workers
          through the field door.
        </Sidecar>
      ) : (
        <Sidecar>
          An application that is not yet decided grants nothing. Where approval is manual, the instance operator
          decides — never the organisation itself.
        </Sidecar>
      )}
      <p className="muted">
        <Link to="/">To the console</Link>
      </p>
    </OnboardFrame>
  );
}
