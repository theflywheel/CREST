package parties

import (
	"errors"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// The project surface's decisions, tested where they are made: pure functions
// over a Context document. Every table below has the negative case in it,
// because a state machine tested only on its happy path is a state machine
// whose refusals are comments.

// Synthetic identifiers, spelling the fixture's people in the ULID alphabet
// (which has no I, L, O or U — hence MTAWA for the Configurator). No real
// identity system issues these, which is the point: nothing in a project
// fixture may resemble a national identifier.
var (
	orgMinistry  = "did:crest:party:01JCRESTMHKENYA00000000000"
	personPeter  = "did:crest:party:01JCRESTPETER0000000000000"
	personAlice  = "did:crest:party:01JCRESTMTAWA0000000000000"
	personSarah  = "did:crest:party:01JCRESTSARAH0000000000000"
	projectID    = "crest:context:01JCRESTBEDNET260000000000"
	handoverTime = time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	answerTime   = time.Date(2026, 9, 3, 11, 30, 0, 0, time.UTC)
)

// bednetProject is the fixture named after its situation: the project p1_3
// creates, before anybody has answered the handover.
func bednetProject() schema.Context {
	return schema.Context{
		ID:           projectID,
		Kind:         contextKindProject,
		Name:         "Malaria Bednet Campaign — 2026",
		OwnerPartyID: orgMinistry,
		Period:       schema.Period{Start: handoverTime},
		// Coverage is L2 and lives here, opaque. The core stores the words
		// "Ward 4, Ward 7 · Kisumu County" and never parses them.
		Configuration: map[string]any{"coverage": "Ward 4, Ward 7 · Kisumu County"},
		State:         schema.ContextStateDRAFT,
	}
}

func handedToAlice() schema.Context {
	c, _ := nameConfigurator(bednetProject(), personAlice, personPeter, handoverTime)
	return c
}

// ── The handover (design finding F2) ────────────────────────────────────────

func TestNamingAConfiguratorIsAProposalNotAnAssignment(t *testing.T) {
	c, ev := nameConfigurator(bednetProject(), personAlice, personPeter, handoverTime)
	if c.Ownership == nil {
		t.Fatal("naming a configurator must record an ownership state")
	}
	if c.Ownership.State != schema.ContextOwnershipStatePENDING {
		t.Fatalf("a named configurator starts PENDING, got %q", c.Ownership.State)
	}
	if c.Ownership.NamedByPartyID != personPeter || c.Ownership.PartyID != personAlice {
		t.Fatal("both sides of the handover must be recorded: who named, and who was named")
	}
	if !c.Ownership.NamedAt.Equal(handoverTime) {
		t.Fatalf("namedAt must be the service clock, got %v", c.Ownership.NamedAt)
	}
	if ev.Event != ownershipNamed || ev.ActorPartyID != personPeter {
		t.Fatalf("the trail event must say who named whom, got %+v", ev)
	}
	if err := schema.Validate(schema.IDContext, c); err != nil {
		t.Fatalf("a handed-over project must still be a valid Context: %v", err)
	}
}

func TestOwnershipDecisions(t *testing.T) {
	declined, _, err := decideOwnership(handedToAlice(), false,
		"I am not running this campaign — Dr. Kimani is", personAlice, answerTime)
	if err != nil {
		t.Fatalf("declining is a first-class outcome, not an error: %v", err)
	}

	for _, tc := range []struct {
		name    string
		in      schema.Context
		accept  bool
		reason  string
		actor   string
		wantErr error
	}{
		{
			name: "accepting records the answer", in: handedToAlice(),
			accept: true, actor: personAlice,
		},
		{
			name: "declining with a reason", in: handedToAlice(),
			reason: "not my campaign", actor: personAlice,
		},
		{
			name: "declining with no reason is refused", in: handedToAlice(),
			actor: personAlice, wantErr: errDeclineNeedsReason,
		},
		{
			name: "declining with whitespace for a reason is refused", in: handedToAlice(),
			reason: "   \t ", actor: personAlice, wantErr: errDeclineNeedsReason,
		},
		{
			name: "somebody else cannot answer for the named party", in: handedToAlice(),
			accept: true, actor: personSarah, wantErr: errNotTheConfigurator,
		},
		{
			name: "a project nobody was handed has nothing to answer", in: bednetProject(),
			accept: true, actor: personAlice, wantErr: errNoHandover,
		},
		{
			name: "a settled handover stays settled", in: declined,
			accept: true, actor: personAlice, wantErr: errOwnershipAlreadyDecided,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, ev, err := decideOwnership(tc.in, tc.accept, tc.reason, tc.actor, answerTime)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			want := schema.ContextOwnershipStateDECLINED
			wantEvent := ownershipDeclined
			if tc.accept {
				want, wantEvent = schema.ContextOwnershipStateACCEPTED, ownershipAccepted
			}
			if out.Ownership.State != want {
				t.Fatalf("state: want %q, got %q", want, out.Ownership.State)
			}
			if ev.Event != wantEvent {
				t.Fatalf("event: want %q, got %q", wantEvent, ev.Event)
			}
			if out.Ownership.DecidedAt == nil || !out.Ownership.DecidedAt.Equal(answerTime) {
				t.Fatal("an answer must carry when it was given")
			}
			if err := schema.Validate(schema.IDContext, out); err != nil {
				t.Fatalf("the answered context must validate: %v", err)
			}
		})
	}
}

