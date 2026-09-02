// The Definition Approver's surface (reference P-3 ratification screens
// p3_15/p3_16). The approver ratifies what an author drafted and may never
// draft it — so this view is a read of the definition as it stands, with the
// ratification act itself named as the gap it is: no ratification endpoint
// exists yet, and nothing here pretends one does.
import { useEffect, useState } from "react";
import { api, FIX } from "@crest/api";
import { Chip, ErrBar, KV, OpenNote, Sidecar } from "@crest/ui";

export function Ratify() {
  const [def, setDef] = useState<any | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    api
      .get("definitions", `/v1/definitions/${FIX.definition}`)
      .then(setDef, (e: any) => setErr(String(e?.message || e)));
  }, []);
  return (
    <>
      <div className="pagehead">
        <h2 className="scr-title m">Ratify the definition</h2>
        <Chip kind="info">approver — reads everything, drafts nothing</Chip>
      </div>
      <p className="muted" style={{ maxWidth: 640 }}>
        Ratification is a signature over what the author drafted, exactly as drafted. An approver who could edit
        while approving would be an author with a second hat — the reference keeps the two apart, and so does this
        console: the authoring wizard is not in this session's navigation at all.
      </p>
      {err ? <ErrBar>{err}</ErrBar> : null}
      {def ? (
        <div className="card" id="ratify-read">
          <KV
            rows={[
              ["Definition", <span className="mono">{def.id}</span>],
              ["Version", String(def.version ?? "—")],
              [
                "Activity",
                def.activity?.label || def.activity?.code || def.name || def.title || "—",
              ],
              ["Drafted by", "the work definition author (a separate session in this console)"],
            ]}
          />
        </div>
      ) : !err ? (
        <p className="muted">Reading the definition as the author left it…</p>
      ) : null}
      <OpenNote>
        <b>Honest gap — no ratification endpoint exists (traceability p3_15, p3_16).</b> The definitions service has
        no draft/ratify state machine: the seeded definition is already live. What the reference asks for — a
        signature that turns a draft into the one version sources register against — is L1 work; this screen shows
        the read the approver would sign over, and refuses to fake the signature.
      </OpenNote>
      <Sidecar>
        When ratification lands, the rule it must keep: the approver ratifies a named version, the author cannot
        approve their own draft, and a source registers against exactly one ratified version (reference p3_28).
      </Sidecar>
    </>
  );
}
