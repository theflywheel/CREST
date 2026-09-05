package adapters_test

import (
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/theflywheel/crest/adapters"
)

type stubAdapter string

func (a stubAdapter) Ref() string { return string(a) }
func (stubAdapter) Parse(io.Reader, adapters.Source, time.Time) ([]adapters.Row, []adapters.Rejection, error) {
	return nil, nil, nil
}

func connector(ref string) adapters.Connector {
	return adapters.Connector{Adapter: stubAdapter(ref), Class: "test", Name: ref}
}

func TestRegistryLooksUpExactVersionsAndListsStably(t *testing.T) {
	r, err := adapters.NewRegistry(connector("z-source@2"), connector("a-source@1"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := r.Lookup("a-source@1"); !ok || got.Adapter.Ref() != "a-source@1" {
		t.Fatalf("lookup = %#v, %v", got, ok)
	}
	if _, ok := r.Lookup("a-source@2"); ok {
		t.Fatal("an unregistered version resolved")
	}
	if got, want := r.Refs(), []string{"a-source@1", "z-source@2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("refs = %v, want %v", got, want)
	}
}

func TestRegistryRejectsUnsafeRegistrations(t *testing.T) {
	for name, registrations := range map[string][]adapters.Connector{
		"unversioned":   {connector("source")},
		"ambiguous ref": {connector("source@format@1")},
		"spaced ref":    {connector(" source@1 ")},
		"duplicate":     {connector("source@1"), connector("source@1")},
		"no class":      {{Adapter: stubAdapter("source@1"), Name: "Source"}},
		"no name":       {{Adapter: stubAdapter("source@1"), Class: "test"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := adapters.NewRegistry(registrations...); err == nil {
				t.Fatal("invalid registration was accepted")
			}
		})
	}
}
