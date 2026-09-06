// The verifier's half of per-share consent (§9; the loop that proves w1_19,
// w1_15 and w1_20 end to end): an onboarded institution asks to see more than
// the bare QR gives, waits on the worker's per-share answer, and collects
// exactly what was approved — once.
//
// The minimal honest surface: create a request (subject + readable purpose),
// read back one's own requests with their derived state and the SAME resolved
// disclosureList the worker sees, and collect. A collect before the worker
// approves is refused by the service (409 not_approved) and that refusal is
// shown, not smoothed over — nothing is shared before approval, structurally.
import { useEffect, useState } from "react";
import { api } from "@crest/api";
import { Chip, KV, OpenNote, Sidecar } from "@crest/ui";
import { short, day, useVerify } from "../state";

type ShareView = {
  request: {
    id: string;
    subjectPartyId: string;
    purpose: string;
    approvedCredentialIds?: string[];
    declineReason?: string;
    createdAt: string;
    expiresAt: string;
  };
  state: string;
  disclosureList: Array<{ credentialId: string; issuedAt: string; revoked: boolean }> | null;
};

export function Requests() {
  const s = useVerify();
  const [subject, setSubject] = useState<string>("");
  const [purpose, setPurpose] = useState("");
  const [list, setList] = useState<ShareView[] | null>(null);
  const [collected, setCollected] = useState<Record<string, any>>({});

  const refresh = async () => {
    try {
      const out = await api.get(
        "verification",
        `/v1/presentation-requests?requestedByPartyId=${encodeURIComponent(s.orgParty?.id || "")}`,
      );
      setList((out.requests || []).slice().reverse());
    } catch (e) {
      s.fail(e);
    }
  };
  // The org session is established by the shell on entering a V-2 route;
  // poll once it holds so the list read carries the institution's own token.
  useEffect(() => {
    if (s.orgSession) void refresh();
  }, [s.orgSession]); // eslint-disable-line react-hooks/exhaustive-deps

  const create = async (ev: React.FormEvent) => {
    ev.preventDefault();
    s.clearErr();
    try {
      await api.post("verification", "/v1/presentation-requests", {
        subjectPartyId: subject.trim(),
        requestedByPartyId: s.orgParty?.id,
        purpose: purpose.trim(),
      });
      setPurpose("");
      await refresh();
    } catch (e) {
      s.fail(e);
    }
  };

  const collect = async (id: string) => {
    s.clearErr();
    try {
      const out = await api.post("verification", `/v1/presentation-requests/${encodeURIComponent(id)}/collect`);
      setCollected((c) => ({ ...c, [id]: out }));
      await refresh();
    } catch (e) {
      s.fail(e);
    }
  };

  return (
    <>
      <h2 className="scr-title m">Ask to see more</h2>
      <p className="body-2">
        The bare QR already proves the work is real. Anything past it needs the worker's say — per share, every time.
        Your ask names you and your purpose, because the worker reads both before deciding.
      </p>
      <div className="card">
        <form id="shareform" onSubmit={create} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <label className="body-2">
            Whose record (party id)
            <input name="sharesubject" value={subject} onChange={(e) => setSubject(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
          </label>
          <label className="body-2">
            Why — the worker reads this sentence (10–200 characters)
            <input
              name="sharepurpose"
              value={purpose}
              onChange={(e) => setPurpose(e.target.value)}
              placeholder="e.g. Hiring for a private clinic"
              style={{ width: "100%", marginTop: 4 }}
            />
          </label>
          <button className="btn" id="share-create" type="submit">
            Send the request
          </button>
        </form>
      </div>
      <Sidecar>
        The list on each request below is resolved by the service once, for both faces: the worker decides against the
        very list you see. Documents move only on an approved collect, only to you, and only once.
      </Sidecar>
      {(list || []).map((v) => (
        <div className="card" key={v.request.id} data-vshare={v.request.id}>
          <div style={{ display: "flex", justifyContent: "space-between", gap: 10 }}>
            <span style={{ font: "500 13.5px/1.4 Roboto" }}>
              {short(v.request.subjectPartyId)} · “{v.request.purpose}”
            </span>
            <Chip sm kind={v.state === "FULFILLED" ? "ok" : v.state === "APPROVED" ? "ok" : v.state === "DECLINED" ? "warn" : "plain"}>
              {v.state}
            </Chip>
          </div>
          <div className="muted">
            asked {day(v.request.createdAt)} · {(v.disclosureList || []).length} on the disclosure list
            {v.request.approvedCredentialIds ? ` · ${v.request.approvedCredentialIds.length} approved` : ""}
            {v.request.declineReason ? ` · declined: “${v.request.declineReason}”` : ""}
          </div>
          {v.state === "REQUESTED" || v.state === "APPROVED" ? (
            <div className="btn-row" style={{ marginTop: 8 }}>
              <button className="btn secondary" data-collect={v.request.id} onClick={() => collect(v.request.id)}>
                Collect
              </button>
            </div>
          ) : null}
          {collected[v.request.id] ? (
            <div style={{ marginTop: 8 }} data-collected={v.request.id}>
              <KV
                rows={[
                  ["Collected", `${collected[v.request.id].count} credential(s) — exactly the approved list, nothing else`],
                  ...(collected[v.request.id].credentials || []).map((c: any, i: number) => [
                    `Credential ${i + 1}`,
                    <span className="mono">{short(c.id)}</span>,
                  ] as [string, React.ReactNode]),
                ]}
              />
            </div>
          ) : null}
        </div>
      ))}
      {list && !list.length ? (
        <OpenNote>No requests yet from this institution. Send one above; the worker sees it the moment they look.</OpenNote>
      ) : null}
    </>
  );
}
