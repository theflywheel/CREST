// P-10, the external-institution panel (w6_1–w6_3), ported 1:1 from
// apps/verify. These render in the panel shell, outside the console chrome —
// the institution receiving a scoped link has no CREST account and sees no
// console.
import { Chip, Sidecar, OpenNote } from "@crest/ui";

const CSV_COLUMNS: Array<[string, string]> = [
  ["activity", "bednet-distribution"],
  ["outcome_value", "12"],
  ["outcome_unit", "bednets-distributed"],
  ["worker_id_kind", "phone"],
  ["worker_id", "+15550100011"],
  ["period_start", "2026-03-02"],
  ["period_end", "2026-03-02"],
  ["geography", "district-north"],
  ["source_record_ref", "HMIS-2026-03-8841"],
  ["household_id", "HH-101"],
  ["beneficiary_count", "5"],
  ["supervisor_present", "true"],
];

export function W61() {
  return (
    <>
      <div className="eyebrow">P-10 · External institution · w6_1</div>
      <h2 className="scr-title m">A request for one attestation</h2>
      <p className="body-2">
        You received an emailed, scoped link: CREST is asking your institution to attest one thing it already knows. The
        shape is a template out, a file back — the definition's own CSV columns, one row, from your records. You need no
        CREST account; the link is the scope.
      </p>
      <div className="eyebrow">The row you would return</div>
      <div className="tblwrap">
        <table className="tbl">
          <tbody>
            <tr>
              <th>Column</th>
              <th>Example</th>
            </tr>
            {CSV_COLUMNS.map(([c, ex]) => (
              <tr key={c}>
                <td className="mono">{c}</td>
                <td className="mono">{ex}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Sidecar>
        The worker id here is whatever identifier your system holds — never a national ID number. CREST resolves it and
        keeps only a pairwise reference and a salted hash.
      </Sidecar>
      <OpenNote>
        <b>Not backed yet.</b> The scoped-link request object — the thing the emailed link would resolve to, carrying
        which attestation is being asked for and from whom — has no endpoint. The evidence service accepts CSV batches
        from authenticated submitters (<span className="mono">POST /v1/batches</span>), but nothing issues or honours an
        external scoped link. This panel is the design's shape only; no file can actually be returned from here.
      </OpenNote>
    </>
  );
}

export function W62() {
  return (
    <>
      <div className="eyebrow">P-10 · External institution · w6_2–w6_3</div>
      <h2 className="scr-title m">Three tiers, four kinds of evidence</h2>
      <p className="body-2">
        What your attestation is worth is not decided by you, and not stored anywhere — it is computed from provenance
        every time someone checks.
      </p>
      <div className="card">
        <div style={{ display: "flex", gap: 8, alignItems: "flex-start" }}>
          <Chip kind="tier1">Tier 1</Chip>
          <span className="body-2">
            <b>Outcome-linked.</b> The claim rides a record in a system that exists for its own reasons — a health
            information system, a stock ledger. The evidence would exist whether or not anyone was paid for it.
          </span>
        </div>
      </div>
      <div className="card">
        <div style={{ display: "flex", gap: 8, alignItems: "flex-start" }}>
          <Chip kind="tier2">Tier 2</Chip>
          <span className="body-2">
            <b>Supervisor-entered.</b> A named person with standing attested it. Checkable against that person's
            authorization, contestable to their name.
          </span>
        </div>
      </div>
      <div className="card">
        <div style={{ display: "flex", gap: 8, alignItems: "flex-start" }}>
          <Chip kind="tier3">Tier 3</Chip>
          <span className="body-2">
            <b>Worker-asserted.</b> The worker's own account, held as exactly that. Real, kept, and labelled — never
            dressed up as more.
          </span>
        </div>
      </div>
      <div className="eyebrow">Where your institution sits</div>
      <div className="card hi">
        <p className="body-2">
          <b>External institutions are capped at Tier 2</b> — the §16 ruling. An institution's account of its own
          capture is one nobody here can check: your system may well be outcome-linked from where you stand, but from
          CREST's side that linkage is your assertion about yourself. Tier 1 needs provenance a verifier can trace past
          the teller — a registered, assessed source. An external attestation arrives as the word of a named
          institution, and the word of a named institution is what Tier 2 means.
        </p>
      </div>
      <p className="muted">
        The tier is derived at query time from provenance facts on the credential (<span className="mono">sourceClass</span>
        , <span className="mono">captureMethod</span>, <span className="mono">adapterRef</span>) against the definition's
        public tier map. If your source is later registered and assessed, existing credentials rise with it — nothing is
        reissued, because nothing was stored.
      </p>
    </>
  );
}
