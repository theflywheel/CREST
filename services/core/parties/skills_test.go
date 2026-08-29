package parties

import (
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

func str(s string) *string { return &s }

// The published face is what a verifier reads off the node. Optional fields
// that are absent must stay absent rather than becoming empty strings — an
// empty `supersedes` reads as "supersedes nothing in particular" rather than
// "this is the first version".
func TestSkillFaceOmitsWhatIsAbsent(t *testing.T) {
	face := skillFace(schema.Skill{
		Code:        "CREST-SKILL:chw.bednet-distribution.v1",
		Label:       "Bednet distribution",
		PublishedAt: time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC),
	})
	for _, k := range []string{"supersedes", "description"} {
		if _, found := face[k]; found {
			t.Errorf("%q was published despite being absent: %v", k, face[k])
		}
	}
	if face["publishedAt"] != "2026-01-10T09:00:00Z" {
		t.Errorf("publishedAt = %v", face["publishedAt"])
	}
}

func TestSkillFaceCarriesTheSupersessionChain(t *testing.T) {
	face := skillFace(schema.Skill{
		Code:        "CREST-SKILL:chw.bednet-distribution.v2",
		Label:       "Bednet distribution",
		Supersedes:  str("CREST-SKILL:chw.bednet-distribution.v1"),
		PublishedAt: time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC),
	})
	// Without this, a worker holding a credential against v1 has a code that
	// resolves and no way to learn it has a successor — their older evidence
	// silently stops connecting to the current vocabulary.
	if face["supersedes"] != "CREST-SKILL:chw.bednet-distribution.v1" {
		t.Fatalf("supersedes = %v", face["supersedes"])
	}
}

// The code format carries the version, so the schema has to refuse one that
// does not. A code without a version is a code whose meaning can change under
// the credentials that already carry it.
func TestSkillCodeMustCarryItsVersion(t *testing.T) {
	for name, code := range map[string]string{
		"no version":   "CREST-SKILL:chw.bednet-distribution",
		"wrong prefix": "SKILL:chw.bednet-distribution.v1",
		"upper case":   "CREST-SKILL:CHW.Bednet.v1",
		"empty":        "",
		"version only": "CREST-SKILL:v1",
	} {
		err := schema.Validate(schema.IDSkill, schema.Skill{
			Code: code, Label: "x", PublishedAt: time.Now(),
		})
		if err == nil {
			t.Errorf("%s (%q) was accepted as a skill code", name, code)
		}
	}
	if err := schema.Validate(schema.IDSkill, schema.Skill{
		Code: "CREST-SKILL:chw.bednet-distribution.v2", Label: "x", PublishedAt: time.Now(),
	}); err != nil {
		t.Errorf("a well-formed code was rejected: %v", err)
	}
}
