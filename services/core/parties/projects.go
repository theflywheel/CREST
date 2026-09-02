// Projects: the J3 "setting up a project" surface, on the Context primitive.
//
// The layering test decided almost every question in this file. A project is a
// Context (§2: "'Project' and 'campaign' are what profiles call it"), so there
// is no new primitive and no new lifecycle — DRAFT/ACTIVE/CLOSED already
// existed, and so did activationGates and the opaque configuration map. What
// this file adds is the small set of things two deployments could NOT
// reasonably disagree about:
//
//   - a named configurator has either agreed or not (design finding F2);
//   - a context does not go live with an unsatisfied gate;
//   - a grant narrower than the terms is checked against those terms;
//   - a link to somebody else's system records who linked it and when.
//
// Everything a deployment could disagree about stays configuration: coverage,
// the names of the composition choices and their values, definition origins,
// payment postures, role and function vocabularies, finance-code formats,
// sector taxonomies. None of them appears as an enum, a column or a constant
// anywhere below. Where this file needs a name for something L2 — a gate, a
// composition choice, a record kind — it takes the caller's string and stores
// it without reading it.
package parties

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// The three acknowledgement events. Verbs of the handover, deliberately not
// the context's own lifecycle: a declined handover leaves a perfectly valid
// DRAFT context, and conflating the two is how a decline would come to look
// like a deletion.
const (
	ownershipNamed    = "NAMED"
	ownershipAccepted = "ACCEPTED"
	ownershipDeclined = "DECLINED"
)

// contextKindProject is the default profile vocabulary for what /v1/projects
// creates. A caller may say otherwise (a cohort, a campaign) and the core will
// not read the word either way; this is a default, not a permitted set.
const contextKindProject = "project"

// Record kinds this surface reads back under names of its own. The strings are
// the console's vocabulary, not the core's rules — context_records.kind is
// opaque, and a deployment that wants a sixth kind needs no migration.
const (
	recordFinanceLink     = "finance-link"
	recordSupportOwner    = "support-owner"
	recordCompositionPfx  = "composition:"
	maxRecordKindLength   = 120
	maxDeclineReasonBytes = 2000
)

var (
	// errOwnershipAlreadyDecided keeps a settled acknowledgement settled. A
	// second answer would overwrite the first with no trace of it.
	errOwnershipAlreadyDecided = errors.New("this handover has already been answered")
	// errNoHandover is an answer to a question nobody asked.
	errNoHandover = errors.New("nobody has been named to configure this context")
	// errNotTheConfigurator refuses an answer from somebody other than the
	// person named. An acknowledgement anybody could give is not one.
	errNotTheConfigurator = errors.New("only the named configurator answers a handover")
	// errDeclineNeedsReason is the same posture as a held payment carrying a
	// reason with an owner: a refusal without one is a dead end.
	errDeclineNeedsReason = errors.New("declining a handover records a reason")
	// errNotDraft refuses activation of something that is not waiting to run.
	errNotDraft = errors.New("only a DRAFT context activates")
	// errGatesUnmet is activation refused with the conditions readable.
	errGatesUnmet = errors.New("activation conditions are not all satisfied")
)

// ownershipEvent is one row of the acknowledgement trail.
type ownershipEvent struct {
	Seq          int       `json:"seq"`
	Event        string    `json:"event"`
	PartyID      string    `json:"partyId"`
	ActorPartyID string    `json:"actorPartyId"`
	Reason       *string   `json:"reason,omitempty"`
	At           time.Time `json:"at"`
}

// nameConfigurator hands a context to a party, or hands it to somebody else
// after a decline.
//
// Naming is a proposal, never an assignment: the result is always PENDING,
// including on a re-name after an ACCEPTED one, because a person who agreed to
// configure a project has not thereby agreed to hand it on. The returned event
// is what the caller appends to the trail, so the previous answer survives the
// overwrite of the current view.
func nameConfigurator(c schema.Context, configuratorPartyID, actorPartyID string,
	at time.Time) (schema.Context, ownershipEvent) {

	c.Ownership = &schema.ContextOwnership{
		ConfiguratorPartyID: configuratorPartyID,
		NamedByPartyID:      actorPartyID,
		NamedAt:             at,
		State:               schema.ContextOwnershipStatePENDING,
	}
	return c, ownershipEvent{
		Event: ownershipNamed, PartyID: configuratorPartyID,
		ActorPartyID: actorPartyID, At: at,
	}
}

