package verification

import (
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

func TestTrustedResolverURLRejectsUntrustedURLShapes(t *testing.T) {
	for _, url := range []string{
		"file:///etc/passwd",
		"https://user:pass@example.test/keys",
		"https://issuer.example/keys?redirect=http://169.254.169.254",
		"https://issuer.example/keys#fragment",
		"/relative/path",
	} {
		if trustedResolverURL(url) {
			t.Errorf("trustedResolverURL(%q) accepted an unsafe resolver", url)
		}
	}
	if !trustedResolverURL("https://issuer.example/registry") {
		t.Fatal("trustedResolverURL rejected a configured HTTPS resolver")
	}
}

func TestStatusFreshRequiresSignedWindow(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	valid := map[string]any{
		"validFrom":  now.Add(-time.Minute).Format(time.RFC3339),
		"validUntil": now.Add(time.Minute).Format(time.RFC3339),
	}
	if !statusFresh(valid, now) {
		t.Fatal("status list inside its signed validity window was rejected")
	}
	valid["validUntil"] = now.Format(time.RFC3339)
	if statusFresh(valid, now) {
		t.Fatal("expired status list was accepted")
	}
}

func TestProvenanceSystemRefSeparatesCurrentAndLegacyAssessments(t *testing.T) {
	legacy := schema.Provenance{AdapterRef: "csv-batch@1"}
	if got := provenanceSystemRef(legacy); got != "" {
		t.Fatalf("legacy provenance unexpectedly selected scoped assessment %q", got)
	}
	ref := "riverside-dhis2"
	current := schema.Provenance{AdapterRef: "csv-batch@1", SystemRef: &ref}
	if got := provenanceSystemRef(current); got != ref {
		t.Fatalf("current provenance systemRef = %q, want %q", got, ref)
	}
}
