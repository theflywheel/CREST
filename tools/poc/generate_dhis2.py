#!/usr/bin/env python3
"""The same month of work, exported the way DHIS2 actually names things.

Not a second fixture for its own sake. The first PoC batch carries CREST's
column names, which meant "the CSV adapter unblocks every source system"
silently required the source system to rename its columns first — the opposite
of unblocking, and a constraint that would have surfaced in a partner
conversation rather than before one.

This file carries a DHIS2 event export's own vocabulary: `eventDate`,
`orgUnitName`, `event`, and data elements named as a programme names them. It
has no column at all for the unit of measure, because that is a fact about the
programme rather than about the row.

Nothing in CREST is changed to read it. The mapping registered beside the source
(see poc-dhis2 in the Makefile) is configuration.
"""
import csv
import io
import sys

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from generate import rows, WARDS  # noqa: E402  — same month, same anomalies

# A DHIS2 event export's columns. `event` is the event uid, `eventDate` the
# occurrence date, `orgUnit`/`orgUnitName` the hierarchy, `storedBy` the clerk,
# and the last three are this programme's own data elements, named as somebody
# typed them into the metadata screen.
HEADER = [
    "event", "program", "programStage", "orgUnit", "orgUnitName",
    "eventDate", "completedDate", "status", "storedBy", "lastUpdated",
    "CHW phone number", "Bednets distributed", "Household ID",
    "Beneficiaries reached", "Supervisor present",
]


def main():
    buf = io.StringIO()
    w = csv.DictWriter(buf, fieldnames=HEADER, lineterminator="\r\n")
    w.writeheader()

    unit_code = {name: code for name, unit, code in WARDS}
    unit_name = {name: unit for name, unit, code in WARDS}

    for r in rows():
        geography = r.get("geography", "")
        w.writerow({
            "event": r.get("event_uid", ""),
            "program": "Malaria prevention 2026",
            "programStage": "Bednet distribution",
            "orgUnit": unit_code.get(geography, r.get("org_unit_code", "")),
            "orgUnitName": unit_name.get(geography, geography),
            "eventDate": r.get("period_start", ""),
            "completedDate": r.get("period_end", ""),
            "status": "COMPLETED",
            "storedBy": r.get("stored_by", ""),
            "lastUpdated": r.get("last_updated", ""),
            "CHW phone number": r.get("worker_id", ""),
            "Bednets distributed": r.get("outcome_value", ""),
            "Household ID": r.get("household_id", ""),
            "Beneficiaries reached": r.get("beneficiary_count", ""),
            "Supervisor present": r.get("supervisor_present", ""),
        })
    sys.stdout.write(buf.getvalue())


if __name__ == "__main__":
    main()
