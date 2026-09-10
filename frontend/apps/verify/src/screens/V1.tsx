// V-1, the pass-only verifier (v1_1–v1_3), ported 1:1 from apps/verify.
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { links } from "@crest/api";
import { Chip, KV, Sidecar, OpenNote, NextBlock, DisLi } from "@crest/ui";
import { useVerify, short, day, type Verdict } from "../state";
import { parseCredential } from "../offline";

const tick = (
  <svg viewBox="0 0 10 10" aria-hidden="true">
    <path d="M1.5 5.5 L4 8 L8.5 2.5" fill="none" stroke="#fff" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
);

export function VerdictChip(props: { v: Verdict }) {
  return props.v.valid ? (
    <Chip kind="ok">Valid — the signature checks out</Chip>
  ) : (
    <Chip kind="err">Not valid</Chip>
  );
}

// The checkable/trusting chain, field handling copied from the PoC's
// verifier result: each link says whether you could check it without CREST.
export function ChainList(props: { v: Verdict }) {
  return (
    <>
      {(props.v.trustChain || []).map((l, i) => (
        <div className="dis" key={i}>
          <DisLi on={l.checkable} t={l.claim} s={(l.checkable ? "checkable — " : "trusting — ") + (l.how || l.trusting || "")} />
        </div>
      ))}
      {(props.v.notEstablished || []).map((n, i) => (
        <div className="dis" key={"ne" + i}>
          <DisLi on={false} t="Not established" s={n} />
        </div>
      ))}
    </>
  );
}

