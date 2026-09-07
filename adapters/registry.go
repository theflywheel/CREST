package adapters

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Plugin is a statically linked adapter implementation. Deployments compose a
// catalogue from these packages; the registry never loads code named by an
// HTTP request or environment variable.
type Plugin struct {
	Ref string
	New func() Adapter
}

// AdapterDescriptor is the public, non-sensitive catalogue entry for a
// registered adapter version.
type AdapterDescriptor struct {
	AdapterRef string `json:"adapterRef"`
}

// Registry resolves only adapter factories explicitly supplied by service
// wiring. A source registration stores the resolved Ref, so the same version
// is used for parsing and provenance.
type Registry struct {
	plugins map[string]Plugin
}

// NewRegistry validates and builds a statically linked adapter catalogue.
func NewRegistry(plugins ...Plugin) (Registry, error) {
	r := Registry{plugins: make(map[string]Plugin, len(plugins))}
	for _, plugin := range plugins {
		ref := strings.TrimSpace(plugin.Ref)
		if ref == "" || !strings.Contains(ref, "@") {
			return Registry{}, fmt.Errorf("adapter plugin has invalid ref %q", plugin.Ref)
		}
		if plugin.New == nil {
			return Registry{}, fmt.Errorf("adapter plugin %q has no factory", ref)
		}
		if _, exists := r.plugins[ref]; exists {
			return Registry{}, fmt.Errorf("adapter plugin %q is registered more than once", ref)
		}
		adapter := plugin.New()
		if adapter == nil || adapter.Ref() != ref {
			return Registry{}, fmt.Errorf("adapter plugin %q factory does not produce the declared ref", ref)
		}
		plugin.Ref = ref
		r.plugins[ref] = plugin
	}
	return r, nil
}

// Lookup returns a fresh adapter instance for a registered version.
func (r Registry) Lookup(ref string) (Adapter, bool) {
	plugin, ok := r.plugins[strings.TrimSpace(ref)]
	if !ok {
		return nil, false
	}
	adapter := plugin.New()
	if adapter == nil || adapter.Ref() != plugin.Ref {
		return nil, false
	}
	return adapter, true
}

// Catalogue lists registered versions in stable order for an operator or
// configuration screen. It intentionally contains no source or credential
// data.
func (r Registry) Catalogue() []AdapterDescriptor {
	refs := make([]string, 0, len(r.plugins))
	for ref := range r.plugins {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	out := make([]AdapterDescriptor, 0, len(refs))
	for _, ref := range refs {
		out = append(out, AdapterDescriptor{AdapterRef: ref})
	}
	return out
}

// ErrUnknown is useful to callers that need to distinguish an unregistered
// version from a parser failure without exposing implementation details.
var ErrUnknown = errors.New("adapter version is not registered")
