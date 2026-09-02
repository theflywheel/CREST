// V-2, the institutional verifier (v2_1–v2_3, person), ported 1:1 from
// apps/verify: same endpoints, same disclosure rendering, same refusals-as-
// absence caveats.
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError, FIX } from "@crest/api";
import { Chip, KV, Sidecar, OpenNote } from "@crest/ui";
import { useVerify, loadSampleCredential, errText, short, day, type Verdict, type WorkEvent } from "../state";
import { VerdictChip, ChainList } from "./V1";

export function V21() {
  const s = useVerify();
  const nav = useNavigate();
  const p = s.orgParty;
  return (
    <>
      <div className="eyebrow">V-2 · Screen 1 of 3</div>
      <h2 className="scr-title">Checking as an onboarded institution</h2>
      <p className="body-2">
        Onboarding does not make the answer more true — the signature already settled that. What it adds is standing: an
        accreditation ceiling (what tier of claim this institution is trusted to attest, not to read), batch checking,
        and a name the worker's trail can hold accountable.
      </p>
      {p ? (
        <KV
          rows={[
            ["Signed in as", p.displayName || p.name || "Ministry of Health"],
            ["Party", <span className="mono">{short(p.id || FIX.org)}</span>],
            ["Kind", p.kind || "organisation"],
          ]}
        />
      ) : (
        <OpenNote>
          <b>Authorization facts not readable here.</b> The org's party record could not be read (
          {s.orgPartyErr || "unknown"}). The session is real — checks below run under the Ministry of Health login — but
          what onboarding granted this verifier is shown as a label, not as read facts.
        </OpenNote>
      )}
      <KV
        rows={[
          ["Onboarding adds", "accreditation ceiling · batch checks · an accountable name"],
          ["Onboarding never adds", "access to anything identifying, or a stronger verdict"],
        ]}
      />
      <OpenNote>
        <b>The accreditation ceiling itself has no readable endpoint.</b> The parties service records authorizations,
        but no route exposes a verifier's accreditation ceiling as a fact this screen could render. Shown here as the
        design's label only.
      </OpenNote>
      <div className="btn-row">
        <button className="btn" onClick={() => nav("/v2_2")}>
          Check a credential as this institution
        </button>
      </div>
    </>
  );
}

// The fields a work-event credential can carry, in the disclosure list's
// order. Included fields get the filled tick; anything the credential does
// not carry gets the hollow ring, stated as absence — the backend has no
// selective disclosure yet, so refusal is not yet a state a credential can
// express.
const evHas = (we: WorkEvent, name: string) => (we.evidenceFields || []).includes(name);
const DISCLOSABLE: Array<[string, (we: WorkEvent) => unknown]> = [
  ["What the work was", (we) => we.activity],
  ["The counted outcome", (we) => (we.outcome ? we.outcome.value + " " + we.outcome.unit : undefined)],
  [
    "When it was done",
    (we) => (we.period?.start ? day(we.period.start) + (we.period?.end ? " – " + day(we.period.end) : "") : undefined),
  ],
  ["Where, coarsely", (we) => we.geography || (evHas(we, "geography") ? "included in the credential's evidence" : undefined)],
  ["Household reference", (we) => we.householdId || (evHas(we, "household_id") ? "included in the credential's evidence" : undefined)],
  [
    "How many people it reached",
    (we) => we.beneficiaryCount ?? (evHas(we, "beneficiary_count") ? "included in the credential's evidence" : undefined),
  ],
  [
    "Whether a supervisor was present",
    (we) => we.supervisorPresent ?? (evHas(we, "supervisor_present") ? "included in the credential's evidence" : undefined),
  ],
  [
    "The source system's own record reference",
    (we) => we.sourceRecordRef || (evHas(we, "source_record_ref") ? "included in the credential's evidence" : undefined),
  ],
];

const tickSvg = (
  <svg viewBox="0 0 10 10" aria-hidden="true">
    <path d="M1.5 5.5 L4 8 L8.5 2.5" fill="none" stroke="#fff" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
);

