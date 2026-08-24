// Package schema holds the Go projection of schemas/ and the validator that
// enforces what the projection cannot.
//
// The split matters. types_gen.go gives a value the right *shape*; the schema
// gives it the right *content* — a tier between 1 and 3, a hash that is 64 hex
// characters, a context-scoped authorization that actually names a context. A
// Go struct cannot carry any of that, so anything crossing a service boundary
// is validated here rather than trusted because it compiled.
package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// The validator renders its messages through a printer; passing nil panics.
// English because these strings are for logs and developers, not for workers —
// anything a worker reads is the product's own wording, not a schema error.
var printer = message.NewPrinter(language.English)

// Schema IDs, so callers name a schema rather than spelling a URN.
const (
	IDParty          = "urn:crest:schema:primitives:party:1"
	IDTerms          = "urn:crest:schema:primitives:terms:1"
	IDAuthorization  = "urn:crest:schema:primitives:authorization:1"
	IDContext        = "urn:crest:schema:primitives:context:1"
	IDDefinition     = "urn:crest:schema:primitives:definition:1"
	IDUnit           = "urn:crest:schema:primitives:unit:1"
	IDClaim          = "urn:crest:schema:primitives:claim:1"
	IDCredential     = "urn:crest:schema:primitives:credential:1"
	IDConsent        = "urn:crest:schema:primitives:consent:1"
	IDContest        = "urn:crest:schema:primitives:contest:1"
	IDLinkedRecord   = "urn:crest:schema:primitives:linked-record:1"
	IDEvidenceRecord = "urn:crest:schema:evidence:work-evidence-record:1"
	// Reference data rather than a primitive — §3 files the skill list beside
	// credential shapes and adapters, not beside Party and Definition.
	IDSkill         = "urn:crest:schema:reference:skill:1"
	IDWorkEventCred = "urn:crest:schema:credentials:work-event-credential:1"
	IDPaymentSetup  = "urn:crest:schema:profiles:trusted-payments:payment-setup:1"
	IDPaymentInstr  = "urn:crest:schema:profiles:trusted-payments:payment-instruction:1"
	IDCompensation  = "urn:crest:schema:profiles:trusted-payments:compensation:1"
)

var (
	once     sync.Once
	compiled map[string]*jsonschema.Schema
	compErr  error
)

// ValidationError is what a caller can put in front of a person: which schema,
// and every place the value failed it. Not one failure — a form that reports
// its problems one at a time is a form nobody finishes.
type ValidationError struct {
	SchemaID string
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("does not satisfy %s: %s", e.SchemaID, strings.Join(e.Problems, "; "))
}

// Validate checks a value against a schema by its $id. The value is marshalled
// to JSON first, so what is validated is exactly what will be transmitted —
// validating the Go value directly would check a different thing than the wire
// format that the other service actually parses.
func Validate(schemaID string, v any) error {
	s, err := lookup(schemaID)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal for validation: %w", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("unmarshal for validation: %w", err)
	}
	if err := s.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return &ValidationError{SchemaID: schemaID, Problems: problems(ve)}
		}
		return err
	}
	return nil
}

// ValidateBytes is Validate for a value that is already JSON — the shape an
// adapter receives, before anything has decided what type it is.
func ValidateBytes(schemaID string, raw []byte) error {
	s, err := lookup(schemaID)
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("not JSON: %w", err)
	}
	if err := s.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return &ValidationError{SchemaID: schemaID, Problems: problems(ve)}
		}
		return err
	}
	return nil
}

// IDs lists every compiled schema. Used by the contract tests to assert that
// every schema in schemas/ is reachable, so adding a file is enough.
func IDs() []string {
	out := make([]string, 0, len(Sources))
	for id := range Sources {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func lookup(schemaID string) (*jsonschema.Schema, error) {
	once.Do(compile)
	if compErr != nil {
		return nil, compErr
	}
	s, ok := compiled[schemaID]
	if !ok {
		return nil, fmt.Errorf("no schema %q — known: %s", schemaID, strings.Join(IDs(), ", "))
	}
	return s, nil
}

func compile() {
	c := jsonschema.NewCompiler()
	for id, src := range Sources {
		doc, err := jsonschema.UnmarshalJSON(strings.NewReader(src))
		if err != nil {
			compErr = fmt.Errorf("schema %s: %w", id, err)
			return
		}
		if err := c.AddResource(id, doc); err != nil {
			compErr = fmt.Errorf("schema %s: %w", id, err)
			return
		}
	}
	compiled = make(map[string]*jsonschema.Schema, len(Sources))
	for _, id := range IDs() {
		s, err := c.Compile(id)
		if err != nil {
			compErr = fmt.Errorf("compile %s: %w", id, err)
			return
		}
		compiled[id] = s
	}
}

// problems flattens the validator's causal tree into one line per leaf. The
// tree explains how the validator got there; the leaves say what is wrong.
func problems(ve *jsonschema.ValidationError) []string {
	var out []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := "/" + strings.Join(e.InstanceLocation, "/")
			if loc == "/" {
				loc = "(root)"
			}
			out = append(out, fmt.Sprintf("%s: %s", loc, e.ErrorKind.LocalizedString(printer)))
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	sort.Strings(out)
	return out
}
