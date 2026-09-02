// V-1, the pass-only verifier (v1_1–v1_3), ported 1:1 from apps/verify.
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { links } from "@crest/api";
import { Chip, KV, Sidecar, OpenNote, NextBlock, DisLi } from "@crest/ui";
import { useVerify, loadSampleCredential, short, day, type Verdict } from "../state";

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
  const nav = useNavigate();
  return (
    <>
      <div className="eyebrow">V-1 · Screen 1 of 3</div>
      <h2 className="scr-title">Get a pass to check credentials</h2>
      <p className="body-2">
        A verifier pass identifies you without onboarding you. It puts a name on your checks — the worker sees{" "}
        <em>who</em> looked, in their own "who checked me" trail — but it grants nothing: no accreditation ceiling, no
        batch rights, no vetting. Identified, not onboarded.
      </p>
      <KV
        rows={[
          ["A pass adds", "your name on every check the worker sees"],
          ["A pass does not add", "any trust — the answer rides the signature either way"],
          ["Onboarding (V-2) adds", "an accreditation ceiling and batch checking"],
        ]}
      />
      <OpenNote>
        <b>Not backed yet.</b> Pass issuance has no endpoint — the services expose verification (
        <span className="mono">/v1/verify</span>, chain reads) but no <span className="mono">/v1/verifier-passes</span>{" "}
        or equivalent, and the PoC's verifier face never issues one. This screen is the design's shape only. In this
        demo the check itself works without a pass: continue to the next screen and check a credential logged out.
      </OpenNote>
      <div className="btn-row">
        <button className="btn" onClick={() => nav("/v1_2")}>
          Check a credential without a pass
        </button>
      </div>
    </>
  );
}

export function V12() {
  const s = useVerify();
  const nav = useNavigate();
  const [party, setParty] = useState("");
  const [cred, setCred] = useState("");
  const [who, setWho] = useState("");
  const [why, setWhy] = useState("");
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
      await s.runVerify(JSON.parse(cred), who.trim(), why.trim());
      nav("/v1_3");
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <>
      <div className="eyebrow">V-1 · Screen 2 of 3</div>
      <h2 className="scr-title">Scan or enter the credential</h2>
      <p className="body-2">
        Paste the credential exactly as scanned from the worker's printed card or wallet. A bare check needs no account
        and no consent beyond the showing itself.
      </p>
      <div className="card">
        <form id="verifyform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <input
              placeholder="…or borrow one: worker party id"
              value={party}
              onChange={(e) => setParty(e.target.value)}
              style={{ flex: 1 }}
            />
            <button type="button" className="btn secondary" id="loadsample" style={{ width: "auto", padding: "10px 14px" }} onClick={loadSample}>
              Load their newest credential
            </button>
          </div>
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
          <div style={{ display: "flex", gap: 8 }}>
            <input placeholder="who is asking (party id, optional)" value={who} onChange={(e) => setWho(e.target.value)} style={{ flex: 1 }} />
            <input placeholder="why (optional — recorded for the worker)" value={why} onChange={(e) => setWhy(e.target.value)} style={{ flex: 1 }} />
          </div>
          <button className="btn">Check it</button>
        </form>
      </div>
      <Sidecar>
        Every check — with a purpose or without one — leaves a line in the worker's own trail. That is by design: the
        record of who looked belongs to the person looked at.
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
  return (
    <>
      <div className="eyebrow">V-1 · Screen 3 of 3</div>
      <h2 className="scr-title">{v.valid ? "Verified" : "Not verified"}</h2>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        <VerdictChip v={v} />
        {v.valid ? <Chip kind={`tier${v.tier || 3}`}>Tier {v.tier ?? "—"} — computed now, never stored</Chip> : null}
        {v.revoked ? <Chip kind="err">Withdrawn</Chip> : null}
        {(v.contested || []).length ? <Chip kind="warn">Contested — the record, not the money</Chip> : null}
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
        happened="The credential was checked and the check was recorded, one line, even for a bare scan."
        who='Nobody has to. The worker can see this check in their own "who checked me" trail.'
        when="The trail line exists already — it was written with the verdict."
        told="You will not be — the answer above is the whole of what a pass-only verifier gets."
        ifnot='if the result was "not valid": that is an answer too, and it was recorded the same way. A failed check never quietly disappears.'
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
