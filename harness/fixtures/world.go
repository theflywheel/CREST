// Package fixtures loads the canonical fixture world.
//
// One world, loaded the same way by the harness, the invariant suite and the
// contract tests. Every object is validated against schemas/ on the way in, so
// a world that has drifted from a schema fails at load with the field named,
// rather than three assertions later with a nil pointer.
package fixtures

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/theflywheel/crest/pkg/schema"
)

// Fixed identifiers, so a failing assertion names something a person
// recognises. These are the same strings as tests/fixtures/world.yaml; the
// loader checks that each one is actually present, which is what stops this
// block from quietly becoming a second, wrong copy of the world.
const (
	OrgID        = "did:crest:party:01JCREST000000000000000RGN"
	SpecifierID  = "did:crest:party:01JCREST00000000000000SPEC"
	SupervisorID = "did:crest:party:01JCREST00000000000000SPVR"

	// The registry custodian holds the two decisions the system refuses to make
	// for itself: which candidate a held match is (§4), and whose work an
	// unattributed row was (#25).
	CustodianID = "did:crest:party:01JCREST00000000000000CSTD"

	// Three workers at three assurance levels: anchored, contact-verified,
	// asserted. WorkerC is the one that matters most — the weakest worker must
	// still be paid and still get a credential.
	WorkerAID = "did:crest:party:01JCREST00000000000000WRKA"
	WorkerBID = "did:crest:party:01JCREST00000000000000WRKB"
	WorkerCID = "did:crest:party:01JCREST00000000000000WRKC"

	ProjectID      = "crest:context:01JCREST00000000000000PRJC"
	DefinitionID   = "crest:definition:01JCREST00000000000000DEFN"
	TermsID        = "crest:terms:01JCREST00000000000000TERM"
	PaymentSetupID = "crest:linked-record:01JCREST00000000000000PAYS"
)

// Instance is the deployment-level facts the world declares.
type Instance struct {
	Name string `json:"name"`
	// Epoch is where the harness starts its clock. A seven-day window is then
	// arithmetic, not a wait.
	Epoch time.Time `json:"epoch"`
}

// World is the whole fixture world.
type World struct {
	Instance       Instance               `json:"instance"`
	Parties        []schema.Party         `json:"parties"`
	Skills         []schema.Skill         `json:"skills"`
	Terms          []schema.Terms         `json:"terms"`
	Authorizations []schema.Authorization `json:"authorizations"`
	Contexts       []schema.Context       `json:"contexts"`
	Definitions    []schema.Definition    `json:"definitions"`
	LinkedRecords  []schema.LinkedRecord  `json:"linkedRecords"`
	// RuntimeIDs contains server-assigned ids for a seeded run. It is kept out
	// of the fixture document: callers continue to use the stable fixture names
	// while the harness resolves them to ids returned by public APIs.
	RuntimeIDs map[string]string `json:"-"`
}

// Load reads tests/fixtures/world.yaml and validates every object in it, at
// the epoch the file itself declares.
func Load() (*World, error) { return LoadAt(time.Time{}) }

// LoadAt is Load with the whole world slid onto a different epoch.
//
// The world's dates are absolute, and every one of them is meaningful relative
// to the others: terms signed before the authorization that cites them, a
// qualification satisfied before the definition that requires it, an
// authorization period that has not run out. A deployment seeded months after
// the file was written wants that same shape at today's dates, not a programme
// whose authorizations expire on a date already past.
//
// So rather than rewrite the file, every timestamp in it moves by the same
// delta. Spacing is preserved exactly; only the anchor changes. A zero epoch
// means "leave it where the file put it", which is what the test suites want:
// a scenario asserting on a fixed date should not depend on the day it runs.
func LoadAt(epoch time.Time) (*World, error) {
	path, err := worldPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path) //nolint:gosec // a path we located ourselves, in-repo
	if err != nil {
		return nil, fmt.Errorf("read fixture world: %w", err)
	}
	if !epoch.IsZero() {
		raw, err = rebase(raw, epoch)
		if err != nil {
			return nil, fmt.Errorf("rebase %s: %w", path, err)
		}
	}

	// Through JSON rather than a YAML-native decode: the json tags on the
	// generated types are the contract, and this way the world is parsed by the
	// same rules the wire format uses.
	var w World
	if err := yaml.UnmarshalStrict(raw, &w); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := w.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &w, nil
}

