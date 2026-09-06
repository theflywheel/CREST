package adapters_test

import (
	"io"
	"testing"
	"time"

	"github.com/theflywheel/crest/adapters"
)

type testAdapter struct{ ref string }

func (a testAdapter) Ref() string { return a.ref }
func (a testAdapter) Parse(_ io.Reader, _ adapters.Source, _ time.Time) ([]adapters.Row, []adapters.Rejection, error) {
	return nil, nil, nil
}

func TestRegistryResolvesOnlyRegisteredVersions(t *testing.T) {
	r, err := adapters.NewRegistry(adapters.Plugin{
		Ref: "test@1",
		New: func() adapters.Adapter { return testAdapter{ref: "test@1"} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := r.Lookup("test@1"); !ok || got.Ref() != "test@1" {
		t.Fatalf("lookup registered adapter = %v, %v", got, ok)
	}
	if _, ok := r.Lookup("test@2"); ok {
		t.Fatal("unknown adapter version resolved")
	}
	cat := r.Catalogue()
	if len(cat) != 1 || cat[0].AdapterRef != "test@1" {
		t.Fatalf("catalogue = %#v", cat)
	}
}

func TestRegistryRejectsFactoryRefMismatchAndDuplicate(t *testing.T) {
	if _, err := adapters.NewRegistry(adapters.Plugin{
		Ref: "test@1",
		New: func() adapters.Adapter { return testAdapter{ref: "test@2"} },
	}); err == nil {
		t.Fatal("factory with mismatched ref was accepted")
	}
	plugin := adapters.Plugin{Ref: "test@1", New: func() adapters.Adapter { return testAdapter{ref: "test@1"} }}
	if _, err := adapters.NewRegistry(plugin, plugin); err == nil {
		t.Fatal("duplicate adapter ref was accepted")
	}
}
