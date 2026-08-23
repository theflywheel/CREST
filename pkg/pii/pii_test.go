package pii_test

import (
	"strings"
	"testing"

	"github.com/theflywheel/crest/pkg/pii"
)

func hasher(t *testing.T) *pii.Hasher {
	t.Helper()
	h, err := pii.NewHasher("a-fixture-salt-that-is-long-enough", "fixture-salt-1")
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// The same identifier formatted three ways is one person. Without normalisation
// they are three, and the duplicate is invisible because every hash is valid.
func TestFormattingDoesNotCreateASecondPerson(t *testing.T) {
	h := hasher(t)
	want := h.Hash("1234 5678 9012")
	for _, variant := range []string{"1234-5678-9012", "123456789012", " 1234 5678 9012 ", "1234.5678.9012"} {
		if got := h.Hash(variant); got != want {
			t.Errorf("%q hashes differently; the same person would get two records", variant)
		}
	}
}

func TestDifferentIdentifiersDoNotCollide(t *testing.T) {
	h := hasher(t)
	if h.Hash("123456789012") == h.Hash("123456789013") {
		t.Error("two identifiers hashed the same")
	}
}

// A different deployment must not be able to recognise another deployment's
// hashes. That is what stops a leaked table from being cross-referenced with
// another country's.
func TestSaltsSeparateDeployments(t *testing.T) {
	a, _ := pii.NewHasher("deployment-a-salt-long-enough!!", "a")
	b, _ := pii.NewHasher("deployment-b-salt-long-enough!!", "b")
	if a.Hash("123456789012") == b.Hash("123456789012") {
		t.Error("the same identifier hashed the same under two salts")
	}
}

func TestAShortSaltIsRefused(t *testing.T) {
	if _, err := pii.NewHasher("short", "x"); err == nil {
		t.Error("a five-byte salt was accepted; an enumerable identifier space needs a real one")
	}
}

// The hash is what gets stored, so it must not carry the identifier's shape.
func TestTheHashLooksNothingLikeTheInput(t *testing.T) {
	h := hasher(t)
	got := h.Hash("123456789012")
	if strings.Contains(got, "123456789012") || len(got) != 64 {
		t.Errorf("hash %q is not a 64-character digest", got)
	}
}