export function V22() {
  const s = useVerify();
  const [party, setParty] = useState("");
  const [cred, setCred] = useState("");
  const [why, setWhy] = useState("");
  const v = s.verdict;
  const isOrgCheck = v && s.verifiedAs === FIX.org;
  const loadSample = async () => {
    try {
      setCred(JSON.stringify(await loadSampleCredential(party.trim()), null, 2));
    } catch (e) {
      s.fail(e);
    }
  };
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    try {
      await s.runVerify(JSON.parse(cred), FIX.org, why.trim());
    } catch (e) {
      s.fail(e);
    }
  };
  const we = s.credential?.credentialSubject?.workEvent || {};
  const rows = DISCLOSABLE.map(([label, get]) => {
    const val = get(we);
    return { label, val, present: val !== undefined && val !== null && val !== "" };
  });
  const refused = rows.filter((r) => !r.present);
  return (
    <>
      <div className="eyebrow">V-2 · Screen 2 of 3</div>
      <h2 className="scr-title">Verified, with the disclosure list</h2>
      <p className="body-2">
        The same verify call as V-1 — same endpoint, same signature, same verdict. What changes is the rendering: an
        institution sees the disclosure list, field by field, with every field the credential does not carry stated as
        absent.
      </p>
      <div className="card">
        <form id="orgverifyform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <input
              placeholder="worker party id (blank: the fixture worker)"
              value={party}
              onChange={(e) => setParty(e.target.value)}
              style={{ flex: 1 }}
            />
            <button type="button" className="btn secondary" id="orgloadsample" style={{ width: "auto", padding: "10px 14px" }} onClick={loadSample}>
              Load newest
            </button>
          </div>
          <label className="body-2">
            The credential (JSON)
            <textarea className="mono" rows={7} required value={cred} onChange={(e) => setCred(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
          </label>
          <input placeholder="purpose (recorded for the worker to read)" value={why} onChange={(e) => setWhy(e.target.value)} />
          <button className="btn">Check as Ministry of Health</button>
        </form>
      </div>
      {isOrgCheck && v ? (
        <>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            <VerdictChip v={v} />
            {v.valid ? <Chip kind={`tier${v.tier || 3}`}>Tier {v.tier ?? "—"}</Chip> : null}
            {refused.length ? (
              <Chip kind="warn">
                {refused.length} field{refused.length === 1 ? "" : "s"} not in this credential
              </Chip>
            ) : (
              <Chip kind="ok">Everything disclosable was shown</Chip>
            )}
          </div>
          <div className="eyebrow">What was shown — and what is absent</div>
          <div className="dis">
            {rows.map((r) =>
              r.present ? (
                <div className="li" key={r.label}>
                  <span className="tick">{tickSvg}</span>
                  <span>
                    <div className="t">{r.label}</div>
                    <div className="s">{String(r.val)}</div>
                  </span>
                </div>
              ) : (
                <div className="li off" key={r.label}>
                  <span className="tick" />
                  <span>
                    <div className="t">{r.label}</div>
                    <div className="s">Not in this credential — absence, not refusal</div>
                  </span>
                </div>
              ),
            )}
          </div>
          <Sidecar>
            The design's rule is refusals-shown-as-refusals: if withheld fields simply vanished, a verifier could not
            tell a worker who refused from a worker who has nothing. A hollow ring here is not that — it marks a field
            this credential does not carry, which is absence, not refusal.
          </Sidecar>
          <OpenNote>
            <b>Two honest caveats.</b> First, the backend has no selective disclosure yet, so today this screen can only
            show presence and absence — a true refusal state, rendered as an explicit refusal, arrives with selective
            disclosure in the credential itself. Second, whether showing refusals at all will be acceptable is an open
            question in the design — a visible refusal is itself information about the worker.
          </OpenNote>
          <ChainList v={v} />
        </>
      ) : v ? (
        <OpenNote>
          The last check in this session was a bare (pass-only) check, not an institutional one — run one above to see
          the disclosure list rendered under this institution's name.
        </OpenNote>
      ) : null}
    </>
  );
}

export function V23() {
  const s = useVerify();
  const [party, setParty] = useState("");
  const [creds, setCreds] = useState("");
  const [why, setWhy] = useState("");
  const [out, setOut] = useState<{ verdicts?: Verdict[] } | null>(null);
  const [localErr, setLocalErr] = useState<string | null>(null);
  const loadChain = async () => {
    try {
      const pid = party.trim() || FIX.workerA;
      const res = await api.get("verification", `/v1/parties/${encodeURIComponent(pid)}/credentials`);
      const cs = res.credentials || [];
      if (!cs.length) throw new Error("that person's chain holds no credentials yet");
      setCreds(JSON.stringify(cs, null, 2));
    } catch (e) {
      s.fail(e);
    }
  };
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setLocalErr(null);
    setOut(null);
    const purpose = why.trim();
    if (purpose.length < 10 || purpose.length > 200) {
      setLocalErr("The purpose must be 10–200 characters — each worker reads it in their own trail, so it has to say something.");
      return;
    }
    try {
      const parsed = JSON.parse(creds);
      if (!Array.isArray(parsed)) throw new Error("the batch must be a JSON array of credentials");
      if (parsed.length > 100) {
        setLocalErr(
          `${parsed.length} credentials exceeds the deployment's batch cap (L1 default 100, #107). The batch is refused whole — nothing was checked.`,
        );
        return;
      }
      setOut(await api.post("verification", "/v1/verify/batch", { credentials: parsed, requestedByPartyId: FIX.org, purpose }));
    } catch (e) {
      s.fail(e);
    }
  };
  const n = why.length;
  const verdicts = out?.verdicts || [];
  return (
    <>
      <div className="eyebrow">V-2 · Screen 3 of 3</div>
      <h2 className="scr-title">Batch — checking many</h2>
      <p className="body-2">
        A batch declares who is asking and why — in words each worker will read in their own trail — and is size-capped
        by the deployment. Per-credential answers only: there are deliberately no aggregate answers, because "83% of
        this cohort verified" is a judgement about people none of them agreed to.
      </p>
      <div className="card">
        <form id="batchform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <input
              placeholder="worker party id (blank: the fixture worker)"
              value={party}
              onChange={(e) => setParty(e.target.value)}
              style={{ flex: 1 }}
            />
            <button type="button" className="btn secondary" id="batchloadsample" style={{ width: "auto", padding: "10px 14px" }} onClick={loadChain}>
              Load their whole chain
            </button>
          </div>
          <label className="body-2">
            Credentials (JSON array)
            <textarea className="mono" rows={7} required placeholder="[{…}, {…}]" value={creds} onChange={(e) => setCreds(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
          </label>
          <label className="body-2">
            Purpose — free text, 10–200 characters, worker-visible
            <input
              required
              minLength={10}
              maxLength={200}
              placeholder="e.g. Annual CHW programme audit, District North, 2026"
              value={why}
              onChange={(e) => setWhy(e.target.value)}
              style={{ width: "100%", marginTop: 4 }}
            />
          </label>
          <div className="muted" id="purposecount">
            {n} / 200 — {n < 10 ? "at least 10" : "long enough"}
          </div>
          <button className="btn">Check the batch</button>
        </form>
        <div id="batchout" style={{ marginTop: 12 }}>
          {localErr ? <div className="errbar">{localErr}</div> : null}
          {out ? (
            verdicts.length ? (
              <>
                <div className="tblwrap">
                  <table className="tbl">
                    <tbody>
                      <tr>
                        <th>#</th>
                        <th>Credential</th>
                        <th>Verdict</th>
                        <th>Tier</th>
                        <th>Flags</th>
                      </tr>
                      {verdicts.map((v, i) => (
                        <tr key={i}>
                          <td>{i + 1}</td>
                          <td className="mono">{short((v as { credentialId?: string }).credentialId || "—")}</td>
                          <td>{v.valid ? <Chip sm kind="ok">valid</Chip> : <Chip sm kind="err">not valid</Chip>}</td>
                          <td>{v.valid ? <Chip sm kind={`tier${v.tier || 3}`}>tier {v.tier ?? "—"}</Chip> : "—"}</td>
                          <td>
                            {v.revoked ? <Chip sm kind="err">withdrawn</Chip> : null}
                            {(v.contested || []).length ? <Chip sm kind="warn">contested</Chip> : null}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <p className="muted" style={{ marginTop: 8 }}>
                  {verdicts.length} per-credential answer{verdicts.length === 1 ? "" : "s"}. No totals, no rate, no
                  aggregate — deliberately.
                </p>
              </>
            ) : (
              <div className="muted">The batch returned no verdicts.</div>
            )
          ) : null}
        </div>
      </div>
      <Sidecar>
        The size cap is a deployment (L2) setting over an L1 default of 100 — configurable, but never removable (#107).
        A batch over the cap is refused whole, not truncated: a truncated batch would silently check different people
        than the verifier declared.
      </Sidecar>
    </>
  );
}

export function Person() {
  const [party, setParty] = useState<string>(FIX.workerA);
  const [why, setWhy] = useState("");
  const [out, setOut] = useState<{ credentials?: Array<{ id?: string; credentialSubject?: { workEvent?: WorkEvent } }>; count?: number } | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setOut(null);
    setMsg(null);
    try {
      const q = new URLSearchParams({ requestedByPartyId: FIX.org });
      if (why.trim()) q.set("purpose", why.trim());
      setOut(await api.get("verification", `/v1/parties/${encodeURIComponent(party.trim())}/credentials?` + q));
    } catch (e) {
      setMsg(
        e instanceof ApiError && e.status === 404
          ? "Nobody resolves to that id — and whether it once merged into another is deliberately not said."
          : errText(e),
      );
    }
  };
  const creds = out?.credentials || [];
  return (
    <>
      <div className="eyebrow">V-2 · Resolve a person</div>
      <h2 className="scr-title">Resolve a person</h2>
      <p className="body-2">
        Either of a merged person's ids returns their whole chain of credentials — and nothing about the chain itself. A
        verifier is never told a merge happened (#104): the join is invisible by design, because "these two identities
        were once separate" is itself a fact about the worker.
      </p>
      <div className="card">
        <form id="personform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <input className="mono" required placeholder="did:crest:party:…" value={party} onChange={(e) => setParty(e.target.value)} />
          <input placeholder="why (recorded for the worker)" value={why} onChange={(e) => setWhy(e.target.value)} />
          <button className="btn">Resolve the chain</button>
        </form>
        <div id="personout" style={{ marginTop: 12 }}>
          {msg ? <div className="muted">{msg}</div> : null}
          {out ? (
            creds.length ? (
              <>
                <p className="body-2">
                  {String(out.count ?? creds.length)} credential(s) in this person's chain. Each read of this chain
                  wrote a line into the worker's own trail.
                </p>
                <div className="tblwrap">
                  <table className="tbl">
                    <tbody>
                      <tr>
                        <th>Credential</th>
                        <th>Activity</th>
                        <th>Outcome</th>
                        <th>Period</th>
                      </tr>
                      {creds.map((c, i) => {
                        const we = c.credentialSubject?.workEvent || {};
                        return (
                          <tr key={i}>
                            <td className="mono">{short(c.id)}</td>
                            <td>{we.activity || "—"}</td>
                            <td>{we.outcome ? we.outcome.value + " " + we.outcome.unit : "—"}</td>
                            <td>{day(we.period?.start)}</td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              </>
            ) : (
              <div className="muted">This person's chain holds no credentials yet — an honest empty chain, not an error.</div>
            )
          ) : null}
        </div>
      </div>
    </>
  );
}
