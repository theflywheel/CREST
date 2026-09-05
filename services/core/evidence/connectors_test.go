package evidence

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/adapters"
	"github.com/theflywheel/crest/adapters/builtin"
)

type connectorStub string

func (s connectorStub) Ref() string { return string(s) }
func (connectorStub) Parse(io.Reader, adapters.Source, time.Time) ([]adapters.Row, []adapters.Rejection, error) {
	return nil, nil, nil
}

func TestConnectorForDefaultsExistingCallersToCSV(t *testing.T) {
	got, err := connectorFor(builtin.Registry(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref() != defaultConnectorRef {
		t.Fatalf("default = %q, want %q", got.Ref(), defaultConnectorRef)
	}
}

func TestConnectorForSelectsExactRegisteredVersion(t *testing.T) {
	r := adapters.MustRegistry(adapters.Connector{
		Adapter: connectorStub("example-api@2"), Class: "example-api", Name: "Example",
	})
	got, err := connectorFor(r, "example-api@2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref() != "example-api@2" {
		t.Fatalf("selected %q", got.Ref())
	}
	if _, err := connectorFor(r, "example-api@1"); err == nil || !strings.Contains(err.Error(), "example-api@2") {
		t.Fatalf("unsupported version error = %v", err)
	}
}
