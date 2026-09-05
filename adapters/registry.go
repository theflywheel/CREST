package adapters

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	// ErrInvalidConnector means a connector lacks required registry metadata or a versioned ref.
	ErrInvalidConnector = errors.New("invalid connector registration")
	// ErrDuplicateConnector means two implementations claim the same exact ref.
	ErrDuplicateConnector = errors.New("duplicate connector reference")
)

// Connector is an adapter together with the information needed to expose it
// in configuration and operator tooling.
type Connector struct {
	Adapter      Adapter
	Class        string
	Name         string
	ContentTypes []string
	Description  string
}

// Ref returns the versioned reference implemented by the connector.
func (c Connector) Ref() string {
	if c.Adapter == nil {
		return ""
	}
	return c.Adapter.Ref()
}

// Registry is the immutable set of connectors linked into a CREST service.
type Registry struct {
	byRef map[string]Connector
}

// NewRegistry validates registrations and returns a registry. Connector refs
// must be versioned so changing a translator cannot silently change the
// meaning of evidence already attributed to an older implementation.
func NewRegistry(connectors ...Connector) (*Registry, error) {
	r := &Registry{byRef: make(map[string]Connector, len(connectors))}
	for _, connector := range connectors {
		rawRef := connector.Ref()
		ref := strings.TrimSpace(rawRef)
		switch {
		case connector.Adapter == nil:
			return nil, fmt.Errorf("%w: adapter is nil", ErrInvalidConnector)
		case rawRef != ref || ref == "" || strings.Count(ref, "@") != 1 || strings.HasPrefix(ref, "@") || strings.HasSuffix(ref, "@"):
			return nil, fmt.Errorf("%w: adapter ref %q must have name@version form", ErrInvalidConnector, ref)
		case strings.TrimSpace(connector.Class) == "":
			return nil, fmt.Errorf("%w: %s has no class", ErrInvalidConnector, ref)
		case strings.TrimSpace(connector.Name) == "":
			return nil, fmt.Errorf("%w: %s has no name", ErrInvalidConnector, ref)
		}
		if _, exists := r.byRef[ref]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateConnector, ref)
		}
		connector.Class = strings.TrimSpace(connector.Class)
		connector.Name = strings.TrimSpace(connector.Name)
		connector.ContentTypes = append([]string(nil), connector.ContentTypes...)
		r.byRef[ref] = connector
	}
	return r, nil
}

// MustRegistry is for process wiring, where an invalid built-in connector is
// a programming error and there is no useful service CREST can start instead.
func MustRegistry(connectors ...Connector) *Registry {
	r, err := NewRegistry(connectors...)
	if err != nil {
		panic(err)
	}
	return r
}

// Lookup resolves the exact version named by a source or ingestion request.
func (r *Registry) Lookup(ref string) (Connector, bool) {
	if r == nil {
		return Connector{}, false
	}
	c, ok := r.byRef[ref]
	return c, ok
}

// Connectors returns a stable, ref-sorted copy for catalogues and diagnostics.
func (r *Registry) Connectors() []Connector {
	if r == nil {
		return []Connector{}
	}
	out := make([]Connector, 0, len(r.byRef))
	for _, connector := range r.byRef {
		connector.ContentTypes = append([]string(nil), connector.ContentTypes...)
		out = append(out, connector)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref() < out[j].Ref() })
	return out
}

// Refs returns the supported versioned refs in stable order.
func (r *Registry) Refs() []string {
	connectors := r.Connectors()
	refs := make([]string, len(connectors))
	for i, connector := range connectors {
		refs[i] = connector.Ref()
	}
	return refs
}