// The rule the reference's callout states in as many words: declining returns
// the project to the Org Admin's queue rather than deleting anything.
func TestDecliningDestroysNothingAndNamesWhoDeclined(t *testing.T) {
	before := handedToAlice()
	after, ev, err := decideOwnership(before, false, "not my campaign", personAlice, answerTime)
	if err != nil {
		t.Fatalf("decline: %v", err)
	}
	if after.ID != before.ID || after.Name != before.Name || after.State != before.State {
		t.Fatal("a decline must leave the project itself untouched")
	}
	if after.Configuration["coverage"] != before.Configuration["coverage"] {
		t.Fatal("a decline must not disturb the project's configuration")
	}
	if after.Ownership.PartyID != personAlice {
		t.Fatal("the declining party must stay named, or 'who declined' is unreadable")
	}
	if after.Ownership.Reason == nil || *after.Ownership.Reason != "not my campaign" {
		t.Fatal("a decline must carry its reason")
	}
	if ev.Reason == nil || ev.PartyID != personAlice {
		t.Fatal("the trail must carry the reason and the party who gave it")
	}
}

// After a re-handover the current view says PENDING again — which is exactly
// why the trail exists, and why the test asserts the previous answer is
// carried out of the function rather than dropped.
func TestReHandoverAfterADeclineReturnsToPending(t *testing.T) {
	declined, _, err := decideOwnership(handedToAlice(), false, "not mine", personAlice, answerTime)
	if err != nil {
		t.Fatalf("decline: %v", err)
	}
	renamed, ev := nameConfigurator(declined, personSarah, personPeter, answerTime.Add(time.Hour))
	if renamed.Ownership.State != schema.ContextOwnershipStatePENDING {
		t.Fatalf("a re-handover starts PENDING, got %q", renamed.Ownership.State)
	}
	if renamed.Ownership.PartyID != personSarah {
		t.Fatal("the new configurator must be the one named")
	}
	if renamed.Ownership.Reason != nil {
		t.Fatal("the new handover must not inherit the previous decline's reason")
	}
	if ev.Event != ownershipNamed || ev.PartyID != personSarah {
		t.Fatalf("the re-handover must append its own trail event, got %+v", ev)
	}
}

// A configurator who accepted has not thereby agreed to hand the project on.
func TestReNamingAnAcceptedProjectStillNeedsANewAcknowledgement(t *testing.T) {
	accepted, _, err := decideOwnership(handedToAlice(), true, "", personAlice, answerTime)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	renamed, _ := nameConfigurator(accepted, personSarah, personPeter, answerTime.Add(time.Hour))
	if renamed.Ownership.State != schema.ContextOwnershipStatePENDING {
		t.Fatal("handing an accepted project to somebody else must ask them too")
	}
}

