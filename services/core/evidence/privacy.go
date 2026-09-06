package evidence

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theflywheel/crest/pkg/schema"
)

// privacyCheck protects callers that construct records without going through
// the CSV adapter. The adapter rejects these values at the edge, but canonical
// records can also arrive from another adapter and the ledger must enforce the
// same boundary for every source class.
func privacyCheck(rec schema.CanonicalWorkEvidenceRecord) error {
	if rec.Geography != nil && looksLikePreciseLocation(*rec.Geography) {
		return fmt.Errorf("geography contains a precise location; only a coarse geography may be stored")
	}
	if len(rec.Enrichment) > 64 {
		return fmt.Errorf("enrichment has too many fields; at most 64 may be retained")
	}
	for key, value := range rec.Enrichment {
		if len(strings.TrimSpace(key)) == 0 || len(key) > 128 {
			return fmt.Errorf("enrichment field name is empty or too long")
		}
		if sensitiveEnrichmentField(key) {
			return fmt.Errorf("enrichment field %q contains personal or precise-location data", key)
		}
		if value != nil {
			raw, err := json.Marshal(value)
			if err != nil || len(raw) > 4096 {
				return fmt.Errorf("enrichment field %q is too large to retain", key)
			}
		}
	}
	return nil
}

func sensitiveEnrichmentField(name string) bool {
	n := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(strings.ToLower(name))
	switch n {
	case "lat", "latitude", "lon", "lng", "longitude", "gps", "geolocation", "coordinates",
		"name", "fullname", "firstname", "lastname", "email", "emailaddress", "address", "streetaddress",
		"workername", "legalname", "nationalid", "nationalidentifier", "governmentid", "phone", "phonenumber", "mobile", "contactnumber":
		return true
	default:
		return strings.HasSuffix(n, "email") || strings.HasSuffix(n, "phone") || strings.HasSuffix(n, "address") ||
			strings.HasSuffix(n, "latitude") || strings.HasSuffix(n, "longitude") ||
			strings.Contains(n, "coordinate") || strings.Contains(n, "geolocation")
	}
}

func looksLikePreciseLocation(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		for _, r := range part {
			if (r < '0' || r > '9') && r != '.' && r != '-' && r != '+' {
				return false
			}
		}
	}
	return true
}
