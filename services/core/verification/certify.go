package verification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
)

// The Certify data surface (#155 phase C): what Inji Certify's data-provider
// plugin reads at issuance time, replacing the CSV fixture that finding C16
// documented as deployment-bound seeding.
//
// The caller hands over the raw provider subject from the wallet's access
// token, and the pairwise derivation happens HERE, with this deployment's
// salt — the salt never leaves CREST, and Certify never learns how a subject
// becomes a party. That both the door's relying-party client and the wallet's
// share one eSignet relying party ("crest") is what makes the token's `sub`
// resolvable at all: eSignet partitions pairwise subjects by relying party,
// not by client (finding E6, spike #3).
//
// The field vocabulary is the one the WorkEventCredential template in
// Certify's credential_config references — the shape #1 froze and #16 kept —
// so swapping the plugin's source from a CSV to this endpoint changes where
// facts come from and nothing about what a credential says.

// certifyWorkEvent is one row of what the plugin may learn: the facts of a
// confirmed, credentialed claim. No name, no identifier, no tier.
type certifyWorkEvent struct {
	UnitID            string  `json:"unitId"`
	ClaimID           string  `json:"claimId"`
	Activity          string  `json:"activity"`
	DefinitionRef     string  `json:"definitionRef"`
	DefinitionVersion string  `json:"definitionVersion"`
	PeriodStart       string  `json:"periodStart"`
	PeriodEnd         string  `json:"periodEnd"`
	OutcomeValue      float64 `json:"outcomeValue"`
	OutcomeUnit       string  `json:"outcomeUnit"`
	ContextRef        string  `json:"contextRef"`
	IssuerOrg         string  `json:"issuerOrg"`
	SourceClass       string  `json:"sourceClass"`
	CaptureMethod     string  `json:"captureMethod"`
	AdapterRef        string  `json:"adapterRef"`
	ReceivedAt        string  `json:"receivedAt"`
	SourceExposure    string  `json:"sourceExposure"`
}

// certifyWorkEventCap bounds how many credentials one request re-reads. The
// plugin issues over the newest fact; the rest are context for a future
// selection flow, not something worth an unbounded fan-out to evidence.
const certifyWorkEventCap = 20

// certifyWorkEvents answers GET /internal/certify/work-events?issuer=&subject=.
//
// Internal, like the issuance route above it: it is reachable only over the
// service network (the public door refuses /internal/* at the proxy), because
// what it serves is the full fact set of somebody's confirmed work, keyed by
// a subject rather than by a proven caller.
//
// An authenticated stranger — a subject with no binding — gets an empty list
// rather than an error: "you have no confirmed work here" is the true answer,
// and the plugin turns it into Certify's own "no data" refusal.
func (h *handlers) certifyWorkEvents(w http.ResponseWriter, r *http.Request) {
	issuer := r.URL.Query().Get("issuer")
	subject := r.URL.Query().Get("subject")
	if issuer == "" || subject == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"issuer and subject are both required: a bare subject is meaningless without who issued it")
		return
	}
	salt := []byte(config.Str("CREST_SUBJECT_SALT", ""))
	if len(salt) == 0 {
		httpx.WriteError(w, http.StatusServiceUnavailable, "not_configured",
			"CREST_SUBJECT_SALT is not set; this deployment cannot derive a pairwise reference")
		return
	}

	subjectRef := identity.Pairwise(salt, issuer, subject)
	partyID, err := h.partyBySubject(r.Context(), subjectRef)
	if err != nil {
		httpx.Fail(w, h.d.Log, "resolve subject binding", err)
		return
	}
	events := []certifyWorkEvent{}
	if partyID != "" {
		ids, err := h.d.SameParty(r.Context(), partyID)
		if err != nil {
			httpx.Fail(w, h.d.Log, "expand merged party", err)
			return
		}
		if events, err = h.collectWorkEvents(r.Context(), ids); err != nil {
			httpx.Fail(w, h.d.Log, "collect work events", err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"workEvents": events, "count": len(events),
	})
}

// partyBySubject exchanges a pairwise reference for the party bound to it.
// Empty (not an error) when nobody is: that is the unenrolled-stranger case.
func (h *handlers) partyBySubject(ctx context.Context, subjectRef string) (string, error) {
	var out struct {
		PartyID string `json:"partyId"`
	}
	err := h.registry.Get(ctx, identity.PathSubjectBinding+url.PathEscape(subjectRef), &out)
	if client.Code(err) == http.StatusNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return out.PartyID, nil
}

