package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/notify"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

func monitorSources(ctx context.Context, d service.Deps) error {
	now := d.Clock.Now()
	sources, err := listSources(ctx, d.DB.Q(), now, "")
	if err != nil {
		return err
	}
	for _, s := range sources {
		if !s.overdue(now) {
			continue
		}
		if err := d.DB.InTx(ctx, func(tx store.Querier) error {
			opened, err := openSilence(ctx, tx, s.ID, now)
			if err != nil || !opened {
				return err
			}
			return store.Enqueue(ctx, tx, topicSourceQuiet, map[string]any{"partyId": s.OwnerPartyID, "subject": s.SystemRef, "sourceId": s.ID})
		}); err != nil {
			return err
		}
	}
	return nil
}

func monitorLoop(ctx context.Context, d service.Deps, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := monitorSources(ctx, d); err != nil {
				d.Log.Error("source monitoring failed", "error", err)
			}
		}
	}
}

func notifyQuietSource(ctx context.Context, d service.Deps, parties *client.Client, sender notify.Sender, payload json.RawMessage) error {
	if sender == nil {
		return fmt.Errorf("source alert delivery has no notification provider")
	}
	var event struct {
		PartyID  string `json:"partyId"`
		Subject  string `json:"subject"`
		SourceID string `json:"sourceId"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	var party schema.Party
	if err := parties.Get(ctx, "/internal/parties/"+url.PathEscape(event.PartyID), &party); err != nil {
		return err
	}
	for _, contact := range party.ContactRoutes {
		if string(contact.Kind) != "email" {
			continue
		}
		result, err := sender.Send(ctx, notify.Message{To: contact.Value, Subject: "CREST evidence source needs attention", Body: "Expected evidence has not arrived from " + event.Subject + ". Inspect the source connection and resolve its outage."})
		if err != nil {
			return err
		}
		if !result.Accepted {
			return fmt.Errorf("notification provider refused source alert")
		}
		_, err = d.DB.Q().Exec(ctx, "UPDATE sources SET notified_at=$2 WHERE id=$1", event.SourceID, d.Clock.Now())
		return err
	}
	return fmt.Errorf("source owner has no supported contact route")
}