export function V11() {
  const s = useVerify();
  const nav = useNavigate();
  const [name, setName] = useState(s.pass?.name || "");
  const [contact, setContact] = useState("");
  const [why, setWhy] = useState(s.pass?.purpose || "");
  const [busy, setBusy] = useState(false);
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    try {
      await s.getPass(name.trim(), contact.trim(), why.trim());
      nav("/v1_2");
    } catch (e) {
      s.fail(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <div className="eyebrow">V-1 · Screen 1 of 3</div>
      <h2 className="scr-title">Get a pass to check credentials</h2>
      <p className="body-2">
        A verifier pass identifies you without onboarding you. It puts a name on your checks — the worker sees{" "}
        <em>who</em> looked, in their own "who checked me" trail — and it grants nothing: no accreditation ceiling, no
        batch rights, no vetting. Identified, not onboarded.
      </p>
      <div className="card">
        <form id="passform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <label className="body-2">
            Your name
            <input required minLength={2} maxLength={80} value={name} onChange={(e) => setName(e.target.value)} placeholder="Joseph Mwangi" style={{ width: "100%", marginTop: 4 }} />
          </label>
          <label className="body-2">
            Email or phone
            <input required minLength={5} maxLength={120} value={contact} onChange={(e) => setContact(e.target.value)} placeholder="+254 7•• ••• 412" style={{ width: "100%", marginTop: 4 }} />
            <span className="muted"> Kept by this deployment so somebody can reach you; never shown to the worker.</span>
          </label>
          <label className="body-2">
            Why you are checking
            <input maxLength={200} value={why} onChange={(e) => setWhy(e.target.value)} placeholder="Hiring for a private clinic" style={{ width: "100%", marginTop: 4 }} />
            <span className="muted"> Shown to the worker beside your name, on every check you make with this pass.</span>
          </label>
          <div className="btn-row">
            <button className="btn" disabled={busy}>{s.pass ? "Get a fresh pass" : "Get my pass"}</button>
            {s.pass ? (
              <button type="button" className="btn secondary" onClick={() => nav("/v1_2")}>
                Keep the pass I have
              </button>
            ) : null}
          </div>
        </form>
      </div>
      <KV
        rows={[
          ["A pass adds", "your name on every check the worker sees"],
          ["A pass does not add", "any trust — the answer rides the signature either way"],
          ["Onboarding (V-2) adds", "an accreditation ceiling and batch checking"],
        ]}
      />
      <OpenNote>
        <b>Not confirmed.</b> The reference sends a code to the contact before the pass is issued. No notification channel
        exists in this deployment (#150), so the contact you give is recorded as given, not proven reachable. The pass
        is issued anyway — identified, not vetted — and every check you make with it is on the record against it.
      </OpenNote>
      <Sidecar>
        Checking a signature needs no pass at all: the next screen's offline check uses only the issuer's published key
        and never touches this deployment. A pass is for the <em>online</em> check, which is recorded for the worker.
      </Sidecar>
    </>
  );
}

export function V12() {
  const s = useVerify();
  const nav = useNavigate();
  const [cred, setCred] = useState("");
  const [why, setWhy] = useState(s.pass?.purpose || "");
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    try {
      await s.runVerify(parseCredential(cred), "", why.trim());
      nav("/v1_3");
    } catch (e) {
      s.fail(e);
    }
  };
  const submitOffline = async () => {
    try {
      await s.runOfflineVerify(parseCredential(cred));
      nav("/v1_3");
    } catch (e) {
      s.fail(e);
    }
  };
  const refreshTrust = async () => {
    try {
      await s.refreshOfflineTrust();
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <>
      <div className="eyebrow">V-1 · Screen 2 of 3</div>
      <h2 className="scr-title">Scan or enter the credential</h2>
      <p className="body-2">
        Paste the credential exactly as scanned from the worker's printed card or wallet. The online check is recorded
        against your pass; the offline check needs nothing from anyone.
      </p>
      {!s.pass ? (
        <OpenNote>
          <b>No pass held.</b> An online check names who is asking — the service refuses one with nobody behind it
          (G1 #9). <a href="#/v1_1">Get a pass</a> first, or use the offline signature check below.
        </OpenNote>
      ) : (
        <p className="muted">
          Checking as <b>{s.pass.name}</b> · pass {s.pass.id.slice(-6).toUpperCase()}.{" "}
          <a href="#/v1_1">Change</a>
        </p>
      )}
      <div className="card">
        <form id="verifyform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <p className="muted">
            Bring the signed document from the worker's card or wallet. A pass-only verifier cannot look up a person's
            private credential history by party id.
          </p>
          <label className="body-2">
            The credential (JSON, as scanned)
            <textarea
              className="mono"
              rows={9}
              required
              placeholder='{"@context": …}'
              value={cred}
              onChange={(e) => setCred(e.target.value)}
              style={{ width: "100%", marginTop: 4 }}
            />
          </label>
          <input placeholder="why (recorded for the worker beside your name)" maxLength={200} value={why} onChange={(e) => setWhy(e.target.value)} />
          <div className="btn-row">
            <button className="btn">Check it online</button>
            <button type="button" className="btn secondary" onClick={submitOffline}>Check signature offline</button>
            {s.pass ? (
              <button type="button" className="btn secondary" onClick={() => nav("/v1_ask")}>
                Request the check instead
              </button>
            ) : null}
          </div>
          <button type="button" className="btn secondary" onClick={refreshTrust}>Refresh trusted issuer keys (online)</button>
          <p className="body-2">Offline checks use only keys refreshed from this deployment. Refresh while online before taking the verifier offline.</p>
        </form>
      </div>
      <Sidecar>
        Every online check leaves a line in the worker's own trail with your name on it, and no requester gets more
        than the deployment's cap in a window. That is by design: the record of who looked belongs to the person
        looked at, and the cap is what makes it a control rather than a receipt.
      </Sidecar>
      <Sidecar>
        You do not have to take CREST's word for the answer: the same credential verifies in{" "}
        <a href={links.injiVerify} target="_blank" rel="noopener noreferrer">
          Inji Verify
        </a>{" "}
        — a separate verifier from a separate project, checking the same signature against the issuer's published key
        (#155 phase C).
      </Sidecar>
    </>
  );
}

export function V13() {
  const s = useVerify();
  const nav = useNavigate();
  const v = s.verdict;
  if (!v)
    return (
      <>
        <div className="eyebrow">V-1 · Screen 3 of 3</div>
        <h2 className="scr-title">The answer</h2>
        <p className="body-2">
          No credential has been checked in this session yet. The answer screen renders the verdict of the last check.
        </p>
        <div className="btn-row">
          <button className="btn" onClick={() => nav("/v1_2")}>
            Check one now
          </button>
        </div>
      </>
    );
  const we = s.credential?.credentialSubject?.workEvent || {};
  const defRef = we.definition?.id || we.definitionRef || we.activity || "";
  const defVersion = we.definition?.version || "";
  const statusChecked = v.statusCheckedAt ? new Date(v.statusCheckedAt).toLocaleString() : null;
  return (
    <>
      <div className="eyebrow">V-1 · Screen 3 of 3</div>
      <h2 className="scr-title">{v.valid ? (v.offline ? "Signature verified offline" : "Verified") : "Not verified"}</h2>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        <VerdictChip v={v} />
        {v.valid && !v.offline ? <Chip kind={`tier${v.tier || 3}`}>Tier {v.tier ?? "—"} — computed now, never stored</Chip> : null}
        {v.revoked ? <Chip kind="err">Withdrawn</Chip> : null}
        {(v.contested || []).length ? <Chip kind="warn">Contested — the record, not the money</Chip> : null}
        {v.offline ? <Chip kind="warn">Offline — withdrawal not checked</Chip> : null}
      </div>
      {(v.reasons || []).length ? (
        <div className="card quiet">
          {(v.reasons || []).map((r, i) => (
            <div className="body-2" key={i}>
              {r}
            </div>
          ))}
        </div>
      ) : null}
      <div className="eyebrow">Yes, plus facts — and nothing identifying</div>
      <KV
        rows={[
          ["What work", `${we.activity || "—"}${we.outcome ? " · " + we.outcome.value + " " + we.outcome.unit : ""}`],
          [
            "Under which definition",
            <>
              <span className="mono">{short(defRef) || "—"}</span>
              {defVersion ? " · v" + defVersion : ""}
            </>,
          ],
          ["At what tier", `Tier ${v.tier ?? "—"}, derived from provenance at this moment`],
          [
            "Withdrawal status",
            statusChecked
              ? `checked online at ${statusChecked}`
              : "not checked online — an offline signature check cannot establish current withdrawal status",
          ],
          ["When", `${day(we.period?.start)}${we.period?.end ? " – " + day(we.period.end) : ""}`],
        ]}
      />
      <div className="eyebrow">What you can check, and what you are trusting</div>
      <ChainList v={v} />
      <Sidecar ok>
        This answer rides the signature, not CREST's word. The same credential shown to you offline, against the
        published key, verifies the same way — no account, no vetting, and nothing here identifies the worker to you.
      </Sidecar>
      <NextBlock
        happened={v.offline ? "The signature was checked on this device. Offline checks are not sent to CREST's presentation trail." : `The credential was checked and the check was recorded, one line${s.pass ? ", against your pass — the worker sees \"" + s.pass.name + "\"" : ""}.`}
        who='Nobody has to. The worker can see this check in their own "who checked me" trail.'
        when={v.offline ? "No trail line exists: this check stayed on this device." : "The trail line exists already — it was written with the verdict."}
        told="You will not be — the answer above is the whole of what a pass-only verifier gets."
        ifnot={v.offline ? "if the signature did not verify: the credential is not authentic under the cached issuer key." : 'if the result was "not valid": that is an answer too, and it was recorded the same way. A failed check never quietly disappears.'}
      />
      <div className="btn-row">
        <button className="btn secondary" onClick={() => nav("/v1_2")}>
          Check another
        </button>
      </div>
    </>
  );
}

export { tick };