// A proposal must not carry authority, or the acknowledgement is decoration.
// Found by review: the write gate originally compared party ids and never read
// the state, so a party who had DECLINED kept full write authority over the
// project's finance link, support owner, gates and partner grants — writes
// that outlive the refusal, because a re-handover cannot un-grant what the
// previous nominee did.
func TestOnlyAnAcknowledgedConfiguratorMayWrite(t *testing.T) {
	accepted, _, err := decideOwnership(handedToAlice(), true, "", personAlice, answerTime)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	declined, _, err := decideOwnership(handedToAlice(), false, "not mine", personAlice, answerTime)
	if err != nil {
		t.Fatalf("decline: %v", err)
	}

	for _, tc := range []struct {
		name             string
		in               schema.Context
		caller           string
		wantNamed        bool
		wantMayConfigure bool
	}{
		{
			name: "the accepted configurator may write", in: accepted, caller: personAlice,
			wantNamed: true, wantMayConfigure: true,
		},
		{
			name: "a merely named configurator may read but not write", in: handedToAlice(),
			caller: personAlice, wantNamed: true,
		},
		{
			name: "a configurator who declined may read but not write", in: declined,
			caller: personAlice, wantNamed: true,
		},
		{
			name: "a stranger is neither", in: accepted, caller: personSarah,
		},
		{
			name: "an unhanded project names nobody", in: bednetProject(), caller: personAlice,
		},
		{
			// The empty caller is the case that matters on a stack with
			// authentication switched off: "" must never match "".
			name: "an unauthenticated caller is never the named party", in: accepted,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNamedConfigurator(tc.in, tc.caller); got != tc.wantNamed {
				t.Fatalf("named: want %v, got %v", tc.wantNamed, got)
			}
			if got := acknowledgedConfigurator(tc.in, tc.caller); got != tc.wantMayConfigure {
				t.Fatalf("may configure: want %v, got %v", tc.wantMayConfigure, got)
			}
		})
	}
}

// ── Activation (p2_7) ───────────────────────────────────────────────────────

func TestActivationConditionsAreReadableBeforeAnythingIsAttempted(t *testing.T) {
	c, err := declareGates(handedToAlice(),
		[]string{"work definition ratified", "workers registered"}, handoverTime)
	if err != nil {
		t.Fatalf("declare gates: %v", err)
	}
	conds := activationConditions(c)
	if len(conds) != 3 {
		t.Fatalf("want the acknowledgement condition plus two gates, got %d", len(conds))
	}
	if conds[0].Name != "ownership-acknowledged" {
		t.Fatalf("the acknowledgement is a condition of activation, got %q", conds[0].Name)
	}
	for _, cond := range conds {
		if cond.Satisfied {
			t.Fatalf("nothing has been satisfied yet, but %q says it has", cond.Name)
		}
	}
}

func TestActivation(t *testing.T) {
	ready := func(t *testing.T) schema.Context {
		t.Helper()
		c, _, err := decideOwnership(handedToAlice(), true, "", personAlice, answerTime)
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		if c, err = declareGates(c, []string{"definition ratified"}, answerTime); err != nil {
			t.Fatalf("declare: %v", err)
		}
		if c, err = satisfyGate(c, "definition ratified", answerTime); err != nil {
			t.Fatalf("satisfy: %v", err)
		}
		return c
	}

	for _, tc := range []struct {
		name      string
		build     func(*testing.T) schema.Context
		wantErr   error
		wantState schema.ContextState
	}{
		{
			name: "every condition satisfied activates", build: ready,
			wantState: schema.ContextStateACTIVE,
		},
		{
			name: "an unacknowledged handover holds activation",
			build: func(*testing.T) schema.Context {
				c, _ := declareGates(handedToAlice(), nil, handoverTime)
				return c
			},
			wantErr: errGatesUnmet, wantState: schema.ContextStateDRAFT,
		},
		{
			name: "a declined handover holds activation",
			build: func(t *testing.T) schema.Context {
				c, _, err := decideOwnership(handedToAlice(), false, "no", personAlice, answerTime)
				if err != nil {
					t.Fatalf("decline: %v", err)
				}
				return c
			},
			wantErr: errGatesUnmet, wantState: schema.ContextStateDRAFT,
		},
		{
			name: "an unsatisfied gate holds activation",
			build: func(t *testing.T) schema.Context {
				c, _, err := decideOwnership(handedToAlice(), true, "", personAlice, answerTime)
				if err != nil {
					t.Fatalf("accept: %v", err)
				}
				c, err = declareGates(c, []string{"workers registered"}, answerTime)
				if err != nil {
					t.Fatalf("declare: %v", err)
				}
				return c
			},
			wantErr: errGatesUnmet, wantState: schema.ContextStateDRAFT,
		},
		{
			name: "activating an already-live project is the outcome the caller wanted",
			build: func(t *testing.T) schema.Context {
				c := ready(t)
				c.State = schema.ContextStateACTIVE
				return c
			},
			wantState: schema.ContextStateACTIVE,
		},
		{
			name: "a closed project does not re-activate",
			build: func(t *testing.T) schema.Context {
				c := ready(t)
				c.State = schema.ContextStateCLOSED
				return c
			},
			wantErr: errNotDraft, wantState: schema.ContextStateCLOSED,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, conds, err := activate(tc.build(t), answerTime)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
			if out.State != tc.wantState {
				t.Fatalf("state: want %q, got %q", tc.wantState, out.State)
			}
			// A refusal must always come with the conditions, or the screen
			// has nothing to show but the word no.
			if len(conds) == 0 && tc.wantErr != nil {
				t.Fatal("a refusal must carry the conditions that caused it")
			}
		})
	}
}

