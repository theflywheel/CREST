#!/usr/bin/env python3
"""Generate the PoC batch: a month of bednet distribution as a district health
information system would export it.

Deterministic on purpose — no randomness, no clock. A fixture that differs
between runs is a fixture nobody can diff when a run goes wrong.

Every anomaly in the output is deliberate and listed in README.md beside it.
Real exports are not clean, and a proof of concept that only ever ingests a
clean file proves the happy path and nothing about the queues, the holds and
the rejections that a pilot will actually spend its time on.
"""
import csv
import io
import sys

# The header a partner's export mapping produces. The first nine columns are
# what CREST's adapter reads; the rest are the source system's own and are kept
# verbatim as enrichment, because a column CREST does not recognise is still
# information — and one of them, household_id, is what the bednet definition's
# tier map requires.
HEADER = [
    "activity", "outcome_value", "outcome_unit", "worker_id_kind", "worker_id",
    "period_start", "period_end", "geography", "source_record_ref",
    "household_id", "beneficiary_count", "supervisor_present",
    "org_unit", "org_unit_code", "event_uid", "stored_by", "last_updated",
]

WARDS = [
    ("Riverside/Ward-3", "Riverside Ward 3 CHW Team", "RIV-W3"),
    ("Riverside/Ward-4", "Riverside Ward 4 CHW Team", "RIV-W4"),
    ("Riverside/Ward-7", "Riverside Ward 7 CHW Team", "RIV-W7"),
]

# Twelve community health workers, by phone. Phones rather than national
# identifiers deliberately: the rule that no raw national identifier is ever
# persisted applies to fixtures too, and a file carrying twelve of them would
# break it before the pipeline ever saw it.
WORKERS = [f"+1555030{n:04d}" for n in range(1, 13)]

# Three workers the registry has never heard of. A real export contains these:
# somebody joined the programme last week, or their number changed. They are the
# unclear queue's whole reason for existing.
STRANGERS = ["+15550409001", "+15550409002", "+15550409003"]


