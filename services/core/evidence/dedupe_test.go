package evidence

import (
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/pii"
	"github.com/theflywheel/crest/pkg/schema"
)

// The hasher is normally built at route registration, from deployment
// configuration. These tests never start a service, so they supply their own.
func init() {
	h, err := pii.NewHasher("a-salt-for-tests-only", "test-1")
	if err != nil {
		panic(err)
	}
	hasher = h
}

func end(t time.Time) *time.Time { return &t }
func ref(s string) *string       { return &s }

// The duplicate golden file's other half (#41). The adapter passes a repeated
// row through unjudged; this is where the judgement is made, and it is made on
// content rather than on arrival order or batch identity — the same work
// reaching us twice, down two different transports, is one unit.
//
// The distinguishing case matters more than the colliding one. A key that
// collides too eagerly does not merely lose a row: it erases work somebody did,
// silently, and the worker has no way to see that it happened.
func TestTheSameRecordTwiceCollidesOnTheDedupeKey(t *testing.T) {
	unit := schema.Unit{
		ContextID:  "01JB0000000000000000000000",
		Definition: schema.VersionedRef{ID: "bednet-distribution", Version: 1},
		Outcome:    schema.Outcome{Value: 12, Unit: "bednets-distributed"},
		Period: schema.Period{
			Start: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
			End:   end(time.Date(2026, 3, 2, 17, 0, 0, 0, time.UTC)),
		},
	}
	rec := schema.CanonicalWorkEvidenceRecord{
		Activity: "bednet-distribution",
		WorkerJoiningIdentifier: schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifier{
			Kind:  schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindPhone,
			Value: "+15550100011",
		},
		Provenance: schema.Provenance{SourceRecordRef: ref("riverside-dhis2-0001")},
	}

	first := dedupeKey(unit, rec)

	// The same work, arriving again by another route on another day. Neither
	// the transport nor the receipt time is an input, and that is deliberate:
	// §8 says transport never influences a record's treatment.
	again := rec
	again.Provenance.SourceExposure = schema.SourceExposurePushAPI
	again.Provenance.ReceivedAt = time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC)
	if got := dedupeKey(unit, again); got != first {
		t.Errorf("the same work down a second transport produced a different key — it would be paid twice")
	}

	for name, mutate := range map[string]func(*schema.Unit, *schema.CanonicalWorkEvidenceRecord){
		"a different outcome": func(u *schema.Unit, _ *schema.CanonicalWorkEvidenceRecord) {
			u.Outcome.Value = 7
		},
		"a different source record reference": func(_ *schema.Unit, r *schema.CanonicalWorkEvidenceRecord) {
			r.Provenance.SourceRecordRef = ref("riverside-dhis2-0004")
		},
		"a different worker": func(_ *schema.Unit, r *schema.CanonicalWorkEvidenceRecord) {
			r.WorkerJoiningIdentifier.Value = "+15550100012"
		},
		"a different period": func(u *schema.Unit, _ *schema.CanonicalWorkEvidenceRecord) {
			u.Period.Start = u.Period.Start.AddDate(0, 0, 1)
		},
		"a different context": func(u *schema.Unit, _ *schema.CanonicalWorkEvidenceRecord) {
			u.ContextID = "01JB0000000000000000000001"
		},
		"a later version of the definition": func(u *schema.Unit, _ *schema.CanonicalWorkEvidenceRecord) {
			u.Definition.Version = 2
		},
	} {
		t.Run(name, func(t *testing.T) {
			u, r := unit, rec
			mutate(&u, &r)
			if dedupeKey(u, r) == first {
				t.Errorf("%s collided with the original: real work would be discarded as a duplicate", name)
			}
		})
	}
}

// The rule that applies to fixtures too: a national identifier never reaches
// storage. redact runs before a record is written — including into the unclear
// queue, which is the one row whose identifier nobody has resolved yet and so
// the one place a raw number could plausibly come to rest.
func TestRedactReplacesANationalIdentifierBeforeAnythingIsStored(t *testing.T) {
	raw := "1234-5678-9012"
	rec := schema.CanonicalWorkEvidenceRecord{
		Activity: "bednet-distribution",
		WorkerJoiningIdentifier: schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifier{
			Kind:  schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindNationalID,
			Value: raw,
		},
	}

	got := redact(rec).WorkerJoiningIdentifier.Value
	if got == raw {
		t.Fatalf("the raw national identifier survived redaction")
	}
	if got != hashNationalID(raw) {
		t.Errorf("redacted to %q, want the salted hash — re-attribution matches on the hash "+
			"the registry holds, so anything else strands the row", got)
	}

	// A phone number is not a national identifier and must not be mangled into
	// one: an unclear row nobody can read is an unclear row nobody resolves.
	phone := rec
	phone.WorkerJoiningIdentifier.Kind = schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindPhone
	phone.WorkerJoiningIdentifier.Value = "+15550100011"
	if redact(phone).WorkerJoiningIdentifier.Value != "+15550100011" {
		t.Errorf("a phone number was hashed; only national identifiers are")
	}
}