// A project with no gates and no handover is activatable, and that is correct:
// a deployment that declares no readiness conditions has said it has none.
// The test exists so that changing it is a decision rather than a drift.
func TestAProjectWithNoDeclaredConditionsActivates(t *testing.T) {
	out, _, err := activate(bednetProject(), answerTime)
	if err != nil {
		t.Fatalf("no conditions means nothing is unmet: %v", err)
	}
	if out.State != schema.ContextStateACTIVE {
		t.Fatal("want ACTIVE")
	}
}

func TestDeclaringGatesKeepsSatisfactionsAndRefusesNonsense(t *testing.T) {
	c, err := declareGates(bednetProject(), []string{"a", "b"}, handoverTime)
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	if c, err = satisfyGate(c, "a", answerTime); err != nil {
		t.Fatalf("satisfy: %v", err)
	}
	// Re-declaring with "a" still in the list must not clear its answer, and
	// dropping "b" must not resurrect it.
	c, err = declareGates(c, []string{"a", "c"}, answerTime)
	if err != nil {
		t.Fatalf("re-declare: %v", err)
	}
	if len(c.ActivationGates) != 2 {
		t.Fatalf("want two gates, got %d", len(c.ActivationGates))
	}
	if c.ActivationGates[0].SatisfiedAt == nil {
		t.Fatal("re-declaring a gate must not clear a satisfaction somebody gave")
	}
	if c.ActivationGates[1].SatisfiedAt != nil {
		t.Fatal("a newly declared gate starts unsatisfied")
	}

	if _, err := declareGates(bednetProject(), []string{"a", "a"}, handoverTime); err == nil {
		t.Fatal("one condition, one row: a duplicate gate must be refused")
	}
	if _, err := declareGates(bednetProject(), []string{"  "}, handoverTime); err == nil {
		t.Fatal("a gate with no name is a condition nobody can satisfy")
	}
	if _, err := satisfyGate(bednetProject(), "never declared", answerTime); err == nil {
		t.Fatal("a gate that appears when satisfied never gated anything")
	}
}

func TestSatisfyingAGateTwiceKeepsTheFirstAnswer(t *testing.T) {
	c, err := declareGates(bednetProject(), []string{"a"}, handoverTime)
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	if c, err = satisfyGate(c, "a", answerTime); err != nil {
		t.Fatalf("satisfy: %v", err)
	}
	later := answerTime.Add(48 * time.Hour)
	if c, err = satisfyGate(c, "a", later); err != nil {
		t.Fatalf("re-satisfy: %v", err)
	}
	if !c.ActivationGates[0].SatisfiedAt.Equal(answerTime) {
		t.Fatal("when a condition was met is a fact; re-asserting it must not move the date")
	}
}

// ── p2_18: narrower than the terms ──────────────────────────────────────────