// decideOwnership records the named configurator's answer (design finding F2).
//
// Declining is a first-class outcome, not an error path: it writes who
// declined and why, leaves the context and every record keyed to it exactly
// where they were, and leaves the context in the owning organisation's queue
// by way of an ownership state it can filter on. Nothing here deletes
// anything, and there is deliberately no code path from this function that
// could.
func decideOwnership(c schema.Context, accept bool, reason, actorPartyID string,
	at time.Time) (schema.Context, ownershipEvent, error) {

	if c.Ownership == nil {
		return c, ownershipEvent{}, errNoHandover
	}
	if c.Ownership.State != schema.ContextOwnershipStatePENDING {
		return c, ownershipEvent{}, errOwnershipAlreadyDecided
	}
	// The actor is checked against the person named, not against a permission
	// to act generally. An acknowledgement is somebody's own answer; a party
	// permitted to act for them may still submit it on their behalf, which is
	// what identity.Authorize decides before this function is reached.
	if actorPartyID != "" && actorPartyID != c.Ownership.ConfiguratorPartyID {
		return c, ownershipEvent{}, errNotTheConfigurator
	}
	reason = strings.TrimSpace(reason)
	if !accept && reason == "" {
		return c, ownershipEvent{}, errDeclineNeedsReason
	}
	if len(reason) > maxDeclineReasonBytes {
		return c, ownershipEvent{}, fmt.Errorf("a reason of %d bytes is a document, not a reason", len(reason))
	}

	next := c.Ownership
	decided := at
	next.DecidedAt = &decided
	ev := ownershipEvent{PartyID: next.ConfiguratorPartyID, ActorPartyID: next.ConfiguratorPartyID, At: at}
	if accept {
		next.State = schema.ContextOwnershipStateACCEPTED
		next.Reason = nil
		ev.Event = ownershipAccepted
	} else {
		next.State = schema.ContextOwnershipStateDECLINED
		next.Reason = &reason
		ev.Event = ownershipDeclined
		ev.Reason = &reason
	}
	c.Ownership = next
	return c, ev, nil
}

// activationCondition is one readable answer to "what does this project still
// need before it runs?" (p2_7).
type activationCondition struct {
	Name        string     `json:"name"`
	Satisfied   bool       `json:"satisfied"`
	SatisfiedAt *time.Time `json:"satisfiedAt,omitempty"`
	// Because says what the condition is in a sentence, because a gate name a
	// deployment chose is not self-explaining to the person reading the screen.
	Because string `json:"because,omitempty"`
}

// activationConditions lists everything standing between a context and ACTIVE,
// satisfied ones included. A list of only the unmet conditions cannot tell the
// difference between a project that is ready and one whose gates were never
// declared, and those are not the same project.
//
// Two conditions are infrastructure's own. Every other one is a gate name the
// deployment declared, and the core never reads the word.
func activationConditions(c schema.Context) []activationCondition {
	out := make([]activationCondition, 0, len(c.ActivationGates)+1)
	if c.Ownership != nil {
		accepted := c.Ownership.State == schema.ContextOwnershipStateACCEPTED
		out = append(out, activationCondition{
			Name:      "ownership-acknowledged",
			Satisfied: accepted,
			SatisfiedAt: func() *time.Time {
				if accepted {
					return c.Ownership.DecidedAt
				}
				return nil
			}(),
			Because: "the named configurator has accepted the handover; a project " +
				"whose owner never agreed looks staffed and is not",
		})
	}
	for _, g := range c.ActivationGates {
		out = append(out, activationCondition{
			Name: g.Name, Satisfied: g.SatisfiedAt != nil, SatisfiedAt: g.SatisfiedAt,
		})
	}
	return out
}

// activate moves a DRAFT context to ACTIVE, or refuses with the conditions
// readable. Refusing without saying what is unmet is the shape of failure this
// system does not use.
func activate(c schema.Context, at time.Time) (schema.Context, []activationCondition, error) {
	conds := activationConditions(c)
	if c.State == schema.ContextStateACTIVE {
		// Idempotent: an already-live project is the outcome the caller wanted.
		return c, conds, nil
	}
	if c.State != schema.ContextStateDRAFT {
		return c, conds, errNotDraft
	}
	for _, cond := range conds {
		if !cond.Satisfied {
			return c, conds, errGatesUnmet
		}
	}
	c.State = schema.ContextStateACTIVE
	if c.Period.Start.IsZero() {
		c.Period.Start = at
	}
	return c, conds, nil
}

