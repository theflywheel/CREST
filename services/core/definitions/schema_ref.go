package definitions

import (
	"fmt"
	"strings"

	"github.com/theflywheel/crest/pkg/schema"
)

// validateDefinitionSchemaRef keeps the platform face tied to a schema that
// this deployment can actually validate. The definition schema only checks
// that schemaRef is a non-empty string; accepting any string here would let a
// definition become ACTIVE while the evidence ingester has no validator for
// its records.
func validateDefinitionSchemaRef(ref string) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("faces.platform.schemaRef is required")
	}
	for _, registered := range schema.IDs() {
		if ref == registered {
			return nil
		}
	}
	return fmt.Errorf("faces.platform.schemaRef %q is not registered", ref)
}

func schemaRefProblem(ref string) Problem {
	return Problem{
		Section: "sources",
		Field:   "schemaRef",
		Reason:  validateDefinitionSchemaRef(ref).Error(),
	}
}
