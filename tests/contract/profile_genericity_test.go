package contract

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/theflywheel/crest/pkg/schema"
)

// #13's done-when, mechanically: "nothing in the primitive schemas needed a
// payments-specific field to make this work."
//
// The Trusted Payments profile (schemas/profiles/trusted-payments) names roles
// and three LinkedRecord types and carries its own payload schemas. The claim
// §2.1 makes is that the eleven primitives stayed generic while a whole
// payments application was expressed over them — and the only honest form of
// that claim is a test that fails the day a primitive grows a payments word.
//
// Descriptions are deliberately not scanned: prose may say "releases payment"
// to explain a state; a *property* named for payment is the leak.
var paymentsWords = regexp.MustCompile(`(?i)(payment|payer|payee|payout|disburse|compensat|tariff|wage|salary|invoice|currency|amountminor|ratePer|rail)`)

func TestNoPrimitiveSchemaCarriesAPaymentsField(t *testing.T) {
	primitives := 0
	for schemaID, raw := range schema.Sources {
		if !strings.Contains(schemaID, ":primitives:") {
			continue
		}
		primitives++
		var doc any
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatalf("%s: %v", schemaID, err)
		}
		walkProperties(doc, nil, func(path []string, name string) {
			if paymentsWords.MatchString(name) {
				t.Errorf("%s: property %q (at %s) is payments vocabulary in a primitive.\n"+
					"§2.1: payments is a profile over the primitives. The field belongs in a "+
					"LinkedRecord payload under schemas/profiles/trusted-payments, keyed to the "+
					"primitive by id — or this is a design finding (#13), to be raised, not patched.",
					schemaID, name, strings.Join(path, "."))
			}
		})
		walkEnums(doc, nil, func(path []string, value string) {
			if paymentsWords.MatchString(value) {
				t.Errorf("%s: enum value %q (at %s) is payments vocabulary in a primitive; "+
					"the core must not know the profile's states or roles (§2.1, #13).",
					schemaID, value, strings.Join(path, "."))
			}
		})
	}
	if primitives < 11 {
		t.Fatalf("expected the eleven primitives under schema.Sources, found %d", primitives)
	}
}

// The profile is complete on its own side: every LinkedRecord type it names
// has a payload schema of its own, and the primitive it extends leaves the
// type open (a string, not an enum) so a second profile needs nothing new.
func TestTheProfileNamesOnlyTypesItDefines(t *testing.T) {
	profile, ok := schema.Sources["urn:crest:schema:profiles:trusted-payments:1"]
	if !ok {
		t.Fatal("the Trusted Payments profile document is not registered under schema.Sources")
	}
	var doc struct {
		Properties struct {
			LinkedRecordTypes struct {
				Const []string `json:"const"`
			} `json:"linkedRecordTypes"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(profile), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Properties.LinkedRecordTypes.Const) == 0 {
		t.Fatal("the profile names no LinkedRecord types")
	}
	for _, typ := range doc.Properties.LinkedRecordTypes.Const {
		id := "urn:crest:schema:profiles:trusted-payments:" + typ + ":1"
		if _, ok := schema.Sources[id]; !ok {
			t.Errorf("profile names LinkedRecord type %q but no payload schema %s exists", typ, id)
		}
	}

	var linked map[string]any
	if err := json.Unmarshal([]byte(schema.Sources["urn:crest:schema:primitives:linked-record:1"]), &linked); err != nil {
		t.Fatal(err)
	}
	typeProp := linked["properties"].(map[string]any)["type"].(map[string]any)
	if _, closed := typeProp["enum"]; closed {
		t.Error("LinkedRecord.type is an enum: the core is naming a profile's types, and a second profile would need a core change")
	}
}

// walkEnums visits every string in an "enum" array anywhere in a JSON Schema
// document, with the path that led there.
func walkEnums(node any, path []string, visit func(path []string, value string)) {
	switch n := node.(type) {
	case map[string]any:
		if values, ok := n["enum"].([]any); ok {
			for _, v := range values {
				if s, ok := v.(string); ok {
					visit(path, s)
				}
			}
		}
		for key, child := range n {
			if key == "description" {
				continue
			}
			walkEnums(child, append(append([]string{}, path...), key), visit)
		}
	case []any:
		for i, child := range n {
			walkEnums(child, append(append([]string{}, path...), strings.Repeat("[]", 1)+string(rune('0'+i%10))), visit)
		}
	}
}
