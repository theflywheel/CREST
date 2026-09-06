package definitions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/theflywheel/crest/pkg/dedi"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// Publication of a definition to the registry substrate (#21, §3, §7).
//
// A credential names the definition version it was issued against. For that
// name to mean anything to someone outside CREST, the version has to exist
// somewhere they can resolve without asking CREST — and stay resolvable after
// the definition moves on, because a credential issued under v1 must remain
// verifiable once v2 exists.

// registryName is the DeDi registry work definitions live in.
const registryName = "work-definitions"

// topicPublishDefinition is enqueued in the same transaction as the ACTIVE
// transition. Not after it: a publish that is attempted after the commit is a
// publish that a crash loses, and the definition is then ACTIVE and
// unresolvable, which is the state credentials get issued against.
const topicPublishDefinition = "dedi.publish.definition"

type publishMessage struct {
	DefinitionID string `json:"definitionId"`
	Version      int    `json:"version"`
}

// enqueuePublication records that a version needs publishing.
func enqueuePublication(ctx context.Context, tx store.Querier, defID string, version int) error {
	return store.Enqueue(ctx, tx, topicPublishDefinition,
		publishMessage{DefinitionID: defID, Version: version})
}

// publicFace is the projection that goes to the node, and it is a hand-written
// allow-list rather than the definition document.
//
// Marshalling the whole record would publish whatever a future field happens to
// be, and the node is append-only — a field that should not have left the
// country cannot be taken back out of a transparency log. So every field that
// reaches DeDi is named here, and adding one is a visible edit.
//
// The two party ids are deliberate. They are opaque CREST identifiers, not
// personal data, and they carry the one governance fact a verifier has a
// legitimate interest in: that the party who authored this version was not the
// party who ratified it (§7). Separation of duties that only CREST can see is
// separation of duties a verifier has to take on trust.
func publicFace(d schema.Definition) map[string]any {
	out := map[string]any{
		"definitionId": d.ID,
		"version":      d.Version,
		"state":        string(d.State),
		"activity":     d.Activity,
		"outcomeUnit":  d.OutcomeUnit,
		"faces": map[string]any{
			"worker":   d.Faces.Worker,
			"platform": d.Faces.Platform,
			"verifier": d.Faces.Verifier,
		},
		// The tier map, because trust strength is derived and never stored: a
		// verifier who cannot see the evidence-to-tier map cannot check the
		// tier they are shown, they can only be told it. Publishing the map is
		// what makes the derivation checkable by someone who does not trust us.
		"tierMap":           d.TierMap,
		"authoredByPartyId": d.AuthoredByPartyID,
	}
	if d.RatifiedByPartyID != nil {
		out["ratifiedByPartyId"] = *d.RatifiedByPartyID
	}
	if d.ActivatedAt != nil {
		out["activatedAt"] = d.ActivatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// deliver publishes one definition version and records where it landed.
//
// Idempotent, because the relay is at-least-once. A redelivery finds the
// publication row already there and does nothing, rather than appending a
// second identical version to the log — a transparency log with duplicate
// versions of the same fact is one where "which version did the credential
// pin?" stops having one answer.
func deliver(d service.Deps) store.Deliverer {
	return func(ctx context.Context, topic string, payload json.RawMessage) error {
		if topic != topicPublishDefinition {
			return fmt.Errorf("no delivery route for topic %q", topic)
		}
		var msg publishMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			return err
		}
		if d.DeDi == nil {
			return errors.New("a definition needs publishing and this service has no registry substrate")
		}

		already, err := publicationOf(ctx, d.DB.Q(), msg.DefinitionID, msg.Version)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil {
			d.Log.Info("definition version already published",
				"definition", msg.DefinitionID, "version", msg.Version, "registryVersion", already.RegistryVersion)
			return nil
		}

		def, err := getDefinition(ctx, d.DB.Q(), msg.DefinitionID, msg.Version)
		if err != nil {
			return fmt.Errorf("read definition %s v%d: %w", msg.DefinitionID, msg.Version, err)
		}
		if def.State != schema.DefinitionStateACTIVE {
			// Not an error worth retrying forever, but not something to drop
			// either: a message asking to publish a non-ACTIVE version means
			// something enqueued from the wrong place.
			return fmt.Errorf("definition %s v%d is %s, not ACTIVE; only an ACTIVE version is published",
				msg.DefinitionID, msg.Version, def.State)
		}
		if err := validateDefinitionSchemaRef(def.Faces.Platform.SchemaRef); err != nil {
			return fmt.Errorf("definition %s v%d has invalid evidence schema: %w",
				msg.DefinitionID, msg.Version, err)
		}

		ref := dedi.Ref{Namespace: d.DeDiNamespace, Registry: registryName, Record: def.ID}

		// The precondition is decided from what the node says, not from what
		// this service remembers. Create for the first version of a record;
		// otherwise replace the exact version currently there. That is what
		// makes two concurrent activations of different versions a conflict
		// rather than a silent overwrite.
		pre := dedi.Create()
		current, err := d.DeDi.Resolve(ctx, ref, false)
		switch {
		case err == nil:
			pre = dedi.Replace(current.Receipt.Tag())
		case errors.Is(err, dedi.ErrNotFound):
			// first version of this record
		default:
			return fmt.Errorf("read current registry state for %s: %w", ref, err)
		}

		receipt, err := d.DeDi.Publish(ctx, ref, publicFace(def), pre)
		if err != nil {
			return fmt.Errorf("publish %s v%d: %w", def.ID, def.Version, err)
		}
		if !receipt.Transparent {
			// Logged at every publication, not once at boot. Someone reading a
			// definition's history months later needs to see which of them a
			// verifier can actually check.
			d.Log.Warn("definition published without a transparency proof",
				"definition", def.ID, "version", def.Version,
				"consequence", "a verifier can resolve this version only by trusting this deployment")
		}
		return recordPublication(ctx, d.DB, def, receipt, d.Clock.Now())
	}
}