// declareGates replaces a context's gate list, keeping every satisfaction that
// survives the change.
//
// Gate names are L2 vocabulary — "workers registered", "definition ratified",
// whatever this programme's readiness means — so the list is the caller's to
// set. What is not the caller's is the satisfaction timestamp: re-declaring a
// gate must not clear an answer somebody already gave, and must not let a
// caller assert one it never gave.
func declareGates(c schema.Context, names []string, at time.Time) (schema.Context, error) {
	was := map[string]*time.Time{}
	for _, g := range c.ActivationGates {
		was[g.Name] = g.SatisfiedAt
	}
	seen := map[string]bool{}
	next := make([]schema.ContextActivationGatesItem, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			return c, errors.New("a gate with no name is a condition nobody can satisfy")
		}
		if len(name) > maxRecordKindLength {
			return c, fmt.Errorf("gate name %q is too long", name[:32])
		}
		if seen[name] {
			return c, fmt.Errorf("gate %q is declared twice; one condition, one row", name)
		}
		seen[name] = true
		next = append(next, schema.ContextActivationGatesItem{Name: name, SatisfiedAt: was[name]})
	}
	c.ActivationGates = next
	return c, nil
}

// satisfyGate marks one declared gate satisfied, at the service's clock rather
// than at a time the caller chose. An undeclared gate is refused rather than
// created: a gate that appears when it is satisfied is a gate that never
// gated anything.
func satisfyGate(c schema.Context, name string, at time.Time) (schema.Context, error) {
	for i := range c.ActivationGates {
		if c.ActivationGates[i].Name != name {
			continue
		}
		if c.ActivationGates[i].SatisfiedAt == nil {
			when := at
			c.ActivationGates[i].SatisfiedAt = &when
		}
		return c, nil
	}
	return c, fmt.Errorf("%q is not a declared gate on this context", name)
}

// narrowerThanTerms is p2_18's rule: "the grant is narrower than the terms".
//
// It compares the functions a grant would carry against the permissions the
// partner's accepted terms actually contain, and returns the ones the terms do
// not cover. Both sides are opaque strings — the core does not know what
// "register-workers" means, only that a grant cannot exceed what was
// published and agreed.
func narrowerThanTerms(functions, permissions []string) []string {
	allowed := make(map[string]bool, len(permissions))
	for _, p := range permissions {
		allowed[p] = true
	}
	var missing []string
	for _, f := range functions {
		if !allowed[f] {
			missing = append(missing, f)
		}
	}
	sort.Strings(missing)
	return missing
}

// financeLink is the payload of a finance-code record (p2_8).
//
// Every field is somebody else's fact. `System` names the finance system the
// code belongs to and `Code` is that system's own string, stored verbatim:
// CREST does not generate, format, validate or reserve account codes, and the
// screen's own callout is why — a code that reads correctly and was closed
// last year is a rejection weeks after the work was validated, and only the
// finance system can know that.
type financeLink struct {
	System string `json:"system"`
	Code   string `json:"code"`
	Label  string `json:"label,omitempty"`
	// Note carries whatever the linker wants the next person to know, e.g.
	// which fiscal year the code was pulled in.
	Note string `json:"note,omitempty"`
}

func validFinanceLink(l financeLink) error {
	if strings.TrimSpace(l.System) == "" {
		return errors.New("a finance link names the system the code came from; CREST does not mint codes and cannot say where one is from")
	}
	if strings.TrimSpace(l.Code) == "" {
		return errors.New("a finance link with no code links nothing")
	}
	if len(l.Code) > 200 || len(l.System) > 200 || len(l.Label) > 400 || len(l.Note) > 2000 {
		return errors.New("a finance link is a reference, not a document")
	}
	return nil
}

