// Command codegen turns the JSON Schemas in schemas/ into Go types.
//
// schemas/ is the source of truth (docs/COMPONENTS.md). Hand-writing Go structs
// beside it gives two definitions of a primitive that agree right up until they
// don't — and the one that drifts is the one nobody re-reads.
//
// It deliberately understands only the subset of JSON Schema the CREST schemas
// use, and fails on anything else rather than guessing. A generator that
// silently skips a construct produces a type that is wrong in a way the
// compiler cannot see.
//
//	go run ./tools/codegen            # write pkg/schema/types_gen.go
//	go run ./tools/codegen -check     # fail if the file is stale (CI)
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	outPath      = "pkg/schema/types_gen.go"
	embedOutPath = "pkg/schema/schemas_gen.go"
)

type schema struct {
	ID          string             `json:"$id"`
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Type        any                `json:"type"`
	Properties  map[string]*schema `json:"properties"`
	Required    []string           `json:"required"`
	Items       *schema            `json:"items"`
	Ref         string             `json:"$ref"`
	Defs        map[string]*schema `json:"$defs"`
	Enum        []string           `json:"enum"`
	Const       any                `json:"const"`
	Format      string             `json:"format"`

	// Validation-only keywords. Parsed so that an unknown keyword can still be
	// detected, but they never affect the generated shape: a Go type cannot
	// express "minLength 1", and pretending otherwise would be worse than the
	// runtime validation that does express it.
	MinLength  *int     `json:"minLength"`
	MinItems   *int     `json:"minItems"`
	MaxItems   *int     `json:"maxItems"`
	Minimum    *float64 `json:"minimum"`
	Maximum    *float64 `json:"maximum"`
	ExclMin    *float64 `json:"exclusiveMinimum"`
	Pattern    string   `json:"pattern"`
	AllOf      []any    `json:"allOf"`
	AddlProps  *bool    `json:"additionalProperties"`
	SchemaMeta string   `json:"$schema"`
}

type generator struct {
	raw   map[string]string // $id -> the file's bytes, for embedding
	byID  map[string]*schema
	types map[string]string // Go type name -> rendered declaration
	order []string
}

func main() {
	check := flag.Bool("check", false, "fail if the generated file is out of date")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fail(err)
	}
	files, err := schemaFiles(filepath.Join(root, "schemas"))
	if err != nil {
		fail(err)
	}

	g := &generator{byID: map[string]*schema{}, raw: map[string]string{}, types: map[string]string{}}
	for _, f := range files {
		s, raw, err := load(f)
		if err != nil {
			fail(fmt.Errorf("%s: %w", f, err))
		}
		if s.ID == "" {
			fail(fmt.Errorf("%s: schema has no $id", f))
		}
		g.byID[s.ID] = s
		g.raw[s.ID] = raw
	}

	if err := g.run(); err != nil {
		fail(err)
	}
	src, err := g.render()
	if err != nil {
		fail(err)
	}
	// The schemas travel with the binary. A service that validates against a
	// schema it reads from disk validates against whatever is on that disk;
	// embedding makes the version that shipped the version that is enforced.
	embedded, err := g.renderEmbedded()
	if err != nil {
		fail(err)
	}

	outputs := map[string][]byte{outPath: src, embedOutPath: embedded}
	if *check {
		for rel, want := range outputs {
			got, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				fail(fmt.Errorf("%s missing — run `make generate`", rel))
			}
			if !bytes.Equal(got, want) {
				fail(fmt.Errorf("%s is stale — run `make generate`", rel))
			}
		}
		fmt.Println("codegen: up to date")
		return
	}
	for rel, content := range outputs {
		out := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(out, content, 0o600); err != nil {
			fail(err)
		}
	}
	fmt.Printf("codegen: wrote %s (%d types) and %s (%d schemas)\n",
		outPath, len(g.order), embedOutPath, len(g.byID))
}