func TestAGrantCannotExceedTheTermsTheOrganisationAccepted(t *testing.T) {
	terms := []string{"register-workers", "submit-evidence", "confirm-work"}
	for _, tc := range []struct {
		name        string
		functions   []string
		wantMissing []string
	}{
		{name: "a strict subset is narrower", functions: []string{"submit-evidence"}},
		{name: "the whole permission set is still not wider", functions: terms},
		{name: "no functions covers nothing and misses nothing"},
		{
			name:        "a function the terms never carried is refused",
			functions:   []string{"submit-evidence", "issue-credentials"},
			wantMissing: []string{"issue-credentials"},
		},
		{
			name:        "every uncovered function is named, not just the first",
			functions:   []string{"pay-workers", "issue-credentials"},
			wantMissing: []string{"issue-credentials", "pay-workers"},
		},
		{
			name:        "permission strings are published identifiers, so nearly is not a match",
			functions:   []string{"Submit-Evidence"},
			wantMissing: []string{"Submit-Evidence"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := narrowerThanTerms(tc.functions, terms)
			if len(got) != len(tc.wantMissing) {
				t.Fatalf("want missing %v, got %v", tc.wantMissing, got)
			}
			for i := range got {
				if got[i] != tc.wantMissing[i] {
					t.Fatalf("want missing %v, got %v", tc.wantMissing, got)
				}
			}
		})
	}
}

// ── p2_17: the partner directory ────────────────────────────────────────────

