package harness

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
)

// ErrStoryAlreadySeeded means a previous run already told the story on this
// stack. Detected rather than pushed through, because most of the story is not
// idempotent by nature — a second dispute, a second recovery, a second Devika
// would each be a different world, not the same one twice.
var ErrStoryAlreadySeeded = errors.New("story already seeded")

// storySource is the marker the story leaves behind, and the thing it checks
// for before starting. A registered source is the first durable object the
// story creates, so its presence means a run at least began — and a partial
// story is better left for `make e2e-up` (a fresh stack) than patched blind.
const storySourceRef = "riverside-dhis2"

// StoryClockAdvance is how far past the epoch the story ends: one day to the
// first batch, then seven days and change for the T=7 sweep.
//
// Exported because a demo deployment needs it to work backwards: to leave the
// story finishing at roughly the present moment, it seeds an epoch this far in
// the past and walks forward.
const StoryClockAdvance = 8*24*time.Hour + 2*time.Hour

// SeedStory populates a freshly Seed()ed stack with one coherent week of the
// programme, so every screen of the demo web app has something true to show:
// credentials, an open dispute, a held payment with an owner, an unclear row,
// a duplicate hold, an open recovery, an overdue authorization, verification
// trail entries, and unconfirmed windows for a live demo.
//
// Everything goes through the real HTTP endpoints with real authenticated
// callers, exactly as the e2e scenarios do. A story written straight into the
// database could contain states the services would have refused, and a demo
// built on it would be demonstrating the bug.
func (s *Stack) SeedStory(ctx context.Context, w *fixtures.World) error {
	st := &story{Stack: s, w: w, ctx: ctx, oidc: NewOIDC()}
	if err := st.oidc.WaitReady(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("identity provider: %w", err)
	}

	// Prior-state check: if the demo source is already registered, a story has
	// already been told here.
	var sources struct {
		Sources []struct {
			SystemRef string `json:"systemRef"`
		} `json:"sources"`
	}
	if err := s.Evidence.Get(ctx, "/v1/sources", &sources); err != nil {
		return fmt.Errorf("list sources: %w", err)
	}
	for _, src := range sources.Sources {
		if src.SystemRef == storySourceRef {
			// Seed() has just reset every clock to the epoch, which un-tells
			// the story's timeline: the overdue review date is no longer past
			// and the open windows are no longer mid-week. Put the clock back
			// where the story left it before declining to run again.
			if err := s.SetClock(ctx, w.Instance.Epoch.Add(StoryClockAdvance)); err != nil {
				return err
			}
			return ErrStoryAlreadySeeded
		}
	}

	// The clock starts at the world's epoch — Seed() already put it there, but
	// a re-run after a scenario left the clock weeks ahead should not tell the
	// story in the wrong month.
	if err := s.SetClock(ctx, w.Instance.Epoch); err != nil {
		return err
	}

	for _, step := range []func() error{
		st.enrolmentConsents,    // 1
		st.registerSources,      // 2
		st.submitFirstBatch,     // 3 (advances one day first)
		st.anayaResponds,        // 4: confirm, confirm-zero, dispute
		st.assistedConfirm,      // 5: Chandra, through her supervisor
		st.autoConfirmSweep,     // 6: Bina, by the clock
		st.paymentsMaterialised, // 6b: every exit's instruction, readable
		st.verifications,        // 7: "who checked me"
		st.submitLiveBatch,      // 11: open windows for a live demo
		st.duplicateHold,        // 8: Devika on Bina's number
		st.openRecovery,         // 9: Chandra's lost phone
		st.overdueAuthorization, // 10
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

type story struct {
	*Stack
	w    *fixtures.World
	ctx  context.Context
	oidc *OIDC

	// Claim ids from batch #1, keyed by what they are in the story.
	confirmed, zero, disputed, binas, chandras string
}

// login authenticates as a party, binding a story-scoped subject if needed.
//
// The provider subject is prefixed "story|" so it can never collide with the
// "runID|" subjects the e2e scenarios bind — one subject reaching two parties
// is a 409 by design, and the story must not spend one of the fixtures' on it.
func (st *story) login(partyID string) (Caller, error) {
	providerSub := "story|" + strings.TrimPrefix(partyID, "did:crest:party:")
	token, err := st.oidc.Token(st.ctx, providerSub)
	if err != nil {
		return Caller{}, fmt.Errorf("mint token for %s: %w", partyID, err)
	}
	binding := map[string]any{
		"provider":      "mock-oidc",
		"providerClass": "generic-oidc",
		"subjectRef":    st.oidc.Subject(providerSub),
	}
	err = st.Parties.As(Caller{Token: token}).Post(st.ctx,
		"/v1/parties/"+partyID+"/identity-bindings", binding, nil)
	if err != nil && partyID != fixtures.SupervisorID {
		// A party already bound to somebody else's subject — the fixtures'
		// eSignet bindings on the workers — is not self-claimable, by design:
		// holding a valid token proves who the caller is, not that they are
		// the party in the URL. The door for that case is enrolment-agent
		// assistance, and the supervisor's act-for-party grant on the project
		// is exactly that, so the story binds the way an enrolment would.
		sup, supErr := st.login(fixtures.SupervisorID)
		if supErr != nil {
			return Caller{}, fmt.Errorf("bind %s: %w; and no supervisor to assist: %w", partyID, err, supErr)
		}
		sup.OnBehalfOf = partyID
		if aErr := st.Parties.As(sup).Post(st.ctx,
			"/v1/parties/"+partyID+"/identity-bindings?contextId="+fixtures.ProjectID,
			binding, nil); aErr != nil {
			return Caller{}, fmt.Errorf("bind %s: self %w; assisted %w", partyID, err, aErr)
		}
	} else if err != nil {
		return Caller{}, fmt.Errorf("bind %s: %w", partyID, err)
	}
	return Caller{Token: token}, nil
}

// assist is the supervisor acting for a worker — the same token, plus the
// header naming who it is for. The permission lives in the fixture world's
// act-for-party grant, not here.
func (st *story) assist(actor, forParty string) (Caller, error) {
	c, err := st.login(actor)
	if err != nil {
		return Caller{}, err
	}
	c.OnBehalfOf = forParty
	return c, nil
}

// eventually waits for something the outbox relay carries across a service
// boundary. Real time, briefly — the relay runs on real time even when the
// domain clock is frozen.
func (st *story) eventually(what string, fn func() error) error {
	deadline := time.Now().Add(20 * time.Second)
	var last error
	for {
		if last = fn(); last == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s never happened: %w", what, last)
		}
		select {
		case <-st.ctx.Done():
			return st.ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// 1. Enrolment consents for all three workers, recorded by the supervisor
// acting on their behalf. Consent is what lets ingestion record evidence about
// them at all (§9), so the story starts where a real programme does.
func (st *story) enrolmentConsents() error {
	for _, worker := range []string{fixtures.WorkerAID, fixtures.WorkerBID, fixtures.WorkerCID} {
		c, err := st.assist(fixtures.SupervisorID, worker)
		if err != nil {
			return err
		}
		path := fmt.Sprintf(
			"/v1/parties/%s/consents?moment=enrolment&captureMethod=voice&purpose=%s&capturedBy=%s&contextId=%s",
			worker, url.QueryEscape("work-history-and-payment"),
			url.QueryEscape(fixtures.SupervisorID), url.QueryEscape(fixtures.ProjectID))
		if err := st.Parties.As(c).PostRaw(st.ctx, path, "audio/ogg",
			[]byte("a recording of the worker agreeing at enrolment"), nil); err != nil {
			return fmt.Errorf("consent for %s: %w", worker, err)
		}
	}
	return nil
}

// 2. The evidence source the batches will arrive from, and a graded source on
// the assessments screen.
func (st *story) registerSources() error {
	org, err := st.login(fixtures.OrgID)
	if err != nil {
		return err
	}
	if err := st.Evidence.As(org).Post(st.ctx, "/v1/sources", map[string]any{
		"adapterRef":    "csv-batch@1",
		"contextId":     fixtures.ProjectID,
		"systemRef":     storySourceRef,
		"expectedEvery": "24h",
		"ownerPartyId":  fixtures.SupervisorID,
	}, nil); err != nil {
		return fmt.Errorf("register source: %w", err)
	}
	// The assessment grades an adapter the story's credentials do NOT come
	// from. Assessing csv-batch@1 would cap the tier of every credential the
	// story issues — a true capability of the system, but a confusing first
	// impression when the demo's point is that the spine works. The sources
	// screen still gets a graded row; the story's credentials stay clean.
	if err := st.Verification.As(org).Post(st.ctx, "/v1/source-assessments", map[string]any{
		"adapterRef":        "legacy-paper-import",
		"maxTier":           1,
		"reason":            "paper registers re-keyed by hand; no capture-time provenance",
		"assessedByPartyId": fixtures.OrgID,
	}, nil); err != nil {
		return fmt.Errorf("assess source: %w", err)
	}
	return nil
}

// storyCSV builds a batch in the same column shape the programme's export uses
// (and the spine scenarios mirror).
func storyCSV(rows ...string) []byte {
	header := "activity,outcome_value,outcome_unit,worker_id_kind,worker_id," +
		"period_start,period_end,geography,household_id,beneficiary_count,source_record_ref"
	return []byte(header + "\n" + strings.Join(rows, "\n") + "\n")
}

func storyRow(kind, id string, count int, day, ref string) string {
	return fmt.Sprintf(
		"bednet-distribution,%d,bednets-distributed,%s,%s,%s,%s,Riverside,%s,%d,%s",
		count, kind, id, day, day, ref, count, ref)
}

func (st *story) submit(csv []byte) (ingest, error) {
	sup, err := st.login(fixtures.SupervisorID)
	if err != nil {
		return ingest{}, err
	}
	path := fmt.Sprintf("/v1/batches?contextId=%s&definitionId=%s&submittedBy=%s"+
		"&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=signed-batch"+
		"&systemRef="+storySourceRef,
		fixtures.ProjectID, fixtures.DefinitionID, fixtures.SupervisorID)
	var out ingest
	if err := st.Evidence.As(sup).PostRaw(st.ctx, path, "text/csv", csv, &out); err != nil {
		return ingest{}, fmt.Errorf("submit batch: %w", err)
	}
	return out, nil
}

type ingest struct {
	Batch struct {
		ID           string `json:"id"`
		RowsAccepted int    `json:"rowsAccepted"`
		RowsUnclear  int    `json:"rowsUnclear"`
	} `json:"batch"`
	ClaimIDs []string `json:"claimIds"`
}

// 3. Batch #1: a real week of work, including the rows the rest of the story
// hangs off — a zero outcome, a count the worker will dispute, a worker with
// no phone, and a row nobody matches.
func (st *story) submitFirstBatch() error {
	// Chandra has no phone; a roster id is how her rows find her. Registered
	// through her supervisor, whose act-for-party grant covers this project.
	sup, err := st.assist(fixtures.SupervisorID, fixtures.WorkerCID)
	if err != nil {
		return err
	}
	if err := st.Parties.As(sup).Post(st.ctx,
		"/v1/parties/"+url.PathEscape(fixtures.WorkerCID)+"/roster-ids",
		map[string]any{"rosterId": "RIV-STORY-0003", "contextId": fixtures.ProjectID}, nil); err != nil {
		return fmt.Errorf("register roster id: %w", err)
	}

	if err := st.Advance(st.ctx, 24*time.Hour); err != nil {
		return err
	}

	phoneA, err := PhoneOf(st.w, fixtures.WorkerAID)
	if err != nil {
		return err
	}
	phoneB, err := PhoneOf(st.w, fixtures.WorkerBID)
	if err != nil {
		return err
	}
	res, err := st.submit(storyCSV(
		storyRow("phone", phoneA, 12, "2026-03-02", "story-HH-A1"),
		storyRow("phone", phoneA, 7, "2026-03-02", "story-HH-A2"),
		storyRow("phone", phoneA, 0, "2026-03-02", "story-HH-A3"),
		storyRow("phone", phoneB, 9, "2026-03-02", "story-HH-B1"),
		storyRow("roster-id", "RIV-STORY-0003", 5, "2026-03-02", "story-HH-C1"),
		storyRow("phone", "+15550109999", 7, "2026-03-02", "story-HH-X1"),
	))
	if err != nil {
		return err
	}
	if res.Batch.RowsAccepted != 5 || res.Batch.RowsUnclear != 1 {
		return fmt.Errorf("batch #1: %d accepted / %d unclear, wanted 5 / 1",
			res.Batch.RowsAccepted, res.Batch.RowsUnclear)
	}

	// The claim list does not say which row each claim came from, so ask the
	// records themselves: the claim names its worker, the unit its outcome.
	for _, claimID := range res.ClaimIDs {
		var claim schema.Claim
		if err := st.Evidence.Get(st.ctx, "/v1/claims/"+claimID, &claim); err != nil {
			return fmt.Errorf("read claim %s: %w", claimID, err)
		}
		var unit schema.Unit
		if err := st.Evidence.Get(st.ctx, "/v1/units/"+claim.UnitID, &unit); err != nil {
			return fmt.Errorf("read unit %s: %w", claim.UnitID, err)
		}
		switch {
		case claim.PartyID == fixtures.WorkerBID:
			st.binas = claimID
		case claim.PartyID == fixtures.WorkerCID:
			st.chandras = claimID
		case unit.Outcome.Value == 12:
			st.confirmed = claimID
		case unit.Outcome.Value == 7:
			st.disputed = claimID
		case unit.Outcome.Value == 0:
			st.zero = claimID
		}
	}
	for name, id := range map[string]string{"confirmed": st.confirmed, "zero": st.zero,
		"disputed": st.disputed, "binas": st.binas, "chandras": st.chandras} {
		if id == "" {
			return fmt.Errorf("batch #1 produced no %s claim", name)
		}
	}
	// Windows open through the outbox; nothing downstream works until they do.
	for _, id := range []string{st.confirmed, st.zero, st.disputed, st.binas, st.chandras} {
		claimID := id
		if err := st.eventually("window for "+claimID, func() error {
			return st.Confirmation.Get(st.ctx, "/v1/windows/"+claimID, nil)
		}); err != nil {
			return err
		}
	}
	return nil
}

// 4. Anaya confirms two claims and disputes one. The dispute releases payment
// like every other T=7 exit — the story exists partly to have that on screen.
func (st *story) anayaResponds() error {
	anaya, err := st.login(fixtures.WorkerAID)
	if err != nil {
		return err
	}
	for _, claimID := range []string{st.confirmed, st.zero} {
		if err := st.Confirmation.As(anaya).Post(st.ctx,
			"/v1/claims/"+claimID+"/confirm", map[string]any{"route": "self"}, nil); err != nil {
			return fmt.Errorf("confirm %s: %w", claimID, err)
		}
	}
	if err := st.Confirmation.As(anaya).Post(st.ctx,
		"/v1/claims/"+st.disputed+"/dispute", map[string]any{
			"reason":          "count too low — I distributed 11",
			"raisedByPartyId": fixtures.WorkerAID,
		}, nil); err != nil {
		return fmt.Errorf("dispute: %w", err)
	}
	return nil
}

// 5. Chandra's claim is confirmed through her supervisor — the assisted exit,
// the one route a worker with no phone has.
func (st *story) assistedConfirm() error {
	c, err := st.assist(fixtures.SupervisorID, fixtures.WorkerCID)
	if err != nil {
		return err
	}
	if err := st.Confirmation.As(c).Post(st.ctx,
		"/v1/claims/"+st.chandras+"/confirm", nil, nil); err != nil {
		return fmt.Errorf("assisted confirm: %w", err)
	}
	return nil
}

// 6. Seven days pass and Bina never answers. The sweep auto-confirms — the
// fourth exit, and with it all four now exist on this stack.
func (st *story) autoConfirmSweep() error {
	if err := st.Advance(st.ctx, 7*24*time.Hour+2*time.Hour); err != nil {
		return err
	}
	var swept struct {
		AutoConfirmed []string `json:"autoConfirmed"`
	}
	if err := st.Confirmation.Post(st.ctx, "/v1/sweep", nil, &swept); err != nil {
		return fmt.Errorf("sweep: %w", err)
	}
	found := false
	for _, id := range swept.AutoConfirmed {
		if id == st.binas {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("the sweep did not auto-confirm Bina's claim (swept %d)", len(swept.AutoConfirmed))
	}
	return nil
}

// 6b. Every T=7 exit releases payment, but the instruction materialises
// through the outbox relay — asynchronously. The seeder's summary claims these
// instructions exist (including the held one the payments screen is for), so
// it must not return before the payments service can actually show them: the
// next thing anyone does to a demo stack is open that screen, or re-run the
// seeder, which resets the clock underneath anything still in flight.
func (st *story) paymentsMaterialised() error {
	for _, claimID := range []string{st.confirmed, st.zero, st.disputed, st.chandras, st.binas} {
		id := claimID
		if err := st.eventually("payment instruction for "+id, func() error {
			return st.Payments.Get(st.ctx, "/v1/instructions/by-claim/"+id, nil)
		}); err != nil {
			return err
		}
	}
	// The zero-outcome instruction is not merely present but HELD, with the
	// reason and owner the demo shows. Asserted rather than assumed: a
	// RELEASED zero would be the silent failure the hold exists to prevent.
	var inst struct {
		State string `json:"state"`
		Held  *struct {
			Code string `json:"code"`
		} `json:"held"`
	}
	if err := st.Payments.Get(st.ctx, "/v1/instructions/by-claim/"+st.zero, &inst); err != nil {
		return fmt.Errorf("read the zero-outcome instruction: %w", err)
	}
	if inst.State != "HELD" || inst.Held == nil || inst.Held.Code != "nothing_to_pay" {
		return fmt.Errorf("the zero-outcome instruction is %s (%+v), want HELD nothing_to_pay", inst.State, inst.Held)
	}
	return nil
}

// 7. Two verifications of Anaya's credential, so her "who checked me" trail
// has rows with different purposes on them.
func (st *story) verifications() error {
	anaya, err := st.login(fixtures.WorkerAID)
	if err != nil {
		return err
	}
	var out struct {
		Credentials []map[string]any `json:"credentials"`
	}
	if err := st.Confirmation.As(anaya).Get(st.ctx,
		"/v1/credentials?partyId="+url.QueryEscape(fixtures.WorkerAID), &out); err != nil {
		return fmt.Errorf("list credentials: %w", err)
	}
	if len(out.Credentials) == 0 {
		return fmt.Errorf("anaya has no credentials to verify")
	}
	for _, purpose := range []string{"pre-employment check", "programme audit"} {
		if err := st.Verification.Post(st.ctx, "/v1/verify", map[string]any{
			"credential":         out.Credentials[0],
			"requestedByPartyId": fixtures.OrgID,
			"purpose":            purpose,
		}, nil); err != nil {
			return fmt.Errorf("verify (%s): %w", purpose, err)
		}
	}
	return nil
}

// Two new enrolments sharing one handset. The registry does not refuse either
// — one phone per household is the normal arrangement — but the number can no
// longer resolve without a person deciding, which is the hold on the queue.
//
// The shared number belongs to the two story people, deliberately not to a
// fixture worker: putting a duplicate on Bina's phone would send every later
// row that joins on it to the unclear queue, and the e2e suite runs against
// this same stack and joins batches on exactly that phone.
func (st *story) duplicateHold() error {
	sup, err := st.login(fixtures.SupervisorID)
	if err != nil {
		return err
	}
	const shared = "+15550100777"
	for _, name := range []string{"Worker Devika", "Worker Deepa"} {
		if err := st.Parties.As(sup).Post(st.ctx, "/v1/enrolments", map[string]any{
			"party": schema.Party{
				Kind:        schema.PartyKindPerson,
				DisplayName: name,
				ContactRoutes: []schema.PartyContactRoutesItem{{
					Kind: schema.PartyContactRoutesItemKindPhone, Value: shared,
				}},
			},
			"enrolledBy": fixtures.SupervisorID,
			"contextId":  fixtures.ProjectID,
			"method":     "supervisor-attested",
		}, nil); err != nil {
			return fmt.Errorf("enrol %s: %w", name, err)
		}
	}

	cust, err := st.login(fixtures.CustodianID)
	if err != nil {
		return err
	}
	code, body, err := st.Parties.As(cust).Status(st.ctx, http.MethodGet,
		fmt.Sprintf("/v1/resolve?kind=contact-route&value=%s&contextId=%s",
			url.QueryEscape(shared), fixtures.ProjectID), nil)
	if err != nil {
		return err
	}
	// 409 is the designed answer: the registry held rather than picked.
	if code != http.StatusConflict {
		return fmt.Errorf("resolving the shared number answered %d, not 409: %s", code, body)
	}
	return nil
}

// 9. An open recovery for Chandra — left OPEN deliberately. Closing it needs
// confirmers from two distinct authorities, and the fixture world has one;
// an open recovery is also what the console screen is for.
func (st *story) openRecovery() error {
	cust, err := st.login(fixtures.CustodianID)
	if err != nil {
		return err
	}
	if err := st.Parties.As(cust).Post(st.ctx, "/v1/recoveries", map[string]any{
		"partyId":         fixtures.WorkerCID,
		"openedByPartyId": fixtures.CustodianID,
		"reason":          "lost phone; new number to bind",
	}, nil); err != nil {
		return fmt.Errorf("open recovery: %w", err)
	}
	return nil
}

// 10. One grant whose review date has already passed by story end, so the
// overdue-review screen is not empty. The shape mirrors the fixture world's
// authorizations; reviewBy is the only addition.
func (st *story) overdueAuthorization() error {
	epoch := st.w.Instance.Epoch
	end := epoch.Add(300 * 24 * time.Hour)
	// Four days after the epoch, expressed as an offset rather than as an
	// absolute date. Written absolute, it read "overdue by four days" only
	// while the world sat at the date the fixture file was written; on a
	// stack seeded today it would read overdue by however many months have
	// passed since, which is a different screen.
	reviewBy := epoch.Add(4 * 24 * time.Hour)
	// A custodian surface: the grant is created by the organisation, signed in.
	org, err := st.login(fixtures.OrgID)
	if err != nil {
		return err
	}
	if err := st.Parties.As(org).Post(st.ctx, "/v1/authorizations", schema.Authorization{
		PartyID: fixtures.SpecifierID,
		Terms:   schema.VersionedRef{ID: fixtures.TermsID, Version: 1},
		Scope: schema.AuthorizationScope{
			Kind:      schema.AuthorizationScopeKindContext,
			ContextID: ptrTo(fixtures.ProjectID),
		},
		Functions:         []string{"field-data-clerk"},
		Period:            schema.Period{Start: epoch, End: &end},
		AuthorityPartyID:  fixtures.OrgID,
		ApprovedByPartyID: fixtures.OrgID,
		ApprovedAt:        epoch,
		ReviewBy:          &reviewBy,
		State:             schema.AuthorizationStateACTIVE,
	}, nil); err != nil {
		return fmt.Errorf("overdue authorization: %w", err)
	}
	return nil
}

// Batch #2 stays unconfirmed: open windows a live demo can act on.
//
// Submitted BEFORE the duplicate hold is raised, and the order is load-bearing:
// once Devika shares Bina's number, a row joining on that phone goes to the
// unclear queue instead of Bina — the registry holds an ambiguous identifier
// rather than guessing. The live windows need the number still unambiguous.
func (st *story) submitLiveBatch() error {
	phoneA, err := PhoneOf(st.w, fixtures.WorkerAID)
	if err != nil {
		return err
	}
	phoneB, err := PhoneOf(st.w, fixtures.WorkerBID)
	if err != nil {
		return err
	}
	res, err := st.submit(storyCSV(
		storyRow("phone", phoneA, 6, "2026-03-08", "story-HH-A4"),
		storyRow("phone", phoneB, 4, "2026-03-08", "story-HH-B2"),
		storyRow("roster-id", "RIV-STORY-0003", 3, "2026-03-08", "story-HH-C2"),
	))
	if err != nil {
		return err
	}
	if res.Batch.RowsAccepted != 3 {
		return fmt.Errorf("batch #2: %d accepted, wanted 3", res.Batch.RowsAccepted)
	}
	// Wait for these windows before returning. The relay stamps a window with
	// the clock at delivery time, and the next thing anyone does to a demo
	// stack is often re-run the seeder — which resets the clock to the epoch.
	// A window still in flight at that moment opens dated a week early.
	for _, claimID := range res.ClaimIDs {
		id := claimID
		if err := st.eventually("live window for "+id, func() error {
			return st.Confirmation.Get(st.ctx, "/v1/windows/"+id, nil)
		}); err != nil {
			return err
		}
	}
	return nil
}

func ptrTo[T any](v T) *T { return &v }
