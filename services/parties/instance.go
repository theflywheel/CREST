package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// The deployment's public self-description (#70, Blueprint §3).
//
// Not a twelfth primitive. §2's eleven describe the work — who did what, under
// which definition, confirmed how. This describes the deployment itself, and
// putting it in the object model would say that a CREST instance is a kind of
// thing CREST records, which it is not.
//
// It is also not stored. Every field is configuration or derived from it, and a
// stored copy is a second source of truth that drifts from the environment the
// service is actually running with — the same reason the tier and a Party's
// assurance are derived rather than written down. What *is* stored is where the
// description was published, because that is a fact about the past.
//
// The question it answers is the one #69 left open. A verifier who resolves
// `crest/organisations/<id>` on the node has a record and no way to know which
// deployment owns that namespace, which publisher key its writes should carry,
// or who to ask when the record and a credential disagree.

const registryInstances = "instances"

// Instance is what a deployment says about itself.
type Instance struct {
	ID   string `json:"instanceId"`
	Name string `json:"name"`

	// OperatorPartyID is the organisation answerable for this deployment. It is
	// a Party id rather than a name so a verifier can resolve it in the
	// organisations registry and see whether it was ever approved — "who
	// operates this" is worth little if it is only a string this deployment
	// chose for itself.
	OperatorPartyID string `json:"operatorPartyId"`

	// IssuerID is the DID this deployment's credentials are issued under, so a
	// verifier holding a credential can tie it to the deployment whose registry
	// they are reading.
	IssuerID string `json:"issuerId"`

	Registry InstanceRegistry `json:"registry"`
}

// InstanceRegistry is where this deployment publishes and how its writes are
// signed.
type InstanceRegistry struct {
	// URL is empty when the deployment runs on the Postgres fallback.
	URL       string `json:"url,omitempty"`
	Namespace string `json:"namespace,omitempty"`

	// PublisherKeyID names the key a reader should expect on this namespace's
	// records — the `created_by` a DeDi leaf carries.
	//
	// Self-asserted, and worth saying so: anyone holding a valid publisher key
	// for the namespace could publish a different answer. What stops that being
	// silent is the log itself — the record is append-only, so a change to it
	// is visible to anyone who looked before. This is a fact you can watch, not
	// one you can take on faith.
	PublisherKeyID string `json:"publisherKeyId,omitempty"`

	// Transparent reports whether that registry is a transparency log at all.
	Transparent bool `json:"transparent"`
}

// ErrNoInstance is a deployment that has not been told who it is.
var ErrNoInstance = errors.New("this deployment has no instance identity configured")

// loadInstance reads the deployment's identity from the environment.
//
// The three required values have no defaults outside local development. A
// deployment that invented its own identifier would publish under a name
// nobody agreed to, and two deployments that both defaulted would collide in
// the same namespace — which on an append-only log is not a mistake anyone can
// take back.
func loadInstance(cfg config.Base) (Instance, error) {
	inst := Instance{
		ID:              config.Str("CREST_INSTANCE_ID", ""),
		Name:            config.Str("CREST_INSTANCE_NAME", ""),
		OperatorPartyID: config.Str("CREST_OPERATOR_PARTY_ID", ""),
		IssuerID:        config.Str("ISSUER_ID", ""),
		Registry: InstanceRegistry{
			URL:            config.Str("DEDI_URL", ""),
			Namespace:      config.Str("DEDI_NAMESPACE", "crest"),
			PublisherKeyID: config.Str("DEDI_KEY_ID", ""),
		},
	}
	if cfg.Env == "local" {
		if inst.ID == "" {
			inst.ID = "crest:instance:local"
		}
		if inst.Name == "" {
			inst.Name = "Local development deployment"
		}
	}
	var missing []string
	for name, v := range map[string]string{
		"CREST_INSTANCE_ID":       inst.ID,
		"CREST_INSTANCE_NAME":     inst.Name,
		"CREST_OPERATOR_PARTY_ID": inst.OperatorPartyID,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return Instance{}, fmt.Errorf("%w: set %s", ErrNoInstance, strings.Join(sorted(missing), ", "))
	}
	return inst, nil
}

// publishInstance is the service's OnStart. It publishes the self-description,
// and republishes it when it has changed.
//
// Idempotent by content, not by existence. Bootstrap runs on every start, so
// "publish if absent" would freeze the first answer forever and a deployment
// that moved its operator or rotated its publisher key would keep advertising
// the old one. Republishing only on a change is what keeps the log a history
// rather than a stream of identical entries.
func publishInstance(ctx context.Context, d service.Deps) error {
	inst, err := loadInstance(d.Config)
	if err != nil {
		return err
	}
	if d.DeDi == nil {
		// Nothing to publish to. Not an error: a deployment can legitimately
		// run without a registry substrate, and it says so through the same
		// endpoint.
		d.Log.Warn("instance identity is not published",
			"why", "this service has no registry substrate",
			"consequence", "a verifier cannot resolve which deployment owns the records they are reading")
		return nil
	}
	inst.Registry.Transparent = d.DeDi.Transparent()

	// Enqueued rather than published inline, so start-up is not blocked on a
	// node being reachable and a failed publish is retried rather than lost.
	// The same reason the rest of §3's publication goes through the outbox.
	return d.DB.InTx(ctx, func(tx store.Querier) error {
		return enqueueFact(ctx, tx, "instance", inst.ID, 1)
	})
}

// instanceFace is the projection published to the node.
//
// The whole record, because all of it is public by construction — there is no
// personal data in a deployment's description of itself, and a verifier needs
// every field to make use of it.
func instanceFace(inst Instance) map[string]any {
	return map[string]any{
		"instanceId":      inst.ID,
		"name":            inst.Name,
		"operatorPartyId": inst.OperatorPartyID,
		"issuerId":        inst.IssuerID,
		"registry": map[string]any{
			"namespace":      inst.Registry.Namespace,
			"publisherKeyId": inst.Registry.PublisherKeyID,
			"transparent":    inst.Registry.Transparent,
		},
	}
}

// getInstance answers the deployment's self-description over HTTP.
//
// Unauthenticated, deliberately. It is the deployment's public self-description
// and one nobody outside can read is one nobody outside can check — which is
// the entire reason it exists.
func (h *handlers) getInstance(w http.ResponseWriter, r *http.Request) {
	inst, err := loadInstance(h.d.Config)
	if err != nil {
		// 503 rather than 500: the service is running and correctly configured
		// in every other respect, and this is a deployment that has not been
		// told who it is. The message names the variables.
		httpx.WriteError(w, http.StatusServiceUnavailable, "no_instance_identity", "%s", err)
		return
	}
	if h.d.DeDi != nil {
		inst.Registry.Transparent = h.d.DeDi.Transparent()
	} else {
		inst.Registry.URL, inst.Registry.Namespace, inst.Registry.PublisherKeyID = "", "", ""
	}

	out := map[string]any{"instance": inst}
	// Where the description itself was published, so a reader can check that
	// the answer they just got over HTTP is the answer on the log.
	if pub, err := publicationOf(r.Context(), h.d.DB.Q(), "instance", inst.ID, 1); err == nil {
		out["publication"] = pub
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
