# The PoC batch

`riverside-dhis2-march-2026.csv` — three weeks of bednet distribution across
three wards of one district, shaped the way a district health information
system exports it. 47 rows, 15 workers named, 12 of them enrolled.

Regenerate with `make poc-batch`; run it end to end with `make poc` against a
stack from `make e2e-up`.

## What this is not

**It does not satisfy [#25](../../../../issues/25).** That gate asks for *a real
CSV from a real source system* and *no hand-authored data anywhere in the path*.
This file was written here. It is a rehearsal for the gate, not the gate.

Two things it is honest about being:

- **The column names are CREST's, not DHIS2's.** A real export would carry
  `orgUnit`, `eventDate`, `dataElement` and the rest, and something would have
  to map them. CREST has no mapping layer — the CSV adapter reads the canonical
  column names directly — so a partner would configure their export to emit
  these. That is a real constraint on "the CSV adapter unblocks every source
  system", and worth knowing before a partner conversation.
- **The trailing columns are theirs.** `org_unit`, `org_unit_code`, `event_uid`,
  `stored_by` and `last_updated` are what a source system actually carries.
  CREST does not recognise them and keeps them verbatim as enrichment, which is
  the behaviour that matters: a column CREST does not understand is still
  information, and one of them — `household_id` — is what the bednet
  definition's tier map requires.

## The anomalies, and why each is here

A demo that ingests a clean file proves the happy path. A pilot spends its time
on everything else, so every one of these is deliberate:

| # | Row | What it exercises |
|---|---|---|
| 1 | The same event exported twice | Every scheduled export that overlaps its previous window does this. Deduplicated on content: one unit, not two payments. |
| 2 | Three workers the registry has never met | Somebody joined last week, or their number changed. Their rows must reach the unclear queue **with the work kept** — somebody did it. |
| 3 | A day with an outcome of zero | Zero is a real outcome, not a missing one. Held with a reason and an owner, never paid as zero and never dropped. |
| 4 | A ward name containing a comma | Quoted correctly by the exporter. The row that catches a naive `split(",")`. |
| 5 | A date in the local format `20/03/2026` | One row of a thousand. Must reject **that row alone**, not the file. |
| 6 | A negative count from a correction | A negative quantity of work distributed is not a thing that happened. |
| 7 | An empty worker identifier | A clerk tabbed past the field. |
| 8 | Whitespace around every value | What a spreadsheet leaves behind. Must not change a single outcome. |
| 9 | A period ending before it starts | |

The file is CRLF-terminated, because that is what an export from a Windows data
clerk's machine contains.

## What it deliberately does not contain

**No national identifiers.** The rule that no raw national identifier is ever
persisted applies to fixtures too, and a file carrying twelve of them would
break it before the pipeline ever saw it. Workers join on phone numbers.

## The roster

`roster.txt` holds the twelve workers somebody actually enrolled. **The three
strangers in the batch are not in it**, and that is the point: registering
everyone the file mentions would quietly delete the unclear queue's whole reason
for existing.

## What a run looks like

```
ingest: 47 rows in — 40 accepted, 7 unclear, 0 refused
  deduped 1 row(s): the same work exported twice is one unit, not two payments
confirmation: 39 of 39 windows opened
T=7: 39 windows due, 39 auto-confirmed, 0 held because the worker was never reached
  held     nothing_to_pay: this record's outcome is zero, so there is nothing to pay for it
out: 39 credentials, 39 printable cards, 39 payment instructions (38 released, 1 held)

Every exit released payment: true
Nothing was silently dropped: true
Every held payment has a reason and an owner: true
```