def rows():
    out = []
    ref = 1000

    def add(**kw):
        nonlocal ref
        ref += 1
        row = {
            "activity": "bednet-distribution",
            "outcome_unit": "bednets-distributed",
            "worker_id_kind": "phone",
            "period_end": "",
            "supervisor_present": "yes",
            "beneficiary_count": "",
            "stored_by": "riverside.data.clerk",
            "last_updated": "2026-03-23T09:14:00Z",
            "source_record_ref": f"riverside-dhis2-{ref}",
            "event_uid": f"evt{ref:08d}",
        }
        row.update(kw)
        out.append(row)
        return row

    # Three weeks of ordinary work. Twelve workers, three wards, one row per
    # worker per distribution day.
    day = 2
    for week in range(3):
        for i, worker in enumerate(WORKERS):
            ward, unit, code = WARDS[i % len(WARDS)]
            d = day + week * 7 + (i % 3)
            add(
                outcome_value=str(6 + (i * 3 + week * 2) % 11),
                worker_id=worker,
                period_start=f"2026-03-{d:02d}",
                period_end=f"2026-03-{d:02d}T17:00:00Z" if i % 2 == 0 else "",
                geography=ward,
                household_id=f"HH-{week}{i:03d}",
                beneficiary_count=str(2 + (i % 5)),
                org_unit=unit,
                org_unit_code=code,
            )

    # --- deliberate anomalies, each named in README.md -----------------------

    # 1. The same event exported twice. Every scheduled export that overlaps the
    #    previous window does this, and paying twice for it is the failure the
    #    dedupe key exists to prevent.
    duplicate = dict(out[4])
    out.append(duplicate)

    # 2. Workers the registry does not know. These must reach the unclear queue
    #    with their rows kept, not be dropped: somebody did this work.
    for i, stranger in enumerate(STRANGERS):
        add(
            outcome_value=str(5 + i),
            worker_id=stranger,
            period_start=f"2026-03-{17 + i:02d}",
            geography=WARDS[i % len(WARDS)][0],
            household_id=f"HH-9{i:02d}",
            beneficiary_count="3",
            org_unit=WARDS[i % len(WARDS)][1],
            org_unit_code=WARDS[i % len(WARDS)][2],
        )

    # 3. A day somebody worked and distributed nothing. Zero is a real outcome,
    #    not a missing one, and it must be held with a reason rather than paid
    #    as zero or silently dropped.
    add(
        outcome_value="0",
        worker_id=WORKERS[2],
        period_start="2026-03-19",
        geography=WARDS[2][0],
        household_id="HH-410",
        beneficiary_count="0",
        org_unit=WARDS[2][1],
        org_unit_code=WARDS[2][2],
    )

    # 4. A ward name containing a comma. Quoted correctly by the exporter, and
    #    the row that catches a naive split(",").
    add(
        outcome_value="8",
        worker_id=WORKERS[5],
        period_start="2026-03-20",
        geography="Riverside/Ward-7, Block B",
        household_id="HH-411",
        beneficiary_count="4",
        org_unit=WARDS[2][1],
        org_unit_code=WARDS[2][2],
    )

    # 5. A date the source system wrote in its own local format. One row of a
    #    thousand, and it must reject that row alone rather than the file.
    add(
        outcome_value="7",
        worker_id=WORKERS[7],
        period_start="20/03/2026",
        geography=WARDS[1][0],
        household_id="HH-412",
        org_unit=WARDS[1][1],
        org_unit_code=WARDS[1][2],
    )

    # 6. A negative count from a correction entered as a subtraction. Refused:
    #    a negative quantity of work distributed is not a thing that happened.
    add(
        outcome_value="-3",
        worker_id=WORKERS[8],
        period_start="2026-03-21",
        geography=WARDS[0][0],
        household_id="HH-413",
        org_unit=WARDS[0][1],
        org_unit_code=WARDS[0][2],
    )

    # 7. An empty worker identifier — a clerk tabbed past the field.
    add(
        outcome_value="5",
        worker_id="",
        period_start="2026-03-21",
        geography=WARDS[0][0],
        household_id="HH-414",
        org_unit=WARDS[0][1],
        org_unit_code=WARDS[0][2],
    )

    # 8. Whitespace around values, as a spreadsheet leaves it.
    add(
        outcome_value=" 9 ",
        worker_id="  " + WORKERS[10] + "  ",
        period_start=" 2026-03-22 ",
        geography=" " + WARDS[1][0] + " ",
        household_id=" HH-415 ",
        beneficiary_count=" 3 ",
        org_unit=WARDS[1][1],
        org_unit_code=WARDS[1][2],
    )

    # 9. A row whose period ends before it starts.
    add(
        outcome_value="4",
        worker_id=WORKERS[11],
        period_start="2026-03-22",
        period_end="2026-03-21T17:00:00Z",
        geography=WARDS[2][0],
        household_id="HH-416",
        org_unit=WARDS[2][1],
        org_unit_code=WARDS[2][2],
    )

    return out


def main():
    # The roster is written beside the batch, holding only the twelve workers
    # somebody actually enrolled. The three strangers in the file are NOT in it,
    # and that is the point: the unclear queue exists because a source system
    # names people the registry has never met, and a PoC that registers everyone
    # the file mentions has quietly deleted its own hardest case.
    if len(sys.argv) > 1 and sys.argv[1] == "--roster":
        sys.stdout.write("\n".join(WORKERS) + "\n")
        return

    buf = io.StringIO()
    # CRLF, because that is what an export from a Windows data clerk's machine
    # actually contains and it must not change a single outcome.
    w = csv.DictWriter(buf, fieldnames=HEADER, lineterminator="\r\n")
    w.writeheader()
    for row in rows():
        w.writerow({k: row.get(k, "") for k in HEADER})
    sys.stdout.write(buf.getvalue())


if __name__ == "__main__":
    main()