// collectWorkEvents reads the party's issued, unrevoked credentials — the
// record of confirmed claims, in the substrate's own store — and re-expresses
// each as the template's fact set. Revoked credentials are excluded: a
// withdrawn record must not be re-issuable into a wallet through the side
// door.
func (h *handlers) collectWorkEvents(ctx context.Context, partyIDs []string) ([]certifyWorkEvent, error) {
	rows, err := h.d.DB.Q().Query(ctx, `
		SELECT claim_id, doc, issued_at FROM credentials
		 WHERE subject_ref = ANY($1) AND revoked_at IS NULL
		 ORDER BY issued_at DESC, id DESC
		 LIMIT `+strconv.Itoa(certifyWorkEventCap), partyIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		claimID  string
		doc      []byte
		issuedAt time.Time
	}
	var creds []row
	for rows.Next() {
		var c row
		if err := rows.Scan(&c.claimID, &c.doc, &c.issuedAt); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	orgNames := map[string]string{}
	activities := map[string]string{}
	events := make([]certifyWorkEvent, 0, len(creds))
	for _, c := range creds {
		var doc schema.WorkEventCredential
		if err := json.Unmarshal(c.doc, &doc); err != nil {
			// A stored credential that does not parse is a serious fact about
			// the store, but it must not hide the ones that do.
			h.d.Log.Error("stored credential does not parse; skipped for certify", "claim", c.claimID, "error", err)
			continue
		}
		we := doc.CredentialSubject.WorkEvent
		prov := doc.CredentialSubject.Provenance
		ev := certifyWorkEvent{
			UnitID:  we.EventID,
			ClaimID: we.ClaimID,
			// The credential's own `activity` is the definition reference
			// (§2: everything by versioned identifier); the template renders
			// a human-readable activity, the way the definition names it.
			Activity:          h.activityCode(ctx, activities, we.Definition),
			DefinitionRef:     we.Definition.ID,
			DefinitionVersion: strconv.Itoa(we.Definition.Version),
			PeriodStart:       we.Period.Start.UTC().Format(time.RFC3339),
			OutcomeValue:      we.Outcome.Value,
			OutcomeUnit:       we.Outcome.Unit,
			SourceClass:       string(prov.SourceClass),
			CaptureMethod:     string(prov.CaptureMethod),
			AdapterRef:        prov.AdapterRef,
			ReceivedAt:        prov.ReceivedAt.UTC().Format(time.RFC3339),
			SourceExposure:    string(prov.SourceExposure),
		}
		if we.Period.End != nil {
			ev.PeriodEnd = we.Period.End.UTC().Format(time.RFC3339)
		}
		// The context is on the unit, not the credential — read it back from
		// evidence, best-effort like every resolution on the issuance path: a
		// credential with an empty contextRef beats no credential.
		var unit schema.Unit
		if err := h.evidence.Get(ctx, "/internal/units/"+url.PathEscape(we.EventID), &unit); err == nil {
			ev.ContextRef = unit.ContextID
		} else {
			h.d.Log.Warn("could not read unit for certify contextRef", "unit", we.EventID, "error", err)
		}
		if a := doc.CredentialSubject.IssuerAuthority; a != nil {
			ev.IssuerOrg = h.orgName(ctx, orgNames, a.OrgID)
		}
		events = append(events, ev)
	}
	return events, nil
}

// activityCode resolves what the definition calls this work, in this
// deployment's own words — `activity.code` on the definition, deliberately
// not the portable skill code. Best-effort with the reference as fallback: a
// credential naming the definition beats no credential.
func (h *handlers) activityCode(ctx context.Context, cache map[string]string, ref schema.VersionedRef) string {
	key := ref.ID + "@" + strconv.Itoa(ref.Version)
	if code, ok := cache[key]; ok {
		return code
	}
	var def schema.Definition
	if err := h.definitions.Get(ctx, "/v1/definitions/"+url.PathEscape(ref.ID)+
		"?version="+strconv.Itoa(ref.Version), &def); err != nil || def.Activity.Code == "" {
		h.d.Log.Warn("could not read the definition's activity for certify", "definition", ref.ID, "error", err)
		cache[key] = ref.ID
		return ref.ID
	}
	cache[key] = def.Activity.Code
	return def.Activity.Code
}

// orgName resolves an organisation party's display name, once per request.
// Best-effort: the org's id is already on the credential; the name is display.
func (h *handlers) orgName(ctx context.Context, cache map[string]string, orgID string) string {
	if name, ok := cache[orgID]; ok {
		return name
	}
	var party struct {
		Name string `json:"displayName"`
	}
	// The service twin, not the caller-facing route: this is service traffic
	// on the issuance path, and /v1/parties/{id} answers signed-in callers.
	if err := h.registry.Get(ctx, "/internal/parties/"+url.PathEscape(orgID), &party); err != nil {
		h.d.Log.Warn("could not read issuing organisation's name", "party", orgID, "error", err)
		cache[orgID] = ""
		return ""
	}
	cache[orgID] = party.Name
	return party.Name
}