// run generates the shared $defs first, so that every cross-file reference
// resolves to an already-named type rather than an inlined copy.
func (g *generator) run() error {
	ids := make([]string, 0, len(g.byID))
	for id := range g.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		s := g.byID[id]
		for _, name := range sortedKeys(s.Defs) {
			def := s.Defs[name]
			if _, err := g.typeFor(def, goName(name), s, def.Description); err != nil {
				return fmt.Errorf("%s $defs/%s: %w", id, name, err)
			}
		}
	}
	for _, id := range ids {
		s := g.byID[id]
		if s.Title == "" {
			return fmt.Errorf("%s: schema has no title", id)
		}
		if len(s.Properties) == 0 {
			continue // a definitions-only file, e.g. common
		}
		if _, err := g.typeFor(s, goName(s.Title), s, s.Description); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
	}
	return nil
}

// typeFor returns the Go type expression for s, declaring a named type when s
// is an object or a string enum. name is the name to use if a declaration is
// needed; doc is the comment to attach.
func (g *generator) typeFor(s *schema, name string, in *schema, doc string) (string, error) {
	if s.Ref != "" {
		return g.resolve(s.Ref, in)
	}
	switch typeOf(s) {
	case "object":
		if len(s.Properties) == 0 {
			// Deliberately opaque: "the core stores it and never reads inside it".
			return "map[string]any", nil
		}
		return g.declareStruct(s, name, in, doc)
	case "array":
		if s.Items == nil {
			return "", fmt.Errorf("array without items")
		}
		elem, err := g.typeFor(s.Items, name+"Item", in, "")
		if err != nil {
			return "", err
		}
		return "[]" + elem, nil
	case "string":
		if len(s.Enum) > 0 {
			return g.declareEnum(s, name, doc), nil
		}
		if s.Format == "date-time" {
			return "time.Time", nil
		}
		return "string", nil
	case "integer":
		return "int", nil
	case "number":
		return "float64", nil
	case "boolean":
		return "bool", nil
	case "":
		// `const` with no declared type, as in "type": {"const": "worker"}.
		if s.Const != nil {
			return "string", nil
		}
		return "", fmt.Errorf("no type and no const")
	default:
		return "", fmt.Errorf("unsupported type %q", typeOf(s))
	}
}

func (g *generator) declareStruct(s *schema, name string, in *schema, doc string) (string, error) {
	if _, done := g.types[name]; done {
		return name, nil
	}
	g.types[name] = "" // claim the name first: a self-reference must not recurse
	g.order = append(g.order, name)

	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}

	var b strings.Builder
	b.WriteString(comment(doc, ""))
	fmt.Fprintf(&b, "type %s struct {\n", name)
	for _, prop := range sortedKeys(s.Properties) {
		p := s.Properties[prop]
		field := goName(prop)
		typ, err := g.typeFor(p, name+field, in, p.Description)
		if err != nil {
			return "", fmt.Errorf("property %q: %w", prop, err)
		}
		tag := prop
		if !required[prop] {
			tag += ",omitempty"
			if needsPointer(typ) {
				typ = "*" + typ
			}
		}
		if d := comment(p.Description, "\t"); d != "" {
			b.WriteString("\n" + d)
		}
		fmt.Fprintf(&b, "\t%s %s `json:%q`\n", field, typ, tag)
	}
	b.WriteString("}\n")
	g.types[name] = b.String()
	return name, nil
}

func (g *generator) declareEnum(s *schema, name, doc string) string {
	if _, done := g.types[name]; done {
		return name
	}
	g.order = append(g.order, name)

	var b strings.Builder
	b.WriteString(comment(doc, ""))
	fmt.Fprintf(&b, "type %s string\n\nconst (\n", name)
	for _, v := range s.Enum {
		fmt.Fprintf(&b, "\t%s%s %s = %q\n", name, goName(v), name, v)
	}
	b.WriteString(")\n")
	g.types[name] = b.String()
	return name
}

// resolve follows a $ref, which is either local (#/$defs/x) or a cross-file
// URN with a fragment. Both land on a $defs entry, which run() has already
// declared, so this returns a name rather than generating a second copy.
func (g *generator) resolve(ref string, in *schema) (string, error) {
	base, frag, _ := strings.Cut(ref, "#")
	target := in
	if base != "" {
		t, ok := g.byID[base]
		if !ok {
			return "", fmt.Errorf("unknown schema %q", base)
		}
		target = t
	}
	const prefix = "/$defs/"
	if !strings.HasPrefix(frag, prefix) {
		return "", fmt.Errorf("only $defs references are supported, got %q", ref)
	}
	defName := strings.TrimPrefix(frag, prefix)
	def, ok := target.Defs[defName]
	if !ok {
		return "", fmt.Errorf("%s has no $defs/%s", target.ID, defName)
	}
	return g.typeFor(def, goName(defName), target, def.Description)
}