// rebase slides every RFC3339 timestamp in the world by the delta that moves
// its declared epoch to `to`.
//
// Text-level on purpose: the timestamps are spread across a dozen types and
// several map-valued extension fields, and a walk of the parsed tree is the
// only way to catch all of them without a per-type list that would go stale
// the first time a field is added.
func rebase(raw []byte, to time.Time) ([]byte, error) {
	var tree any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	var anchor struct {
		Instance Instance `json:"instance"`
	}
	if err := yaml.Unmarshal(raw, &anchor); err != nil {
		return nil, err
	}
	if anchor.Instance.Epoch.IsZero() {
		return nil, fmt.Errorf("no instance.epoch to rebase from")
	}
	delta := to.UTC().Sub(anchor.Instance.Epoch)
	out, err := json.Marshal(shift(tree, delta))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// shift walks a decoded document, moving every value that is an RFC3339
// instant and leaving everything else exactly as it was. A string that merely
// looks date-ish — an id, a version — does not parse as RFC3339 and is
// therefore untouched.
func shift(v any, d time.Duration) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = shift(val, d)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = shift(val, d)
		}
		return t
	case string:
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return ts.Add(d).UTC().Format(time.RFC3339)
		}
		return t
	default:
		return v
	}
}

// MustLoad is Load for tests, which have nothing useful to do with the error.
func MustLoad() *World {
	w, err := Load()
	if err != nil {
		panic(err)
	}
	return w
}

func (w *World) validate() error {
	groups := []struct {
		name     string
		schemaID string
		items    []any
	}{
		{"party", schema.IDParty, toAny(w.Parties)},
		{"terms", schema.IDTerms, toAny(w.Terms)},
		{"authorization", schema.IDAuthorization, toAny(w.Authorizations)},
		{"context", schema.IDContext, toAny(w.Contexts)},
		{"definition", schema.IDDefinition, toAny(w.Definitions)},
		{"linkedRecord", schema.IDLinkedRecord, toAny(w.LinkedRecords)},
	}
	for _, g := range groups {
		if len(g.items) == 0 {
			return fmt.Errorf("no %s in the world — the harness needs all of them", g.name)
		}
		for i, item := range g.items {
			if err := schema.Validate(g.schemaID, item); err != nil {
				return fmt.Errorf("%s[%d]: %w", g.name, i, err)
			}
		}
	}
	// A LinkedRecord's payload is the profile's business, so the core schema
	// treats it as an opaque object. Somebody still has to check it, and for
	// the fixture world that is here.
	for i, lr := range w.LinkedRecords {
		if lr.Type == "payment-setup" {
			if err := schema.Validate(schema.IDPaymentSetup, lr.Payload); err != nil {
				return fmt.Errorf("linkedRecord[%d] payload: %w", i, err)
			}
		}
	}
	return w.checkNamedIDs()
}

// checkNamedIDs stops the constants above from drifting into a second, wrong
// copy of the world. Every name a test can refer to must resolve.
func (w *World) checkNamedIDs() error {
	present := map[string]bool{}
	for _, p := range w.Parties {
		present[p.ID] = true
	}
	for _, c := range w.Contexts {
		present[c.ID] = true
	}
	for _, d := range w.Definitions {
		present[d.ID] = true
	}
	for _, t := range w.Terms {
		present[t.ID] = true
	}
	for _, l := range w.LinkedRecords {
		present[l.ID] = true
	}
	for _, id := range []string{
		OrgID, SpecifierID, SupervisorID, CustodianID, WorkerAID, WorkerBID, WorkerCID,
		ProjectID, DefinitionID, TermsID, PaymentSetupID,
	} {
		if !present[id] {
			return fmt.Errorf("named id %s is not in the world", id)
		}
	}
	return nil
}

// Definition returns the world's single work definition.
func (w *World) Definition() schema.Definition { return w.Definitions[0] }

// Party returns a party by id.
func (w *World) Party(id string) (schema.Party, bool) {
	for _, p := range w.Parties {
		if p.ID == id {
			return p, true
		}
		// Runtime ids are allocated by the registry, while the fixture slice
		// intentionally keeps its stable ids. Accept either spelling without
		// replacing the fixture id and then looking for that runtime id in the
		// unchanged slice.
		if runtime, ok := w.RuntimeIDs[p.ID]; ok && runtime == id {
			return p, true
		}
	}
	return schema.Party{}, false
}

// ResolveID returns the server-assigned id for a fixture id, or the input when
// the fixture object deliberately retains its stable id (definitions, terms,
// and linked records are immutable public documents whose ids are supplied at
// creation).
func (w *World) ResolveID(id string) string {
	if runtime, ok := w.RuntimeIDs[id]; ok {
		return runtime
	}
	return id
}

func toAny[T any](items []T) []any {
	out := make([]any, len(items))
	for i, it := range items {
		out[i] = it
	}
	return out
}

// worldPath finds tests/fixtures/world.yaml by walking up to the module root,
// so the loader works from any package's test directory.
func worldPath() (string, error) {
	// CREST_WORLD points straight at the file for environments with no repo
	// checkout — the one-shot seeding container on a deployed demo.
	if p := os.Getenv("CREST_WORLD"); p != "" {
		return p, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "tests", "fixtures", "world.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		if dir == filepath.Dir(dir) {
			return "", fmt.Errorf("tests/fixtures/world.yaml not found above %s", wd)
		}
	}
}