func TestDirectoryFilters(t *testing.T) {
	amref := directoryEntry{
		PartyID: orgMinistry, DisplayName: "Amref Health Africa",
		Sector: "health", Country: "KE",
		Permissions: []string{"register-workers", "submit-evidence"},
	}
	for _, tc := range []struct {
		name string
		f    directoryFilter
		want bool
	}{
		{name: "no filters lists everybody approved", want: true},
		{name: "matching sector", f: directoryFilter{Sector: "health"}, want: true},
		{name: "a sector taxonomy is somebody's spreadsheet, so case does not decide",
			f: directoryFilter{Sector: "Health"}, want: true},
		{name: "a different sector excludes", f: directoryFilter{Sector: "education"}},
		{name: "matching country", f: directoryFilter{Country: "ke"}, want: true},
		{name: "a different country excludes", f: directoryFilter{Country: "UG"}},
		{
			name: "a permission the terms carry keeps them in the list",
			f:    directoryFilter{Permissions: []string{"submit-evidence"}}, want: true,
		},
		{
			name: "nobody appears who could not do the work",
			f:    directoryFilter{Permissions: []string{"issue-credentials"}},
		},
		{
			name: "every requested permission must be covered, not just one",
			f:    directoryFilter{Permissions: []string{"submit-evidence", "issue-credentials"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesDirectory(amref, tc.f); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// An organisation that never stated a sector has no sector. Guessing one would
// list it under a heading its own registration does not support.
func TestAnUnstatedAttributeIsAbsentRatherThanGuessed(t *testing.T) {
	if got := attrString(nil, "sector"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
	if got := attrString(map[string]any{"sector": 7}, "sector"); got != "" {
		t.Fatalf("a non-string attribute is not a sector, got %q", got)
	}
	if got := attrString(map[string]any{"sector": "health"}, "sector"); got != "health" {
		t.Fatalf("want health, got %q", got)
	}
	entry := directoryEntry{Permissions: []string{"submit-evidence"}}
	if matchesDirectory(entry, directoryFilter{Sector: "health"}) {
		t.Fatal("an organisation with no stated sector must not match a sector filter")
	}
}

// ── p2_8 and p2_10: the link and the owner ──────────────────────────────────

func TestAFinanceLinkIsAReferenceToSomebodyElsesCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   financeLink
		ok   bool
	}{
		{
			name: "a system and a code",
			in:   financeLink{System: "MoH IFMIS", Code: "4402-11-A7"}, ok: true,
		},
		{
			name: "a code with no system cannot be traced back to whoever minted it",
			in:   financeLink{Code: "4402-11-A7"},
		},
		{name: "a system with no code links nothing", in: financeLink{System: "MoH IFMIS"}},
		{name: "nothing at all", in: financeLink{}},
		{
			name: "a code of unreasonable length is not a code",
			in:   financeLink{System: "MoH IFMIS", Code: string(make([]byte, 201))},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validFinanceLink(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("want accepted, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("want refused")
			}
		})
	}
}

func TestSupportOwnershipAlwaysNamesSomebody(t *testing.T) {
	route := func(kind, value string) *struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} {
		return &struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}{kind, value}
	}
	for _, tc := range []struct {
		name string
		in   supportOwner
		ok   bool
	}{
		{name: "a named party is enough", in: supportOwner{PartyID: personSarah}, ok: true},
		{
			name: "a party with a support route of its own",
			in:   supportOwner{PartyID: personSarah, ContactRoute: route("phone", "+254700000000")},
			ok:   true,
		},
		{name: "nobody named is the dead end this record prevents", in: supportOwner{}},
		{
			name: "a contact route with no value is not a route",
			in:   supportOwner{PartyID: personSarah, ContactRoute: route("phone", "")},
		},
		{
			name: "a contact route with no kind cannot be dialled or emailed",
			in:   supportOwner{PartyID: personSarah, ContactRoute: route("", "+254700000000")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validSupportOwner(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("want accepted, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("want refused")
			}
		})
	}
}

// The core has no vocabulary to check a record name against — that is the
// layering test's answer here — so it checks only that the name is a name.
func TestRecordNamesAreSlugsAndNothingMore(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{in: "definition-origin", ok: true},
		{in: "payment-posture", ok: true},
		{in: "whatever_this_deployment_calls_it", ok: true},
		{in: ""},
		{in: "   "},
		{in: "composition:nested"},
		{in: "has spaces"},
		{in: "path/like"},
		{in: string(make([]byte, maxRecordKindLength+1))},
	} {
		t.Run(tc.in, func(t *testing.T) {
			err := validRecordKind(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("want accepted, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("want refused")
			}
		})
	}
}

// ── The primitive itself ────────────────────────────────────────────────────

// The schema, not the struct, is what refuses a malformed handover. A Go
// struct cannot express "a state is one of three words".
func TestTheSchemaRefusesAMalformedHandover(t *testing.T) {
	c := handedToAlice()
	c.Ownership.State = "MAYBE"
	if err := schema.Validate(schema.IDContext, c); err == nil {
		t.Fatal("an ownership state outside the three must be refused")
	}

	c = handedToAlice()
	c.Ownership.NamedByPartyID = "peter"
	if err := schema.Validate(schema.IDContext, c); err == nil {
		t.Fatal("a namedBy that is not a party DID must be refused")
	}

	// And a project nobody was handed is still a valid Context: ownership is
	// absent, not empty. A cohort with no configurator is a real thing.
	if err := schema.Validate(schema.IDContext, bednetProject()); err != nil {
		t.Fatalf("a project with no handover must validate: %v", err)
	}
}

// Nothing in this file may put a national identifier or a biometric anywhere
// near a project record. The fixture is the assertion: every identifier above
// is a synthetic DID no identity system would issue, and the coverage string
// is a place name rather than a person.
func TestNoProjectFixtureCarriesAPersonalIdentifier(t *testing.T) {
	c := handedToAlice()
	for key, value := range c.Configuration {
		s, isString := value.(string)
		if !isString {
			continue
		}
		for _, forbidden := range []string{"nationalId", "national-id", "biometric", "fingerprint"} {
			if key == forbidden || s == forbidden {
				t.Fatalf("a project record must never carry %q", forbidden)
			}
		}
	}
}

// ── A role holder may read the project their grant names, and only that ─────

func TestAGrantScopedToTheContextAdmitsReadsAndNothingElse(t *testing.T) {
	other := "crest:context:01JCRESTOTHERPRJ0000000000"
	scoped := func(ctx string) schema.Authorization {
		return schema.Authorization{
			PartyID:          personSarah,
			AuthorityPartyID: orgMinistry,
			Functions:        []string{"specify-definition"},
			Scope:            schema.AuthorizationScope{ContextID: &ctx},
		}
	}
	unscoped := schema.Authorization{PartyID: personSarah, AuthorityPartyID: orgMinistry}

	if !grantAdmitsRead([]schema.Authorization{scoped(projectID)}, projectID) {
		t.Fatal("a grant scoped to this context must admit a read of it — the author's wizard runs on the project's declared vocabulary")
	}
	if grantAdmitsRead([]schema.Authorization{scoped(other)}, projectID) {
		t.Fatal("a grant on a different context must not admit this one")
	}
	if grantAdmitsRead([]schema.Authorization{unscoped}, projectID) {
		t.Fatal("a grant with no context scope must not admit any project")
	}
	if grantAdmitsRead([]schema.Authorization{scoped(projectID)}, "") {
		t.Fatal("an empty context id must never match anything")
	}
	if grantAdmitsRead(nil, projectID) {
		t.Fatal("no grants, no read")
	}
}