// supportOwner is the payload of a support-ownership record (p2_10).
//
// The reference moved first-line support from the instance to the project, and
// the fact this stores is the one that move needs: a named party a worker's
// question reaches. Nothing here is a support system — it is the owner, so
// that no worker question, and no held payment, arrives somewhere with nobody
// named against it.
type supportOwner struct {
	PartyID string `json:"partyId"`
	// ContactRoute is how the owner is reached for support specifically, when
	// that differs from the party's own routes. Optional; the party's routes
	// are the fallback and are already registry facts.
	ContactRoute *struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"contactRoute,omitempty"`
	// EscalatesToPartyID is where first line hands a genuine platform fault on
	// to. Optional, and its absence is honest: it means escalation has not
	// been arranged, not that it is unnecessary.
	EscalatesToPartyID string `json:"escalatesToPartyId,omitempty"`
	Note               string `json:"note,omitempty"`
}

func validSupportOwner(s supportOwner) error {
	if strings.TrimSpace(s.PartyID) == "" {
		return errors.New("support ownership names a party; an owner nobody can name is the dead end this record exists to prevent")
	}
	if s.ContactRoute != nil &&
		(strings.TrimSpace(s.ContactRoute.Kind) == "" || strings.TrimSpace(s.ContactRoute.Value) == "") {
		return errors.New("a contact route needs both a kind and a value")
	}
	if len(s.Note) > 2000 {
		return errors.New("a support note is a note, not a runbook")
	}
	return nil
}

// validRecordKind bounds the opaque name a configuration record is filed
// under. The core has no vocabulary to check it against — that is the point of
// the layering test here — so it checks only what it can: that the name is a
// name, short enough to index and free of the separator its own composition
// prefix uses.
func validRecordKind(kind string) error {
	kind = strings.TrimSpace(kind)
	switch {
	case kind == "":
		return errors.New("a configuration record needs a name")
	case len(kind) > maxRecordKindLength:
		return fmt.Errorf("record name is longer than %d characters", maxRecordKindLength)
	case strings.ContainsAny(kind, ":/ \t\n"):
		return errors.New("a record name is a single slug: no colons, slashes or spaces")
	}
	return nil
}

// directoryFilter is p2_17's three filters. Every one of them is L2
// vocabulary: a sector taxonomy, a country list and a permission set all come
// from the deployment, and this struct holds whatever strings it was given.
type directoryFilter struct {
	Sector      string
	Country     string
	Permissions []string
}

// directoryEntry is one approved organisation as a project configurator sees
// it. Deliberately not the published organisation face: that face is an
// allowlist over what reaches the public registry log, and nothing here
// changes it. This is an authenticated read of registry facts the applicant
// itself supplied, which is why self-declared attributes may appear here and
// must not appear there.
type directoryEntry struct {
	PartyID      string   `json:"partyId"`
	DisplayName  string   `json:"displayName"`
	Sector       string   `json:"sector,omitempty"`
	Country      string   `json:"country,omitempty"`
	TermsID      string   `json:"termsId"`
	TermsVersion int      `json:"termsVersion"`
	Permissions  []string `json:"permissions"`
	// ApprovedAt is when the registry decided, not when the organisation
	// applied. p2_17's callout turns on this: onboarding happened once,
	// independently of this project, and this project cannot revisit it.
	ApprovedAt *time.Time `json:"approvedAt,omitempty"`
}

// matchesDirectory decides whether one approved organisation answers the
// filters. Attribute comparison is case-insensitive because a sector taxonomy
// is somebody's spreadsheet; permission comparison is exact because a
// permission string is a published identifier and "nearly" is not a match.
func matchesDirectory(e directoryEntry, f directoryFilter) bool {
	if f.Sector != "" && !strings.EqualFold(e.Sector, f.Sector) {
		return false
	}
	if f.Country != "" && !strings.EqualFold(e.Country, f.Country) {
		return false
	}
	// "Filters on what their terms allow, so nobody appears here who could not
	// do the work" — so a requested permission the terms do not carry excludes
	// the organisation rather than being ignored.
	if len(narrowerThanTerms(f.Permissions, e.Permissions)) > 0 {
		return false
	}
	return true
}

// attrString reads one self-declared attribute as a string, tolerating the
// absence rather than inventing a value. An organisation that never stated a
// sector has no sector, and guessing one would put it in a directory listing
// its own registration does not support.
func attrString(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	if s, ok := attrs[key].(string); ok {
		return s
	}
	return ""
}
