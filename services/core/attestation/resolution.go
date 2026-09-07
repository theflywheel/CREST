package attestation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

const topicContestResolution = "contest.resolve"

type resolutionEvent struct {
	Decision       ContestDecision `json:"decision"`
	Window         Window          `json:"window"`
	ReplacementRef string          `json:"replacementRef,omitempty"`
}

func applyResolution(ctx context.Context, d service.Deps, evidence, verification *client.Client, payload json.RawMessage) error {
	var event resolutionEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	win, decision := event.Window, event.Decision
	if decision.Decision == "REJECTED" {
		if err := evidence.Post(ctx, "/internal/claims/"+url.PathEscape(win.ClaimID)+"/transition", map[string]any{"to": schema.ClaimStateACCEPTED, "route": "review"}, nil); err != nil {
			return err
		}
		var issued issuedCredential
		if err := verification.Post(ctx, "/internal/credentials/issue", issueRequest{ClaimID: win.ClaimID, UnitID: win.UnitID, PartyID: win.PartyID, ContextID: win.ContextID, Route: "review", At: decision.DecidedAt}, &issued); err != nil {
			return err
		}
		win.CredentialID = &issued.ID
	} else {
		if win.CredentialID != nil {
			if err := verification.Post(ctx, "/internal/credentials/"+url.PathEscape(*win.CredentialID)+"/revoke", nil, nil); err != nil {
				return err
			}
		}
	}
	state := "UPHELD"
	if decision.Decision == "REJECTED" {
		state = "REJECTED"
	}
	return d.DB.InTx(ctx, func(tx store.Querier) error {
		_, err := tx.Exec(ctx, `UPDATE contests SET state=$2,
   doc=jsonb_set(jsonb_set(jsonb_set(doc,'{state}',to_jsonb($2::text)),'{resolvedAt}',to_jsonb($3::timestamptz)),'{resolution}',to_jsonb($4::text))
   WHERE id=$1`, decision.ContestID, state, decision.DecidedAt, decision.Reason)
		if err != nil {
			return err
		}
		if decision.Decision == "REJECTED" {
			_, err = tx.Exec(ctx, "UPDATE windows SET credential_id=$2 WHERE claim_id=$1", win.ClaimID, win.CredentialID)
		}
		return err
	})
}

func validateReplacement(ctx context.Context, verification, evidence *client.Client, win Window, ref string) error {
	if ref == "" {
		return fmt.Errorf("a correction must reference an already issued replacement credential")
	}
	var replacement issuedCredential
	if err := verification.Get(ctx, "/internal/credentials/"+url.PathEscape(ref), &replacement); err != nil {
		return fmt.Errorf("read replacement: %w", err)
	}
	if replacement.RevokedAt != nil || replacement.SubjectRef != win.PartyID || replacement.ClaimID == win.ClaimID {
		return fmt.Errorf("replacement must be a standing credential for a separate corrected claim of this worker")
	}
	var claim schema.Claim
	if err := evidence.Get(ctx, "/internal/claims/"+url.PathEscape(replacement.ClaimID), &claim); err != nil {
		return err
	}
	var unit schema.Unit
	if err := evidence.Get(ctx, "/internal/units/"+url.PathEscape(claim.UnitID), &unit); err != nil {
		return err
	}
	if unit.ContextID != win.ContextID || claim.PartyID != win.PartyID || claim.State != schema.ClaimStateACCEPTED {
		return fmt.Errorf("replacement must be accepted work for the same worker and project")
	}

	return nil
}