func (g *generator) render() ([]byte, error) {
	var b strings.Builder
	b.WriteString(`// Code generated by tools/codegen from schemas/. DO NOT EDIT.
//
// The JSON Schemas in schemas/ are the source of truth; these types are one
// projection of them. Editing this file makes it the second definition of a
// primitive, which is the thing schemas/README.md exists to prevent.
//
// What is NOT here: every constraint a Go type cannot carry — minLength,
// patterns, enum membership on input, conditional requirements. Those are
// enforced by validating against the schema itself (pkg/schema.Validate), not
// by the struct. A value of one of these types is well-shaped, not valid.
package schema

import "time"
`)
	names := append([]string(nil), g.order...)
	sort.Strings(names)
	for _, n := range names {
		b.WriteString("\n" + g.types[n])
	}
	return format.Source([]byte(b.String()))
}

// renderEmbedded writes every schema, verbatim, as a Go string keyed by $id.
// Verbatim matters: a re-marshalled schema is a schema someone has to trust the
// marshaller about.
func (g *generator) renderEmbedded() ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by tools/codegen from schemas/. DO NOT EDIT.\n")
	b.WriteString("\npackage schema\n")
	b.WriteString("\n// Sources is every CREST schema, verbatim, keyed by its $id. It is what\n")
	b.WriteString("// Validate compiles against, so the schemas a binary enforces are the ones\n")
	b.WriteString("// it was built from rather than whatever happens to be on disk beside it.\n")
	b.WriteString("var Sources = map[string]string{\n")

	ids := make([]string, 0, len(g.byID))
	for id := range g.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintf(&b, "\t%q: %q,\n", id, g.raw[id])
	}
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}

// needsPointer says whether an optional field of this type needs a pointer to
// distinguish "absent" from "zero". Slices and maps already carry nil.
func needsPointer(typ string) bool {
	return !strings.HasPrefix(typ, "[]") && !strings.HasPrefix(typ, "map[")
}

func typeOf(s *schema) string {
	switch t := s.Type.(type) {
	case string:
		return t
	case []any:
		for _, v := range t {
			if str, ok := v.(string); ok && str != "null" {
				return str
			}
		}
	}
	return ""
}

var acronyms = map[string]string{
	"id": "ID", "did": "DID", "url": "URL", "urn": "URN", "ussd": "USSD",
	"sms": "SMS", "api": "API", "json": "JSON", "ia": "IA", "qr": "QR",
}

// goName turns a JSON name into an exported Go identifier: camelCase,
// kebab-case, "@context" and enum values like "IA-3" all arrive here.
func goName(s string) string {
	s = strings.TrimPrefix(s, "@")
	var parts []string
	var cur strings.Builder
	for i, r := range s {
		switch {
		case r == '-' || r == '_' || r == ' ' || r == '.' || r == '/':
			parts = append(parts, cur.String())
			cur.Reset()
		case r >= 'A' && r <= 'Z' && i > 0:
			parts = append(parts, cur.String())
			cur.Reset()
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
	}
	parts = append(parts, cur.String())

	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if a, ok := acronyms[strings.ToLower(p)]; ok {
			b.WriteString(a)
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
}

// comment wraps a schema description as a Go comment. The descriptions carry
// the reasoning — why a field exists, which invariant it serves — and that is
// exactly what someone reading the Go type needs.
func comment(doc, indent string) string {
	if doc == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range wrap(doc, 72-len(indent)*4) {
		b.WriteString(indent + "// " + line + "\n")
	}
	return b.String()
}

func wrap(s string, width int) []string {
	var lines []string
	var cur string
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case len(cur)+1+len(word) <= width:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func load(path string) (*schema, string, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path comes from our own walk of schemas/
	if err != nil {
		return nil, "", err
	}
	var s schema
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, "", fmt.Errorf("the generator understands a fixed subset of JSON Schema: %w", err)
	}
	return &s, string(raw), nil
}

func schemaFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".json") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		if dir == filepath.Dir(dir) {
			return "", fmt.Errorf("no go.mod above %s", wd)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "codegen:", err)
	os.Exit(1)
}
