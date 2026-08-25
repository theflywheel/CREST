package contract

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/theflywheel/crest/pkg/schema"
)

// §16 (J4, J5): adapter and rail provider secrets are held by the deployment,
// never by CREST.
//
// The ruling: provider credentials — an adapter's API key, a rail's client
// secret — come from the deployment's environment and are referenced by name.
// L1 may store a reference and a fingerprint; it never stores material. The
// reasoning is §14's, applied sideways: a copy of a signing key is a signing
// key, and a registry that stores provider secrets is a credential store with
// a payments system attached, which is a much larger thing to defend than a
// registry.
//
// The rejected alternative was sealing secrets under a deployment key so rail
// onboarding is self-service. Rejected because it changes what a breach of the
// registry means: today it exposes work records, which is bad; then it would
// expose the keys that move money, which is a different category of bad.
//
// This test is the shape that ruling takes in L1: no primitive may have a
// property whose name says it holds secret material. If it fails, the fix is
// not a rename — it is moving the material to the environment and storing the
// reference (§14, the guard-secrets hook, and this test are all the same rule).
var secretWords = regexp.MustCompile(`(?i)^.*(secret|private[_-]?key|api[_-]?key|password|passphrase|access[_-]?token|client[_-]?secret).*$`)

// referenceWords are the shapes that are allowed to match above: a *name* for
// a secret, not the secret. "signingKeyRef" points at material the environment
// holds; "signingKey" would be the material.
var referenceWords = regexp.MustCompile(`(?i)(ref|reference|name|id|fingerprint|hash)$`)

func TestNoPrimitiveHoldsProviderSecretMaterial(t *testing.T) {
	for schemaID, raw := range schema.Sources {
		var doc any
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatalf("%s: %v", schemaID, err)
		}
		walkProperties(doc, nil, func(path []string, name string) {
			if !secretWords.MatchString(name) || referenceWords.MatchString(name) {
				return
			}
			t.Errorf("%s: property %q (at %s) is named like secret material.\n"+
				"§16 rules that provider secrets live in the deployment's environment, never in a "+
				"CREST store. Store a reference (…Ref) or a fingerprint, and read the material "+
				"from the environment at the point of use.",
				schemaID, name, strings.Join(path, "."))
		})
	}
}

// walkProperties visits every property name declared anywhere in a JSON
// Schema document, with the path that led there.
func walkProperties(node any, path []string, visit func(path []string, name string)) {
	m, ok := node.(map[string]any)
	if !ok {
		if arr, ok := node.([]any); ok {
			for _, item := range arr {
				walkProperties(item, path, visit)
			}
		}
		return
	}
	for key, value := range m {
		if key == "properties" {
			if props, ok := value.(map[string]any); ok {
				for name, sub := range props {
					visit(path, name)
					walkProperties(sub, append(path, name), visit)
				}
				continue
			}
		}
		walkProperties(value, append(path, key), visit)
	}
}
